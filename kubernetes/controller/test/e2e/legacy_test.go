package e2e

import (
	"os"
	"path/filepath"
)

// Filenames common to scenarios. They are constants instead of magic
// strings so a typo at one call site fails the build, not the run. The
// declarative runner does not consume these — they survive only for the
// applyset and credentials specs that have not yet migrated to the new
// scenario format.
const (
	ComponentConstructor = "component-constructor.yaml"
	Bootstrap            = "bootstrap.yaml"
	Rgd                  = "rgd.yaml"
	Instance             = "instance.yaml"
	K8sManifest          = "k8s-manifest.yaml"
	PublicKey            = "ocm.software.pub"
	PrivateKey           = "ocm.software"
)

// legacyExamplesDir resolves the examples folder for tests that have not
// yet migrated to the declarative runner (applyset, credentials). It
// honors EXAMPLES_DIR/PROJECT_DIR so tests can be invoked from anywhere,
// matching the lookup the legacy `init()` used to perform.
//
// Once Stage 4 lands these specs as scenarios, this helper and the
// constants above can be deleted.
func legacyExamplesDir() string {
	if dir := os.Getenv("EXAMPLES_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(projectDir(), "examples")
}

// legacyScenariosDir resolves the test/e2e/scenarios folder, which holds
// test-only fixtures (corner cases not meant as user-facing demos). Honors
// SCENARIOS_DIR so tests can be invoked from anywhere. Like
// legacyExamplesDir, this helper exists only until Stage 4 of the runner
// migration lands.
func legacyScenariosDir() string {
	if dir := os.Getenv("SCENARIOS_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(projectDir(), "test", "e2e", "scenarios")
}
