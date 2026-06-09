package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/kubernetes/controller/test/e2e/hooks"
	"ocm.software/open-component-model/kubernetes/controller/test/utils"
)

// Scenario execution engine — ScenarioConfig, runScenario, walkScenarios, loadScenario, etc.

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
	Debug    []DebugCmd   `json:"debug,omitempty"`

	PreDeployHooks   []string `json:"preDeployHooks,omitempty"`
	PostDeployHooks  []string `json:"postDeployHooks,omitempty"`
	PreAssertHooks   []string `json:"preAssertHooks,omitempty"`
	PostAssertHooks  []string `json:"postAssertHooks,omitempty"`
	PreCleanupHooks  []string `json:"preCleanupHooks,omitempty"`
	PostCleanupHooks []string `json:"postCleanupHooks,omitempty"`
}

type DebugCmd struct {
	Kubectl string `json:"kubectl"`
	Label   string `json:"label,omitempty"`
}

type PrepareSpec struct {
	Components []PrepareComponent `json:"components,omitempty"`
}

type PrepareComponent struct {
	Constructor   string `json:"constructor"`
	SigningKey    string `json:"signingKey,omitempty"`
	OCMConfig     string `json:"ocmConfig,omitempty"`
	Registry      string `json:"registry,omitempty"`
	CopyResources bool   `json:"copyResources,omitempty"`
}

type DeployStep struct {
	Apply   string      `json:"apply,omitempty"`
	WaitFor WaitForList `json:"waitFor,omitempty"`
	Debug   []DebugCmd  `json:"debug,omitempty"`
}

type WaitForList []WaitForSpec

func (w *WaitForList) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] != '[' {
		return fmt.Errorf("waitFor must be an array, got: %s", string(data[:min(len(data), 40)]))
	}
	var list []WaitForSpec
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	*w = list
	return nil
}

type WaitForSpec struct {
	Kind       string   `json:"kind,omitempty"`
	Name       string   `json:"name,omitempty"`
	Namespace  string   `json:"namespace,omitempty"`
	Timeout    string   `json:"timeout,omitempty"`
	Conditions []string `json:"conditions,omitempty"`
	Kubectl    string   `json:"kubectl,omitempty"`
}

type AssertSpec struct {
	Kubectl     []string         `json:"kubectl,omitempty"`
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

	if len(cfg.Prepare.Components) == 0 {
		if _, err := os.Stat(filepath.Join(scenarioDir, "component-constructor.yaml")); err == nil {
			cfg.Prepare.Components = []PrepareComponent{{Constructor: "component-constructor.yaml"}}
		}
	}

	if err := validateHookRefs(&cfg); err != nil {
		return nil, fmt.Errorf("scenario %s: %w", folder, err)
	}

	if err := validateWaitFor(&cfg); err != nil {
		return nil, fmt.Errorf("scenario %s: %w", folder, err)
	}

	if componentsDir != "" {
		if err := validateRequires(&cfg, componentsDir); err != nil {
			return nil, fmt.Errorf("scenario %s: %w", folder, err)
		}
	}

	return &cfg, nil
}

// validateWaitFor checks that each waitFor entry uses either the kubectl
// shorthand OR the structured kind/name/conditions fields, never both.
func validateWaitFor(cfg *ScenarioConfig) error {
	for i, step := range cfg.Deploy {
		for j, w := range step.WaitFor {
			hasKubectl := w.Kubectl != ""
			hasStructured := w.Kind != "" || w.Name != "" || len(w.Conditions) > 0
			if hasKubectl && hasStructured {
				return fmt.Errorf("deploy[%d].waitFor[%d]: cannot mix 'kubectl' with 'kind'/'name'/'conditions'", i, j)
			}
			if !hasKubectl && !hasStructured {
				return fmt.Errorf("deploy[%d].waitFor[%d]: must specify either 'kubectl' or 'kind'+'name'+'conditions'", i, j)
			}
		}
	}
	return nil
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

	DeferCleanup(func() {
		if !CurrentSpecReport().Failed() && !isWorkflowDebug() {
			return
		}
		runDebugCommands(cfg)
	})

	scenarioCtx := &hooks.Scenario{
		Folder:     cfg.Folder,
		SimpleName: cfg.SimpleName,
		Dir:        cfg.Dir,
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
		constructor := comp.Constructor
		if constructor == "" {
			constructor = "component-constructor.yaml"
		}
		Expect(utils.PrepareOCMComponentWithOptions(ctx, utils.PrepareOCMComponentOptions{
			Name:                     cfg.SimpleName,
			ComponentConstructorPath: filepath.Join(cfg.Dir, constructor),
			ImageRegistry:            registry,
			SigningKey:               signingKey,
			OCMConfig:                ocmConfig,
			CopyResources:            comp.CopyResources,
		})).To(Succeed(), "PrepareOCMComponent failed for %s", comp.Constructor)
	}

	dispatchHooks("preDeployHooks", cfg.PreDeployHooks, scenarioCtx)

	By("deploying scenario " + cfg.Folder)
	for i, step := range cfg.Deploy {
		if step.Apply != "" {
			manifest := filepath.Join(cfg.Dir, step.Apply)
			err := utils.DeployResource(ctx, manifest)
			if err != nil {
				runStepDebug(cfg.Deploy[i].Debug)
				Expect(err).NotTo(HaveOccurred(), "kubectl apply -f %s failed", manifest)
			}
			if isWorkflowDebug() == true {
				runStepDebug(cfg.Deploy[i].Debug)
			}
		}
		for _, w := range step.WaitFor {
			t := timeout
			if w.Timeout != "" {
				t = w.Timeout
			}
			if w.Kubectl != "" {
				args := strings.Fields(w.Kubectl)
				args = append(args, "--timeout="+t)
				cmd := exec.CommandContext(ctx, "kubectl", append([]string{"wait"}, args...)...)
				out, err := utils.Run(cmd)
				if err != nil {
					runStepDebug(cfg.Deploy[i].Debug)
					Expect(err).NotTo(HaveOccurred(), "kubectl wait %s failed: %s", w.Kubectl, string(out))
				}
			} else {
				resource := w.Kind + "/" + w.Name
				args := []string{resource}
				if w.Namespace != "" {
					args = append(args, "-n", w.Namespace)
				}
				for _, cond := range w.Conditions {
					err := utils.WaitForResource(ctx, cond, t, args...)
					if err != nil {
						runStepDebug(cfg.Deploy[i].Debug)
						Expect(err).NotTo(HaveOccurred(), "wait %s on %s failed", cond, resource)
					}
				}
			}
		}
	}

	dispatchHooks("postDeployHooks", cfg.PostDeployHooks, scenarioCtx)
	dispatchHooks("preAssertHooks", cfg.PreAssertHooks, scenarioCtx)

	By("asserting scenario " + cfg.Folder)
	for _, kc := range cfg.Assert.Kubectl {
		args := strings.Fields(kc)
		cmd := exec.CommandContext(ctx, "kubectl", args...)
		out, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "assert kubectl %s failed: %s", kc, string(out))
	}
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

var defaultDebugCommands = []DebugCmd{
	{Kubectl: "get pods -n " + controllerNamespace + " -o wide", Label: "controller-pods"},
	{Kubectl: "logs -n " + controllerNamespace + " deploy/ocm-k8s-toolkit-controller-manager --tail=80 --all-containers", Label: "controller-logs"},
	{Kubectl: "get pods -n kro -o wide", Label: "kro-pods"},
	{Kubectl: "get events --sort-by=.lastTimestamp", Label: "events"},
	{Kubectl: "get rgd -o custom-columns=NAME:.metadata.name,READY:.status.conditions[?(@.type==\"Ready\")].status,READY_MSG:.status.conditions[?(@.type==\"Ready\")].message", Label: "rgd-conditions"},
}

// isWorkflowDebug reports whether the GitHub Actions workflow is running in
// debug mode. GitHub sets RUNNER_DEBUG=1 when the user picks "Re-run with
// debug logging" and ACTIONS_STEP_DEBUG=true when step-level debug is
// enabled via repo/secret configuration. Either flag is sufficient — the
// runner then runs the scenario's debug: commands on success as well as on
// failure, so a debug-mode re-run yields a cluster snapshot without having
// to manufacture a failure.
func isWorkflowDebug() bool {
	return os.Getenv("RUNNER_DEBUG") == "1" ||
		strings.EqualFold(os.Getenv("ACTIONS_STEP_DEBUG"), "true")
}

func runStepDebug(cmds []DebugCmd) {
	if len(cmds) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, d := range cmds {
		label := d.Label
		if label == "" {
			label = d.Kubectl
		}
		args := strings.Fields(d.Kubectl)
		cmd := exec.CommandContext(ctx, "kubectl", args...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			GinkgoLogr.Info(fmt.Sprintf("[STEP-DEBUG] %s: error: %v", label, err))
		} else {
			for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
				GinkgoLogr.Info(fmt.Sprintf("[STEP-DEBUG] %s: %s", label, line))
			}
		}
	}
}

func runDebugCommands(cfg *ScenarioConfig) {
	cmds := cfg.Debug
	if len(cmds) == 0 {
		cmds = defaultDebugCommands
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, d := range cmds {
		label := d.Label
		if label == "" {
			label = d.Kubectl
		}
		args := append([]string{}, strings.Fields(d.Kubectl)...)
		cmd := exec.CommandContext(ctx, "kubectl", args...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			GinkgoLogr.Info(fmt.Sprintf("[DEBUG] %s: error: %v", label, err))
		} else {
			for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
				GinkgoLogr.Info(fmt.Sprintf("[DEBUG] %s: %s", label, line))
			}
		}
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
