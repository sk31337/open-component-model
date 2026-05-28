package e2e

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/kubernetes/controller/test/e2e/hooks"
)

// e2eYamlFile is the per-scenario contract filename. Its presence in a
// directory marks that directory as a scenario root; the walker stops
// descending past it.
const e2eYamlFile = "e2e.yaml"

// ScenarioConfig is the parsed and variable-substituted shape of an
// e2e.yaml. Field documentation lives in DESIGN.md §"The e2e.yaml schema";
// keep the two in sync.
type ScenarioConfig struct {
	// Folder, SimpleName, Dir are not loaded from YAML; they are populated
	// by the loader from the scenario's path on disk.
	Folder     string `json:"-"`
	SimpleName string `json:"-"`
	Dir        string `json:"-"`

	Timeout  string       `json:"timeout,omitempty"`
	Requires []string     `json:"requires,omitempty"`
	Prepare  PrepareSpec  `json:"prepare,omitempty"`
	Deploy   []DeployStep `json:"deploy,omitempty"`
	Assert   AssertSpec   `json:"assert,omitempty"`
	Cleanup  CleanupSpec  `json:"cleanup,omitempty"`

	PreDeployHooks   []string `json:"preDeployHooks,omitempty"`
	PostDeployHooks  []string `json:"postDeployHooks,omitempty"`
	PreAssertHooks   []string `json:"preAssertHooks,omitempty"`
	PostAssertHooks  []string `json:"postAssertHooks,omitempty"`
	PreCleanupHooks  []string `json:"preCleanupHooks,omitempty"`
	PostCleanupHooks []string `json:"postCleanupHooks,omitempty"`
}

type PrepareSpec struct {
	Components []PrepareComponent `json:"components,omitempty"`
}

type PrepareComponent struct {
	Constructor string `json:"constructor"`
	SigningKey  string `json:"signingKey,omitempty"`
	OCMConfig   string `json:"ocmConfig,omitempty"`
}

type DeployStep struct {
	Apply   string       `json:"apply,omitempty"`
	WaitFor *WaitForSpec `json:"waitFor,omitempty"`
}

type WaitForSpec struct {
	Kind       string   `json:"kind"`
	Name       string   `json:"name"`
	Namespace  string   `json:"namespace,omitempty"`
	Conditions []string `json:"conditions"`
}

type AssertSpec struct {
	Resources   []AssertResource `json:"resources,omitempty"`
	FieldEquals []FieldEquals    `json:"fieldEquals,omitempty"`
}

type AssertResource struct {
	Kind      string            `json:"kind"`
	Name      string            `json:"name"`
	Namespace string            `json:"namespace,omitempty"`
	WaitFor   []string          `json:"waitFor,omitempty"`
	JSONPath  map[string]string `json:"jsonPath,omitempty"`
	Pods      *PodCheck         `json:"pods,omitempty"`
}

type PodCheck struct {
	Selector  string `json:"selector"`
	Condition string `json:"condition"`
}

type FieldEquals struct {
	Resource string `json:"resource"`
	JSONPath string `json:"jsonPath"`
	Value    string `json:"value"`
}

type CleanupSpec struct {
	CascadeFromBootstrap bool   `json:"cascadeFromBootstrap,omitempty"`
	CascadeTimeout       string `json:"cascadeTimeout,omitempty"`
}

// walkScenarios returns the absolute paths of every directory under root that
// contains an e2e.yaml. The walker stops descending into a directory once it
// finds an e2e.yaml there: nested scenarios are illegal.
//
// A non-existent root is treated as "no scenarios", not an error. This is
// what Stage 1 needs — neither examples/ nor test/e2e/scenarios/ has been
// migrated yet, so callers must tolerate empty results.
func walkScenarios(root string) ([]string, error) {
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, nil
	}

	var found []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if _, statErr := os.Stat(filepath.Join(path, e2eYamlFile)); statErr == nil {
			found = append(found, path)
			return fs.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(found)
	return found, nil
}

// loadScenario parses scenarioDir/e2e.yaml, applies ${VAR} substitution, and
// validates that every referenced hook resolves against hooks.Registry.
//
// root is the discovery root (e.g. .../examples or .../test/e2e/scenarios)
// used to derive the scenario's slash-separated Folder name.
func loadScenario(scenarioDir, root string, vars map[string]string) (*ScenarioConfig, error) {
	folder, err := filepath.Rel(root, scenarioDir)
	if err != nil {
		return nil, fmt.Errorf("scenario %q is not under root %q: %w", scenarioDir, root, err)
	}
	folder = filepath.ToSlash(folder)
	simpleName := strings.ReplaceAll(folder, "/", "-")

	// Per-scenario variables augment the caller-supplied set. We copy first
	// so two scenarios sharing the same vars map cannot trample each other.
	merged := make(map[string]string, len(vars)+3)
	for k, v := range vars {
		merged[k] = v
	}
	merged["SCENARIO_FOLDER"] = folder
	merged["SCENARIO_SIMPLE_NAME"] = simpleName
	merged["SCENARIO_DIR"] = scenarioDir

	raw, err := os.ReadFile(filepath.Join(scenarioDir, e2eYamlFile))
	if err != nil {
		return nil, fmt.Errorf("read %s/%s: %w", scenarioDir, e2eYamlFile, err)
	}

	substituted, err := substituteVars(string(raw), merged)
	if err != nil {
		return nil, fmt.Errorf("substitute vars in %s/%s: %w", scenarioDir, e2eYamlFile, err)
	}

	var cfg ScenarioConfig
	if err := yaml.Unmarshal([]byte(substituted), &cfg); err != nil {
		return nil, fmt.Errorf("parse %s/%s: %w", scenarioDir, e2eYamlFile, err)
	}
	cfg.Folder = folder
	cfg.SimpleName = simpleName
	cfg.Dir = scenarioDir

	if err := validateHookRefs(&cfg); err != nil {
		return nil, fmt.Errorf("scenario %s: %w", folder, err)
	}

	return &cfg, nil
}

// validateHookRefs returns an error naming any hook reference in cfg that
// does not resolve against hooks.Registry. All six phase arrays are checked
// at once so a scenario with multiple typos surfaces all of them.
func validateHookRefs(cfg *ScenarioConfig) error {
	phases := map[string][]string{
		"preDeployHooks":   cfg.PreDeployHooks,
		"postDeployHooks":  cfg.PostDeployHooks,
		"preAssertHooks":   cfg.PreAssertHooks,
		"postAssertHooks":  cfg.PostAssertHooks,
		"preCleanupHooks":  cfg.PreCleanupHooks,
		"postCleanupHooks": cfg.PostCleanupHooks,
	}
	var missing []string
	for phase, names := range phases {
		for _, name := range names {
			if _, ok := hooks.Resolve(name); !ok {
				missing = append(missing, fmt.Sprintf("%s[%q]", phase, name))
			}
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("unknown hook reference(s): %s", strings.Join(missing, ", "))
	}
	return nil
}

// varRef matches ${NAME} where NAME is letters, digits, and underscores.
// It deliberately does not match ${NAME:-default} or any other shell-ism;
// the schema is fixed-list envsubst, not bash.
var varRef = regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*)\}`)

// substituteVars expands every ${NAME} reference in s against vars. An
// unknown reference is a hard error — DESIGN.md §"Templated variables".
func substituteVars(s string, vars map[string]string) (string, error) {
	var unknown []string
	out := varRef.ReplaceAllStringFunc(s, func(match string) string {
		name := match[2 : len(match)-1]
		val, ok := vars[name]
		if !ok {
			unknown = append(unknown, name)
			return match
		}
		return val
	})
	if len(unknown) > 0 {
		// Deduplicate while preserving order of first occurrence.
		seen := make(map[string]struct{}, len(unknown))
		var dedup []string
		for _, name := range unknown {
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			dedup = append(dedup, name)
		}
		return "", fmt.Errorf("unknown variable(s): %s", strings.Join(dedup, ", "))
	}
	return out, nil
}

// controllerNamespace is the kubernetes namespace the controller-manager
// runs in. Mirrored here so e2e_runner.go (a non-test file) can reference
// it without depending on symbols declared in *_test.go files.
const controllerNamespace = "ocm-k8s-toolkit-system"

// builtinVars returns the harness-level variables (everything except the
// per-scenario ${SCENARIO_*} set, which loadScenario adds).
//
// Stage 1 wires only the variables the existing tests already export
// (IMAGE_REGISTRY, derived IMAGE_REGISTRY_HOST, CONTROLLER_NAMESPACE).
// Protected-registry variables are added when Stage 4 migrates the
// credentials scenarios.
func builtinVars() map[string]string {
	registry := os.Getenv("IMAGE_REGISTRY")
	host := registry
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	return map[string]string{
		"IMAGE_REGISTRY":       registry,
		"IMAGE_REGISTRY_HOST":  host,
		"CONTROLLER_NAMESPACE": controllerNamespace,
	}
}

// runScenario executes one parsed scenario against the live cluster. Stage 1
// lands the function as a no-op shell so the rest of the wiring compiles;
// Stage 2 fills in the cluster operations. Until then the scenarios slice is
// always empty (no migrated scenarios exist), so the no-op body is never
// reached.
func runScenario(cfg *ScenarioConfig) {
	// Intentionally empty. See DESIGN.md §"Migration plan".
	_ = cfg
}
