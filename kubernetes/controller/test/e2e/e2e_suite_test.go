package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/kubernetes/controller/test/utils"
)

// e2e_suite_test.go Ginkgo suite entry point — TestE2E, SynchronizedBeforeSuite, SynchronizedAfterSuite, collectAndInstallRequires

const namespace = "ocm-k8s-toolkit-system"

var (
	// imageRegistry is the OCI registry the e2e suite pushes and pulls from.
	imageRegistry string
	// timeout is the kubectl wait timeout for suite-wide resource waits.
	timeout string
	// controllerPodName is captured per-process for log collection.
	controllerPodName string
)

// Run e2e tests using the Ginkgo runner.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	fmt.Fprintf(GinkgoWriter, "Starting ocm-k8s-toolkit suite\n")
	RunSpecs(t, "e2e suite")
}

// suiteData is serialised by proc 1 and broadcast to every parallel process.
type suiteData struct {
	ImageRegistry string `json:"imageRegistry"`
	Timeout       string `json:"timeout"`
}

var _ = SynchronizedBeforeSuite(
	// Proc 1 only: install all required components for the active scenarios,
	// then broadcast shared configuration to every process.
	func(ctx SpecContext) {
		timeout := os.Getenv("RESOURCE_TIMEOUT")
		if timeout == "" {
			timeout = "10m"
		}
		imageRegistry := os.Getenv("IMAGE_REGISTRY")
		Expect(imageRegistry).NotTo(BeEmpty(), "IMAGE_REGISTRY must be set")

		By("installing required components (proc 1)", func() {
			collectAndInstallRequires(ctx)
		})

		By("validating controller-manager is running", func() {
			verifyControllerUp := func(ctx context.Context) error {
				cmd := exec.CommandContext(ctx, "kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)
				podOutput, err := utils.Run(cmd)
				ExpectWithOffset(2, err).NotTo(HaveOccurred())

				var podNames []string
				for _, name := range strings.Split(string(podOutput), "\n") {
					if name != "" {
						podNames = append(podNames, name)
					}
				}
				if len(podNames) != 1 {
					return fmt.Errorf("expect 1 controller pod running, but got %d", len(podNames))
				}
				controllerPodName = podNames[0]
				ExpectWithOffset(2, controllerPodName).Should(ContainSubstring("controller-manager"))

				cmd = exec.CommandContext(ctx, "kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				status, err := utils.Run(cmd)
				ExpectWithOffset(2, err).NotTo(HaveOccurred())
				if string(status) != "Running" {
					return fmt.Errorf("controller pod in %s status", status)
				}
				return nil
			}
			EventuallyWithOffset(1, verifyControllerUp, time.Minute, time.Second).WithContext(ctx).Should(Succeed())
		})
	},
	// All procs: unpack shared configuration, verify the controller is running.
	func(ctx SpecContext, data []byte) {
	},
)

var _ = SynchronizedAfterSuite(
	// All procs: no-op — per-spec cleanup is handled by DeferCleanup inside DeployResource.
	func() {},
	// Proc 1 only: dump controller logs when CONTROLLER_LOG_PATH is set.
	func(ctx SpecContext) {
		logPath := os.Getenv("CONTROLLER_LOG_PATH")
		if logPath == "" {
			return
		}
		By("displaying logs from the controller", func() {
			cmd := exec.CommandContext(ctx, "kubectl",
				"logs", "-n", namespace, controllerPodName,
				"--log-path", logPath,
			)
			_, err := utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
		})
	},
)

// collectAndInstallRequires walks both scenario roots, filters by the active
// Ginkgo focus filter, unions the requires: entries across all matching
// scenarios, and runs each component's install script exactly once.
// This runs only on proc 1 inside SynchronizedBeforeSuite.
func collectAndInstallRequires(ctx SpecContext) {
	suiteConf, _ := GinkgoConfiguration()
	focusPatterns := suiteConf.FocusStrings

	dir := projectDir()
	roots := []string{
		filepath.Join(dir, "examples"),
		filepath.Join(dir, "test", "e2e", "scenarios"),
	}

	compsDir := componentsDir()
	vars := builtinVars()

	seen := make(map[string]struct{})
	var ordered []string

	for _, root := range roots {
		dirs, err := walkScenarios(root)
		if err != nil {
			continue
		}
		for _, scenDir := range dirs {
			cfg, err := loadScenario(scenDir, root, compsDir, vars)
			if err != nil {
				continue
			}
			if !scenarioMatchesFocus(cfg.Folder, focusPatterns) {
				continue
			}
			for _, name := range cfg.Requires {
				if _, ok := seen[name]; !ok {
					seen[name] = struct{}{}
					ordered = append(ordered, name)
				}
			}
		}
	}

	g, gctx := errgroup.WithContext(ctx)
	for _, name := range ordered {
		name := name
		g.Go(func() error {
			defer GinkgoRecover()
			script := filepath.Join(compsDir, name+".sh")
			fmt.Fprintf(GinkgoWriter, "==> installing component %q\n", name)
			cmd := exec.CommandContext(gctx, "bash", script)
			cmd.Stdout = GinkgoWriter
			cmd.Stderr = GinkgoWriter
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("component script %s failed: %w", script, err)
			}
			return nil
		})
	}
	Expect(g.Wait()).To(Succeed())
}

// scenarioMatchesFocus reports whether a scenario's folder matches any of the
// Ginkgo focus patterns. An empty patterns list means "match everything".
// Matching is against the full Ginkgo spec name ("should run <folder>") using
// the same regexp the Taskfile produces (^.*<pattern>$).
func scenarioMatchesFocus(folder string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	specName := "should run " + folder
	for _, pat := range patterns {
		re, err := regexp.Compile(pat)
		if err != nil {
			continue
		}
		if re.MatchString(specName) {
			return true
		}
	}
	return false
}
