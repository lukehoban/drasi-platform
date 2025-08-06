// Copyright 2024 The Drasi Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package installers

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	drasiapi "drasi.io/cli/api"
	"drasi.io/cli/output"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type KubernetesManifestInstaller struct {
	kubeNamespace      string
	daprRuntimeVersion string
	daprSidecarVersion string
	outputDir          string
}

func MakeKubernetesManifestInstaller(namespace string, outputDir string) (*KubernetesManifestInstaller, error) {
	result := KubernetesManifestInstaller{
		kubeNamespace:      namespace,
		daprRuntimeVersion: DAPR_RUNTIME_VERSION,
		daprSidecarVersion: DAPR_SIDECAR_VERSION,
		outputDir:          outputDir,
	}

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	return &result, nil
}

func (t *KubernetesManifestInstaller) SetDaprRuntimeVersion(version string) {
	t.daprRuntimeVersion = version
}

func (t *KubernetesManifestInstaller) SetDaprSidecarVersion(version string) {
	t.daprSidecarVersion = version
}

func (t *KubernetesManifestInstaller) Install(localMode bool, acr string, version string, output output.TaskOutput, daprRegistry string, observabilityLevel string) error {
	var kubernetesManifests []*unstructured.Unstructured
	var drasiManifests []drasiapi.Manifest

	output.AddTask("Manifest-Generation", "Generating installation manifests...")

	// Generate namespace manifest
	namespaceManifest, err := t.generateNamespaceManifest()
	if err != nil {
		output.FailTask("Manifest-Generation", "Error generating namespace manifest")
		return err
	}
	kubernetesManifests = append(kubernetesManifests, namespaceManifest)

	// Generate config manifest
	configManifest, err := t.generateConfigManifest(localMode, acr, version)
	if err != nil {
		output.FailTask("Manifest-Generation", "Error generating config manifest")
		return err
	}
	kubernetesManifests = append(kubernetesManifests, configManifest)

	// Generate infrastructure manifests
	infraManifests, err := t.generateInfrastructureManifests()
	if err != nil {
		output.FailTask("Manifest-Generation", "Error generating infrastructure manifests")
		return err
	}
	kubernetesManifests = append(kubernetesManifests, infraManifests...)

	// Generate observability manifests
	observabilityManifests, err := t.generateObservabilityManifests(observabilityLevel)
	if err != nil {
		output.FailTask("Manifest-Generation", "Error generating observability manifests")
		return err
	}
	kubernetesManifests = append(kubernetesManifests, observabilityManifests...)

	// Generate control plane manifests
	controlPlaneManifests, err := t.generateControlPlaneManifests(localMode, acr, version)
	if err != nil {
		output.FailTask("Manifest-Generation", "Error generating control plane manifests")
		return err
	}
	kubernetesManifests = append(kubernetesManifests, controlPlaneManifests...)

	// Generate query container manifest (Drasi resource)
	queryContainerManifest, err := t.generateQueryContainerManifest()
	if err != nil {
		output.FailTask("Manifest-Generation", "Error generating query container manifest")
		return err
	}
	drasiManifests = append(drasiManifests, queryContainerManifest...)

	// Generate default source providers (Drasi resources)
	sourceProviderManifests, err := t.generateDefaultSourceProviders()
	if err != nil {
		output.FailTask("Manifest-Generation", "Error generating source provider manifests")
		return err
	}
	drasiManifests = append(drasiManifests, sourceProviderManifests...)

	// Generate default reaction providers (Drasi resources)
	reactionProviderManifests, err := t.generateDefaultReactionProviders()
	if err != nil {
		output.FailTask("Manifest-Generation", "Error generating reaction provider manifests")
		return err
	}
	drasiManifests = append(drasiManifests, reactionProviderManifests...)

	// Write Kubernetes manifests to file
	if err := t.writeKubernetesManifests(kubernetesManifests); err != nil {
		output.FailTask("Manifest-Generation", "Error writing Kubernetes manifests")
		return err
	}

	// Write Drasi manifests to file
	if err := t.writeDrasiManifests(drasiManifests); err != nil {
		output.FailTask("Manifest-Generation", "Error writing Drasi manifests")
		return err
	}

	output.SucceedTask("Manifest-Generation", fmt.Sprintf("Manifests generated in %s", t.outputDir))

	return nil
}

func (t *KubernetesManifestInstaller) generateNamespaceManifest() (*unstructured.Unstructured, error) {
	namespace := &unstructured.Unstructured{}
	namespace.SetAPIVersion("v1")
	namespace.SetKind("Namespace")
	namespace.SetName(t.kubeNamespace)

	labels := map[string]string{
		"drasi.io/namespace": "true",
	}
	namespace.SetLabels(labels)

	return namespace, nil
}

func (t *KubernetesManifestInstaller) generateConfigManifest(localMode bool, acr string, version string) (*unstructured.Unstructured, error) {
	cfg := map[string]interface{}{}

	if localMode {
		cfg["IMAGE_PULL_POLICY"] = "IfNotPresent"
		cfg["IMAGE_VERSION_TAG"] = version
	} else {
		cfg["ACR"] = acr
		cfg["IMAGE_VERSION_TAG"] = version
		cfg["IMAGE_PULL_POLICY"] = "IfNotPresent"
	}

	cfg["DAPR_SIDECAR"] = "daprio/daprd:" + t.daprSidecarVersion

	configMap := &unstructured.Unstructured{}
	configMap.SetAPIVersion("v1")
	configMap.SetKind("ConfigMap")
	configMap.SetName("drasi-config")
	configMap.SetNamespace(t.kubeNamespace)

	if err := unstructured.SetNestedMap(configMap.Object, cfg, "data"); err != nil {
		return nil, err
	}

	return configMap, nil
}

func (t *KubernetesManifestInstaller) generateInfrastructureManifests() ([]*unstructured.Unstructured, error) {
	raw, err := resources.ReadFile("resources/infra.yaml")
	if err != nil {
		return nil, err
	}

	manifests, err := readK8sManifests(raw)
	if err != nil {
		return nil, err
	}

	// Set namespace for all manifests
	for _, manifest := range manifests {
		if manifest.GetNamespace() == "" {
			manifest.SetNamespace(t.kubeNamespace)
		}
	}

	return manifests, nil
}

func (t *KubernetesManifestInstaller) generateObservabilityManifests(observabilityLevel string) ([]*unstructured.Unstructured, error) {
	var manifests []*unstructured.Unstructured

	if observabilityLevel == "none" {
		return manifests, nil
	}

	fileName := map[string]string{
		"tracing": "tracing.yaml",
		"metrics": "metrics.yaml",
		"full":    "full-observability.yaml",
	}[observabilityLevel]

	raw, err := resources.ReadFile("resources/observability/" + fileName)
	if err != nil {
		return nil, err
	}

	observabilityManifests, err := readK8sManifests(raw)
	if err != nil {
		return nil, err
	}
	manifests = append(manifests, observabilityManifests...)

	// Add otel-collector
	raw, err = resources.ReadFile("resources/observability/otel-collector.yaml")
	if err != nil {
		return nil, err
	}
	otelManifests, err := readK8sManifests(raw)
	if err != nil {
		return nil, err
	}
	manifests = append(manifests, otelManifests...)

	// Set namespace for all manifests
	for _, manifest := range manifests {
		if manifest.GetNamespace() == "" {
			manifest.SetNamespace(t.kubeNamespace)
		}
	}

	return manifests, nil
}

func (t *KubernetesManifestInstaller) generateControlPlaneManifests(localMode bool, acr string, version string) ([]*unstructured.Unstructured, error) {
	var manifests []*unstructured.Unstructured

	// Service account manifests
	raw, err := resources.ReadFile("resources/service-account.yaml")
	if err != nil {
		return nil, err
	}

	svcAcctManifests, err := readK8sManifests(raw)
	if err != nil {
		return nil, err
	}
	manifests = append(manifests, svcAcctManifests...)

	// Control plane manifests
	raw, err = resources.ReadFile("resources/control-plane.yaml")
	if err != nil {
		return nil, err
	}

	rawStr := strings.Replace(string(raw), "%TAG%", version, -1)
	if localMode {
		rawStr = strings.Replace(rawStr, "%ACR%", "", -1)
		rawStr = strings.Replace(rawStr, "%IMAGE_PULL_POLICY%", "IfNotPresent", -1)
	} else {
		rawStr = strings.Replace(rawStr, "%ACR%", acr+"/", -1)
		rawStr = strings.Replace(rawStr, "%IMAGE_PULL_POLICY%", "Always", -1)
	}

	daprVersionString := "daprio/daprd:" + t.daprSidecarVersion
	rawStr = strings.Replace(rawStr, "%DAPRD_VERSION%", daprVersionString, -1)
	raw = []byte(rawStr)

	controlPlaneManifests, err := readK8sManifests(raw)
	if err != nil {
		return nil, err
	}
	manifests = append(manifests, controlPlaneManifests...)

	// Set namespace for all manifests
	for _, manifest := range manifests {
		if manifest.GetNamespace() == "" {
			manifest.SetNamespace(t.kubeNamespace)
		}
	}

	return manifests, nil
}

func (t *KubernetesManifestInstaller) generateQueryContainerManifest() ([]drasiapi.Manifest, error) {
	raw, err := resources.ReadFile("resources/default-container.yaml")
	if err != nil {
		return nil, err
	}

	manifests, err := drasiapi.ReadManifests(raw)
	if err != nil {
		return nil, err
	}

	return *manifests, nil
}

func (t *KubernetesManifestInstaller) generateDefaultSourceProviders() ([]drasiapi.Manifest, error) {
	raw, err := resources.ReadFile("resources/default-source-providers.yaml")
	if err != nil {
		return nil, err
	}

	manifests, err := drasiapi.ReadManifests(raw)
	if err != nil {
		return nil, err
	}

	return *manifests, nil
}

func (t *KubernetesManifestInstaller) generateDefaultReactionProviders() ([]drasiapi.Manifest, error) {
	raw, err := resources.ReadFile("resources/default-reaction-providers.yaml")
	if err != nil {
		return nil, err
	}

	manifests, err := drasiapi.ReadManifests(raw)
	if err != nil {
		return nil, err
	}

	return *manifests, nil
}

func (t *KubernetesManifestInstaller) writeKubernetesManifests(manifests []*unstructured.Unstructured) error {
	var buffer bytes.Buffer

	for i, manifest := range manifests {
		if i > 0 {
			buffer.WriteString("---\n")
		}

		manifestYAML, err := yaml.Marshal(manifest.Object)
		if err != nil {
			return fmt.Errorf("failed to marshal Kubernetes manifest: %w", err)
		}

		buffer.Write(manifestYAML)
	}

	filePath := filepath.Join(t.outputDir, "kubernetes-resources.yaml")
	if err := os.WriteFile(filePath, buffer.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write Kubernetes manifests to file: %w", err)
	}

	return nil
}

func (t *KubernetesManifestInstaller) writeDrasiManifests(manifests []drasiapi.Manifest) error {
	var buffer bytes.Buffer

	for i, manifest := range manifests {
		if i > 0 {
			buffer.WriteString("---\n")
		}

		manifestYAML, err := yaml.Marshal(manifest)
		if err != nil {
			return fmt.Errorf("failed to marshal Drasi manifest: %w", err)
		}

		buffer.Write(manifestYAML)
	}

	filePath := filepath.Join(t.outputDir, "drasi-resources.yaml")
	if err := os.WriteFile(filePath, buffer.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write Drasi manifests to file: %w", err)
	}

	return nil
}
