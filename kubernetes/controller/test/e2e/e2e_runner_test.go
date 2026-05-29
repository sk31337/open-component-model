package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubstituteVars(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      string
		vars    map[string]string
		want    string
		wantErr string
	}{
		{
			name: "no variables",
			in:   "plain text with no refs",
			vars: nil,
			want: "plain text with no refs",
		},
		{
			name: "single substitution",
			in:   "hello ${NAME}",
			vars: map[string]string{"NAME": "world"},
			want: "hello world",
		},
		{
			name: "multiple substitutions",
			in:   "${A}-${B}-${A}",
			vars: map[string]string{"A": "x", "B": "y"},
			want: "x-y-x",
		},
		{
			name:    "unknown variable",
			in:      "hello ${UNKNOWN}",
			vars:    map[string]string{"NAME": "world"},
			wantErr: "UNKNOWN",
		},
		{
			name:    "duplicate unknown is reported once",
			in:      "${X} and ${X} again",
			vars:    nil,
			wantErr: "X",
		},
		{
			name:    "multiple unknowns are reported together",
			in:      "${A} ${B}",
			vars:    nil,
			wantErr: "A, B",
		},
		{
			name: "shell default syntax is not interpreted",
			// ${X:-default} is not the schema; the regex does not match it
			// (colon is not in the allowed name charset), so it passes through.
			in:   "${X:-default}",
			vars: nil,
			want: "${X:-default}",
		},
		{
			name: "lowercase names are not matched",
			// Schema is uppercase only. Lowercase passes through.
			in:   "${name}",
			vars: nil,
			want: "${name}",
		},
		{
			name: "scenario name embedding",
			in:   "deployment.apps/${SCENARIO_SIMPLE_NAME}-podinfo",
			vars: map[string]string{"SCENARIO_SIMPLE_NAME": "helm-simple"},
			want: "deployment.apps/helm-simple-podinfo",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := substituteVars(tt.in, tt.vars)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got nil; result=%q", tt.wantErr, got)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWalkScenarios(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// Layout under root:
	//   helm/                    (group dir, no e2e.yaml)
	//     simple/e2e.yaml        ← scenario
	//     simple/nested/e2e.yaml ← MUST NOT be discovered (descend stops)
	//     signing/e2e.yaml       ← scenario
	//   kustomize/               (group dir, no e2e.yaml)
	//     simple/                (no e2e.yaml — not a scenario)
	//   README.md                (incidental file)
	mkdir := func(p string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(root, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	touch := func(p string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, p), []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mkdir("helm/simple/nested")
	mkdir("helm/signing")
	mkdir("kustomize/simple")
	touch("helm/simple/e2e.yaml")
	touch("helm/simple/nested/e2e.yaml") // must be hidden by descend-stop rule
	touch("helm/signing/e2e.yaml")
	touch("README.md")

	got, err := walkScenarios(root)
	if err != nil {
		t.Fatalf("walkScenarios: %v", err)
	}

	want := []string{
		filepath.Join(root, "helm", "signing"),
		filepath.Join(root, "helm", "simple"),
	}
	if len(got) != len(want) {
		t.Fatalf("got %d scenarios, want %d: %v", len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("scenario[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestWalkScenariosMissingRoot(t *testing.T) {
	t.Parallel()
	got, err := walkScenarios(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("expected nil error for missing root, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

func TestLoadScenarioMinimal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	scenarioDir := filepath.Join(root, "helm", "simple")
	if err := os.MkdirAll(scenarioDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := `
requires:
  - kro
  - flux-source

prepare:
  components:
    - constructor: component-constructor.yaml

deploy:
  - apply: bootstrap.yaml
  - apply: rgd.yaml
    waitFor:
      kind: rgd
      name: ${SCENARIO_SIMPLE_NAME}
      conditions: [create, condition=Ready=true]

assert:
  resources:
    - kind: deployment.apps
      name: ${SCENARIO_SIMPLE_NAME}-podinfo
      waitFor: [create, condition=Available]
`
	if err := os.WriteFile(filepath.Join(scenarioDir, "e2e.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadScenario(scenarioDir, root, "", nil)
	if err != nil {
		t.Fatalf("loadScenario: %v", err)
	}
	if cfg.Folder != "helm/simple" {
		t.Errorf("Folder = %q, want %q", cfg.Folder, "helm/simple")
	}
	if cfg.SimpleName != "helm-simple" {
		t.Errorf("SimpleName = %q, want %q", cfg.SimpleName, "helm-simple")
	}
	if cfg.Dir != scenarioDir {
		t.Errorf("Dir = %q, want %q", cfg.Dir, scenarioDir)
	}
	if got := cfg.Requires; len(got) != 2 || got[0] != "kro" || got[1] != "flux-source" {
		t.Errorf("Requires = %v, want [kro flux-source]", got)
	}
	if len(cfg.Deploy) != 2 || cfg.Deploy[1].WaitFor == nil || cfg.Deploy[1].WaitFor.Name != "helm-simple" {
		t.Errorf("deploy waitFor.name not substituted: %+v", cfg.Deploy)
	}
	if len(cfg.Assert.Resources) != 1 || cfg.Assert.Resources[0].Name != "helm-simple-podinfo" {
		t.Errorf("assert resource.name not substituted: %+v", cfg.Assert.Resources)
	}
}

func TestLoadScenarioUnknownHookRejected(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	scenarioDir := filepath.Join(root, "applyset", "pruning")
	if err := os.MkdirAll(scenarioDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := `
requires: []
postAssertHooks:
  - thisHookDoesNotExist
deploy:
  - apply: bootstrap.yaml
assert: {}
`
	if err := os.WriteFile(filepath.Join(scenarioDir, "e2e.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := loadScenario(scenarioDir, root, "", nil)
	if err == nil {
		t.Fatal("expected error for unknown hook reference, got nil")
	}
	if !strings.Contains(err.Error(), "thisHookDoesNotExist") {
		t.Errorf("error should name the missing hook, got: %v", err)
	}
}

func TestLoadScenarioUnknownVarRejected(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	scenarioDir := filepath.Join(root, "helm", "simple")
	if err := os.MkdirAll(scenarioDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := `
deploy:
  - apply: ${MADE_UP_VARIABLE}
`
	if err := os.WriteFile(filepath.Join(scenarioDir, "e2e.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := loadScenario(scenarioDir, root, "", nil)
	if err == nil {
		t.Fatal("expected error for unknown variable, got nil")
	}
	if !strings.Contains(err.Error(), "MADE_UP_VARIABLE") {
		t.Errorf("error should name the missing variable, got: %v", err)
	}
}

func TestLoadScenarioUnknownRequiresRejected(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	scenarioDir := filepath.Join(root, "helm", "simple")
	if err := os.MkdirAll(scenarioDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := `
requires: [kro, this-component-does-not-exist]
deploy: []
`
	if err := os.WriteFile(filepath.Join(scenarioDir, "e2e.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	componentsDir := t.TempDir()
	// kro.sh exists; the second name doesn't.
	if err := os.WriteFile(filepath.Join(componentsDir, "kro.sh"), []byte("#!/usr/bin/env bash\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := loadScenario(scenarioDir, root, componentsDir, nil)
	if err == nil {
		t.Fatal("expected error for unknown requires entry, got nil")
	}
	if !strings.Contains(err.Error(), "this-component-does-not-exist") {
		t.Errorf("error should name the missing component, got: %v", err)
	}
}

func TestLoadScenarioRequiresAllPresent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	scenarioDir := filepath.Join(root, "helm", "simple")
	if err := os.MkdirAll(scenarioDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := `
requires: [kro, flux-source]
deploy: []
`
	if err := os.WriteFile(filepath.Join(scenarioDir, "e2e.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	componentsDir := t.TempDir()
	for _, n := range []string{"kro.sh", "flux-source.sh"} {
		if err := os.WriteFile(filepath.Join(componentsDir, n), []byte("#!/usr/bin/env bash\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := loadScenario(scenarioDir, root, componentsDir, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuiltinVarsRegistryHostStripsScheme(t *testing.T) {
	tests := []struct {
		registry string
		wantHost string
	}{
		{"http://image-registry:5000", "image-registry:5000"},
		{"https://image-registry:5000", "image-registry:5000"},
		{"image-registry:5000", "image-registry:5000"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.registry, func(t *testing.T) {
			t.Setenv("IMAGE_REGISTRY", tt.registry)
			got := builtinVars()
			if got["IMAGE_REGISTRY"] != tt.registry {
				t.Errorf("IMAGE_REGISTRY = %q, want %q", got["IMAGE_REGISTRY"], tt.registry)
			}
			if got["IMAGE_REGISTRY_HOST"] != tt.wantHost {
				t.Errorf("IMAGE_REGISTRY_HOST = %q, want %q", got["IMAGE_REGISTRY_HOST"], tt.wantHost)
			}
		})
	}
}
