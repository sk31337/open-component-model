package e2e

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/kubernetes/controller/test/e2e/hooks"
	"ocm.software/open-component-model/kubernetes/controller/test/utils"
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
	// Registry overrides the transfer target for this component. When empty
	// the harness falls back to ${IMAGE_REGISTRY}. Used by credentials
	// scenarios to push into the protected registry the bootstrap then
	// references.
	Registry string `json:"registry,omitempty"`
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
// validates that every referenced hook resolves against hooks.Registry and
// that every `requires:` entry has a matching script under componentsDir.
//
// root is the discovery root (e.g. .../examples or .../test/e2e/scenarios)
// used to derive the scenario's slash-separated Folder name. componentsDir
// is the absolute path to test/e2e/setup/components used to validate
// `requires:` entries — pass "" to skip that check (used by unit tests that
// stub the harness).
func loadScenario(scenarioDir, root, componentsDir string, vars map[string]string) (*ScenarioConfig, error) {
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

	if componentsDir != "" {
		if err := validateRequires(&cfg, componentsDir); err != nil {
			return nil, fmt.Errorf("scenario %s: %w", folder, err)
		}
	}

	return &cfg, nil
}

// validateRequires returns an error naming any `requires:` entry that has no
// matching `<name>.sh` under componentsDir. DESIGN.md §"Setup composition":
// each requires entry is the basename of a script in test/e2e/setup/components.
func validateRequires(cfg *ScenarioConfig, componentsDir string) error {
	var missing []string
	for _, name := range cfg.Requires {
		script := filepath.Join(componentsDir, name+".sh")
		if _, err := os.Stat(script); err != nil {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("unknown requires entries (no matching script in %s): %s",
			componentsDir, strings.Join(missing, ", "))
	}
	return nil
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
// The protected-registry variables expose the externally-reachable URL the
// `ocm transfer` step pushes to. The bootstrap.yaml manifests reference the
// in-cluster URL directly and do not template it; that mirrors the legacy
// test fixtures, which hard-code the kube-internal hostname.
func builtinVars() map[string]string {
	registry := os.Getenv("IMAGE_REGISTRY")
	host := registry
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	return map[string]string{
		"IMAGE_REGISTRY":                        registry,
		"IMAGE_REGISTRY_HOST":                   host,
		"CONTROLLER_NAMESPACE":                  controllerNamespace,
		"PROTECTED_REGISTRY_BASIC_AUTH":         os.Getenv("PROTECTED_REGISTRY_URL"),
		"PROTECTED_REGISTRY_DOCKER_CONFIG_JSON": os.Getenv("PROTECTED_REGISTRY_URL2"),
	}
}

// runScenario executes one parsed scenario against the live cluster.
// It is invoked from inside an It() body, so it relies on Ginkgo's Expect
// machinery for assertions and on test/utils helpers for kubectl/ocm calls.
//
// The execution order mirrors DESIGN.md §"Per-scenario lifecycle":
//
//	prepare → preDeployHooks → deploy → postDeployHooks
//	        → preAssertHooks → assert → postAssertHooks
//	        → preCleanupHooks → (DeferCleanup runs)  → postCleanupHooks
//
// Cleanup itself is delegated to DeployResource's DeferCleanup, so the
// declarative cleanup section in e2e.yaml is currently advisory; the
// CascadeFromBootstrap flag is honored implicitly by the order in which
// deploy steps are applied (bootstrap first → its DeferCleanup deletes
// last).
func runScenario(cfg *ScenarioConfig) {
	ctx := context.Background()
	timeout := scenarioTimeout(cfg)
	imageRegistry := os.Getenv("IMAGE_REGISTRY")

	scenarioCtx := &hooks.Scenario{
		Folder:     cfg.Folder,
		SimpleName: cfg.SimpleName,
		Dir:        cfg.Dir,
	}

	if len(cfg.Requires) > 0 {
		By("ensuring required components for " + cfg.Folder)
		dir := componentsDir()
		for _, name := range cfg.Requires {
			script := filepath.Join(dir, name+".sh")
			By(fmt.Sprintf("installing component %q (script %s)...", name, script))
			cmd := exec.CommandContext(ctx, "bash", script)
			cmd.Stdout = GinkgoWriter
			cmd.Stderr = GinkgoWriter
			Expect(cmd.Run()).To(Succeed(), "requires component %q (script %s) failed", name, script)
		}
	}

	By("preparing OCM components for " + cfg.Folder)
	for _, comp := range cfg.Prepare.Components {
		signingKey := ""
		if comp.SigningKey != "" {
			signingKey = filepath.Join(cfg.Dir, comp.SigningKey)
		} else if candidate := filepath.Join(cfg.Dir, "ocm.software"); fileExists(candidate) {
			signingKey = candidate
		}
		ocmConfig := ""
		if comp.OCMConfig != "" {
			ocmConfig = filepath.Join(cfg.Dir, comp.OCMConfig)
		}
		registry := imageRegistry
		if comp.Registry != "" {
			registry = comp.Registry
		}
		Expect(utils.PrepareOCMComponentWithOptions(ctx, utils.PrepareOCMComponentOptions{
			Name:                     cfg.SimpleName,
			ComponentConstructorPath: filepath.Join(cfg.Dir, comp.Constructor),
			ImageRegistry:            registry,
			SigningKey:               signingKey,
			OCMConfig:                ocmConfig,
		})).To(Succeed(), "PrepareOCMComponent failed for %s", comp.Constructor)
	}

	dispatchHooks("preDeployHooks", cfg.PreDeployHooks, scenarioCtx)

	By("deploying scenario " + cfg.Folder)
	for _, step := range cfg.Deploy {
		manifest := filepath.Join(cfg.Dir, step.Apply)
		Expect(utils.DeployResource(ctx, manifest)).To(Succeed(), "kubectl apply -f %s failed", manifest)
		if step.WaitFor != nil {
			waitForSpec(ctx, step.WaitFor, timeout)
		}
	}

	dispatchHooks("postDeployHooks", cfg.PostDeployHooks, scenarioCtx)
	dispatchHooks("preAssertHooks", cfg.PreAssertHooks, scenarioCtx)

	By("asserting scenario " + cfg.Folder)
	for _, res := range cfg.Assert.Resources {
		assertResource(ctx, res, timeout)
	}
	for _, fe := range cfg.Assert.FieldEquals {
		Expect(utils.CompareResourceField(ctx, fe.Resource, fe.JSONPath, fe.Value)).To(
			Succeed(),
			"fieldEquals mismatch on %s %s", fe.Resource, fe.JSONPath,
		)
	}

	dispatchHooks("postAssertHooks", cfg.PostAssertHooks, scenarioCtx)
	dispatchHooks("preCleanupHooks", cfg.PreCleanupHooks, scenarioCtx)
	// Actual delete happens via DeferCleanup registered by DeployResource;
	// postCleanupHooks queue a DeferCleanup so they run after that.
	for _, name := range cfg.PostCleanupHooks {
		hook, ok := hooks.Resolve(name)
		Expect(ok).To(BeTrue(), "unknown postCleanupHooks reference %q", name)
		DeferCleanup(func(ctx SpecContext) {
			Expect(hook(ctx, scenarioCtx)).To(Succeed(), "postCleanupHooks[%q] failed", name)
		})
	}
}

// scenarioTimeout returns the timeout to pass to kubectl wait for this
// scenario. cfg.Timeout overrides the suite-wide default.
func scenarioTimeout(cfg *ScenarioConfig) string {
	if cfg.Timeout != "" {
		return cfg.Timeout
	}
	if t := os.Getenv("RESOURCE_TIMEOUT"); t != "" {
		return t
	}
	return "10m"
}

// waitForSpec runs `kubectl wait` for every condition in spec.Conditions,
// failing the spec on the first miss.
func waitForSpec(ctx context.Context, spec *WaitForSpec, timeout string) {
	resource := spec.Kind + "/" + spec.Name
	args := []string{resource}
	if spec.Namespace != "" {
		args = append(args, "-n", spec.Namespace)
	}
	for _, cond := range spec.Conditions {
		Expect(utils.WaitForResource(ctx, cond, timeout, args...)).To(
			Succeed(),
			"wait %s on %s failed", cond, resource,
		)
	}
}

// assertResource runs the kubectl wait sequence for a single AssertResource
// and, if Pods is set, additionally waits for matching pods.
func assertResource(ctx context.Context, res AssertResource, timeout string) {
	resource := res.Kind + "/" + res.Name
	args := []string{resource}
	if res.Namespace != "" {
		args = append(args, "-n", res.Namespace)
	}
	for _, cond := range res.WaitFor {
		Expect(utils.WaitForResource(ctx, cond, timeout, args...)).To(
			Succeed(),
			"wait %s on %s failed", cond, resource,
		)
	}
	if res.Pods != nil {
		podArgs := []string{"pod", "-l", res.Pods.Selector}
		if res.Namespace != "" {
			podArgs = append(podArgs, "-n", res.Namespace)
		}
		Expect(utils.WaitForResource(ctx, res.Pods.Condition, timeout, podArgs...)).To(
			Succeed(),
			"pod wait %s on selector %s failed", res.Pods.Condition, res.Pods.Selector,
		)
	}
}

// dispatchHooks runs every named hook in order. The scenario is rejected at
// load time if any name is unknown, so a missing hook here is a runner bug,
// not a user error.
func dispatchHooks(phase string, names []string, scenario *hooks.Scenario) {
	for _, name := range names {
		hook, ok := hooks.Resolve(name)
		Expect(ok).To(BeTrue(), "%s references unknown hook %q (load-time validation should have caught this)", phase, name)
		ctx, cancel := context.WithCancel(context.Background())
		err := hook(ctx, scenario)
		cancel()
		Expect(err).NotTo(HaveOccurred(), "%s[%q] failed", phase, name)
	}
}

// fileExists reports whether path exists and is a regular file. Used to
// auto-detect the per-scenario `ocm.software` private key.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// componentsDir returns the absolute path to test/e2e/setup/components,
// the directory that holds every component's idempotent install script.
// Resolution mirrors projectDir(): SETUP_COMPONENTS_DIR > PROJECT_DIR >
// cwd, layered with the canonical "test/e2e/setup/components" suffix.
func componentsDir() string {
	if dir := os.Getenv("SETUP_COMPONENTS_DIR"); dir != "" {
		return dir
	}
	base := os.Getenv("PROJECT_DIR")
	if base == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return ""
		}
		base = cwd
	}
	return filepath.Join(base, "test", "e2e", "setup", "components")
}
