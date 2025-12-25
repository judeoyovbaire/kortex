//go:build e2e
// +build e2e

/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/judeoyovbaire/kortex/test/utils"
)

var (
	// Optional Environment Variables:
	// - CERT_MANAGER_INSTALL_SKIP=true: Skips CertManager installation during test setup.
	// These variables are useful if CertManager is already installed, avoiding
	// re-installation and conflicts.
	skipCertManagerInstall = os.Getenv("CERT_MANAGER_INSTALL_SKIP") == "true"
	// isCertManagerAlreadyInstalled will be set true when CertManager CRDs be found on the cluster
	isCertManagerAlreadyInstalled = false

	// projectImage is the name of the image which will be build and loaded
	// with the code source changes to be tested.
	projectImage = "example.com/kortex:v0.0.1"
)

// TestE2E runs the end-to-end (e2e) test suite for the project. These tests execute in an isolated,
// temporary environment to validate project changes with the purpose of being used in CI jobs.
//
// Supported cluster providers (set via CLUSTER_PROVIDER env var):
//   - kind (default): Uses Kind (Kubernetes IN Docker)
//   - minikube: Uses Minikube
//   - docker-desktop: Uses Docker Desktop's built-in Kubernetes
//   - existing: Uses an existing cluster (assumes image is accessible via registry)
//
// Additional environment variables:
//   - KIND_CLUSTER: Kind cluster name (default: "kind")
//   - MINIKUBE_PROFILE: Minikube profile name (default: "minikube")
//   - SKIP_IMAGE_BUILD: Skip building the Docker image (useful for CI with pre-built images)
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting Kortex integration test suite\n")
	RunSpecs(t, "e2e suite")
}

var _ = BeforeSuite(func() {
	// Check if cluster is accessible
	By("verifying cluster is accessible")
	clusterInfo, err := utils.GetClusterInfo()
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to access cluster. Ensure your cluster is running.")
	_, _ = fmt.Fprintf(GinkgoWriter, "Cluster info:\n%s\n", clusterInfo)

	// Build the Docker image unless skipped
	skipImageBuild := os.Getenv("SKIP_IMAGE_BUILD") == "true"
	if !skipImageBuild {
		By("building the manager(Operator) image")
		cmd := exec.Command("make", "docker-build", fmt.Sprintf("IMG=%s", projectImage))
		_, err := utils.Run(cmd)
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to build the manager(Operator) image")
	} else {
		_, _ = fmt.Fprintf(GinkgoWriter, "Skipping image build (SKIP_IMAGE_BUILD=true)\n")
	}

	// Load image to cluster using the configured provider
	By("loading the manager(Operator) image to cluster")
	provider := utils.GetClusterProvider()
	_, _ = fmt.Fprintf(GinkgoWriter, "Using cluster provider: %s\n", provider)
	err = utils.LoadImageToCluster(projectImage)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to load the manager(Operator) image to cluster")

	// The tests-e2e are intended to run on a temporary cluster that is created and destroyed for testing.
	// To prevent errors when tests run in environments with CertManager already installed,
	// we check for its presence before execution.
	// Setup CertManager before the suite if not skipped and if not already installed
	if !skipCertManagerInstall {
		By("checking if cert manager is installed already")
		isCertManagerAlreadyInstalled = utils.IsCertManagerCRDsInstalled()
		if !isCertManagerAlreadyInstalled {
			_, _ = fmt.Fprintf(GinkgoWriter, "Installing CertManager...\n")
			Expect(utils.InstallCertManager()).To(Succeed(), "Failed to install CertManager")
		} else {
			_, _ = fmt.Fprintf(GinkgoWriter, "WARNING: CertManager is already installed. Skipping installation...\n")
		}
	}
})

var _ = AfterSuite(func() {
	// Teardown CertManager after the suite if not skipped and if it was not already installed
	if !skipCertManagerInstall && !isCertManagerAlreadyInstalled {
		_, _ = fmt.Fprintf(GinkgoWriter, "Uninstalling CertManager...\n")
		utils.UninstallCertManager()
	}
})
