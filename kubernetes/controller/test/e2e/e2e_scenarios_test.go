package e2e

import (
	"log"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"ocm.software/open-component-model/kubernetes/controller/test/utils"
)

// projectDir is the repository's controller root, used to resolve the two
// scenario discovery roots. It mirrors the existing examplesDir lookup in
// e2e_suite_test.go so callers control it via EXAMPLES_DIR/PROJECT_DIR.
func projectDir() string {
	if dir := os.Getenv("PROJECT_DIR"); dir != "" {
		return dir
	}
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal("could not get current working directory", err)
	}
	return cwd
}

// discoverAndLoad walks `root` for e2e.yaml files and loads each one. It
// is called at package init time (when Ginkgo collects the spec tree), so
// the build cannot tolerate dynamic Expect() failures here — we panic on
// load errors with a clear message and let Ginkgo surface it.
func discoverAndLoad(root string) []*ScenarioConfig {
	dirs, err := walkScenarios(root)
	if err != nil {
		log.Fatalf("walkScenarios(%q): %v", root, err)
	}
	configs := make([]*ScenarioConfig, 0, len(dirs))
	vars := builtinVars()
	compsDir := componentsDir()
	for _, dir := range dirs {
		cfg, err := loadScenario(dir, root, compsDir, vars)
		if err != nil {
			log.Fatalf("loadScenario(%q): %v", dir, err)
		}
		configs = append(configs, cfg)
	}
	return configs
}

var _ = Describe("scenarios", func() {
	dir := projectDir()
	examplesRoot := filepath.Join(dir, "examples")
	scenariosRoot := filepath.Join(dir, "test/e2e/scenarios")

	Context("examples", func() {
		AfterEach(func() {
			if !CurrentSpecReport().Failed() {
				return
			}
			utils.DumpLogs("kro", "rgd")
			utils.DumpLogs("argocd", "applications.argoproj.io")
		})

		for _, cfg := range discoverAndLoad(examplesRoot) {
			cfg := cfg
			It("should run "+cfg.Folder, func(_ SpecContext) {
				runScenario(cfg)
			})
		}
	})

	Context("scenarios", func() {
		AfterEach(func() {
			if !CurrentSpecReport().Failed() {
				return
			}
			utils.DumpLogs("kro", "rgd")
			utils.DumpLogs("argocd", "applications.argoproj.io")
		})

		for _, cfg := range discoverAndLoad(scenariosRoot) {
			cfg := cfg
			It("should run "+cfg.Folder, func(_ SpecContext) {
				runScenario(cfg)
			})
		}
	})

	// Suppress an empty-context error from Ginkgo when no scenarios have
	// been migrated to test/e2e/scenarios yet.
	_ = Expect
})
