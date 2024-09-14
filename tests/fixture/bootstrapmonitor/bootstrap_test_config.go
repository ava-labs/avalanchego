// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapmonitor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/spf13/cast"
	"k8s.io/client-go/kubernetes"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/version"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const VersionsAnnotationKey = "avalanche.avax.network/avalanchego-versions"

var (
	chainConfigContentEnvName = config.EnvVarName(config.EnvPrefix, config.ChainConfigContentKey)
	networkEnvName            = config.EnvVarName(config.EnvPrefix, config.NetworkNameKey)
)

type BootstrapTestConfig struct {
	Network          string
	StateSyncEnabled bool
	Image            string
	Versions         *version.Versions
}

// GetBootstrapTestConfigFromPod extracts the bootstrap test configuration from the specified pod.
func GetBootstrapTestConfigFromPod(ctx context.Context, clientset *kubernetes.Clientset, namespace string, podName string, nodeContainerName string) (*BootstrapTestConfig, error) {
	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod %s.%s: %w", namespace, podName, err)
	}
	return bootstrapTestConfigForPod(pod, nodeContainerName)
}

// Attempt to retrieve the image versions from a pod annotation. The annotation
// may not be present in the case of a newly-created bootstrap test using an
// image tagged `latest` that hasn't yet had a chance to discover the versions.
func bootstrapTestConfigForPod(pod *corev1.Pod, nodeContainerName string) (*BootstrapTestConfig, error) {
	// Find the node container
	var nodeContainer *corev1.Container
	for _, container := range pod.Spec.Containers {
		if container.Name == nodeContainerName {
			nodeContainer = &container
			break
		}
	}
	if nodeContainer == nil {
		return nil, fmt.Errorf("%q container not found", nodeContainerName)
	}

	// Get the network ID from the container's environment
	var network string
	for _, envVar := range nodeContainer.Env {
		if envVar.Name == networkEnvName {
			network = envVar.Value
			break
		}
	}
	if len(network) == 0 {
		return nil, fmt.Errorf("%s env var missing for %q container", networkEnvName, nodeContainerName)
	}

	// Determine whether state sync is enabled in the env vars
	stateSyncEnabled, err := stateSyncEnabledFromEnvVars(nodeContainer.Env)
	if err != nil {
		return nil, err
	}

	testConfig := &BootstrapTestConfig{
		Network:          network,
		StateSyncEnabled: stateSyncEnabled,
		Image:            nodeContainer.Image,
	}

	// Attempt to retrieve the image versions from a pod annotation. The annotation
	// may not be present in the case of a newly-created bootstrap test using an
	// image tagged `latest` that hasn't yet had a chance to discover the versions.
	if versionsAnnotation, ok := pod.Annotations[VersionsAnnotationKey]; ok && len(versionsAnnotation) > 0 {
		testConfig.Versions = &version.Versions{}
		if err := json.Unmarshal([]byte(versionsAnnotation), testConfig.Versions); err != nil {
			return nil, fmt.Errorf("failed to unmarshal versions annotation: %w", err)
		}
	}

	return testConfig, nil
}

// stateSyncEnabledFromEnvVars determines whether the env vars configure state sync for a
// node container. State sync is assumed to be enabled if the chain config content is
// missing, does not contain C-Chain configuration, or the C-Chain configuration does not
// configure state-sync-enabled.
func stateSyncEnabledFromEnvVars(env []corev1.EnvVar) (bool, error) {
	// Look for chain config content in the env vars
	var encodedChainConfigContent string
	for _, envVar := range env {
		if envVar.Name == chainConfigContentEnvName {
			encodedChainConfigContent = envVar.Value
			break
		}
	}

	if len(encodedChainConfigContent) == 0 {
		return true, nil
	}

	// Attempt to unmarshal
	var chainConfigs map[string]chains.ChainConfig
	chainConfigContent, err := base64.StdEncoding.DecodeString(encodedChainConfigContent)
	if err != nil {
		return false, fmt.Errorf("failed to decode chain config content: %w", err)
	}
	if err := json.Unmarshal(chainConfigContent, &chainConfigs); err != nil {
		return false, fmt.Errorf("failed to unmarshal chain config content: %w", err)
	}

	cChainConfig, ok := chainConfigs["C"]
	if !ok {
		return true, nil
	}

	// Attempt to unmarshal the C-Chain config
	var cChainConfigMap map[string]any
	if err := json.Unmarshal(cChainConfig.Config, &cChainConfigMap); err != nil {
		return false, fmt.Errorf("failed to unmarshal C chain config: %w", err)
	}

	// Attempt to read the value from the C-Chain config
	rawStateSyncEnabled, ok := cChainConfigMap["state-sync-enabled"]
	if !ok {
		return true, nil
	}
	stateSyncEnabled, err := cast.ToBoolE(rawStateSyncEnabled)
	if err != nil {
		return false, fmt.Errorf("failed to cast %v to bool: %w", rawStateSyncEnabled, err)
	}
	return stateSyncEnabled, nil
}
