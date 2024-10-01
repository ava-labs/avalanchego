// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapmonitor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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

	// Errors for bootstrapTestConfigForPod
	errContainerNotFound          = errors.New("container not found")
	errInvalidNetworkEnvVar       = fmt.Errorf("missing or empty %s env var", networkEnvName)
	errFailedToUnmarshalAnnoation = errors.New("failed to unmarshal versions annotation")

	// Errors for stateSyncEnabledFromEnvVars
	errFailedToDecodeChainConfigContent    = errors.New("failed to decode chain config content")
	errFailedToUnmarshalChainConfigContent = errors.New("failed to unmarshal chain config content")
	errFailedToUnmarshalCChainConfig       = errors.New("failed to unmarshal C-Chain config")
	errFailedToCastToBool                  = errors.New("failed to cast to bool")
)

type BootstrapTestConfig struct {
	Network          string            `json:"network"`
	StateSyncEnabled bool              `json:"stateSyncEnabled"`
	Image            string            `json:"image"`
	Versions         *version.Versions `json:"versions,omitempty"`
}

// GetBootstrapTestConfigFromPod extracts the bootstrap test configuration from the specified pod.
func GetBootstrapTestConfigFromPod(ctx context.Context, clientset *kubernetes.Clientset, namespace string, podName string, nodeContainerName string) (*BootstrapTestConfig, error) {
	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod %s.%s: %w", namespace, podName, err)
	}
	return bootstrapTestConfigForPod(pod, nodeContainerName)
}

// bootstrapTestConfigForPod collects the details for a bootstrap test configuration from the provided pod.
func bootstrapTestConfigForPod(pod *corev1.Pod, nodeContainerName string) (*BootstrapTestConfig, error) {
	// Find the node container
	var nodeContainer corev1.Container
	for _, container := range pod.Spec.Containers {
		if container.Name == nodeContainerName {
			nodeContainer = container
			break
		}
	}
	if len(nodeContainer.Name) == 0 {
		return nil, fmt.Errorf("%w: %s", errContainerNotFound, nodeContainerName)
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
		return nil, fmt.Errorf("%w in container %q", errInvalidNetworkEnvVar, nodeContainerName)
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

	// Attempt to retrieve the image versions from a pod annotation. The annotation may not be populated in
	// the case of a newly-created bootstrap test using an image tagged `latest` that hasn't yet had a
	// chance to discover the versions.
	if versionsAnnotation := pod.Annotations[VersionsAnnotationKey]; len(versionsAnnotation) > 0 {
		if err := json.Unmarshal([]byte(versionsAnnotation), &testConfig.Versions); err != nil {
			return nil, fmt.Errorf("%w: %w", errFailedToUnmarshalAnnoation, err)
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
		return false, fmt.Errorf("%w: %w", errFailedToDecodeChainConfigContent, err)
	}
	if err := json.Unmarshal(chainConfigContent, &chainConfigs); err != nil {
		return false, fmt.Errorf("%w: %w", errFailedToUnmarshalChainConfigContent, err)
	}

	cChainConfig, ok := chainConfigs["C"]
	if !ok {
		return true, nil
	}

	// Attempt to unmarshal the C-Chain config
	var cChainConfigMap map[string]any
	if err := json.Unmarshal(cChainConfig.Config, &cChainConfigMap); err != nil {
		return false, fmt.Errorf("%w: %w", errFailedToUnmarshalCChainConfig, err)
	}

	// Attempt to read the value from the C-Chain config
	rawStateSyncEnabled, ok := cChainConfigMap["state-sync-enabled"]
	if !ok {
		return true, nil
	}
	stateSyncEnabled, err := cast.ToBoolE(rawStateSyncEnabled)
	if err != nil {
		return false, fmt.Errorf("%w (%v): %w", errFailedToCastToBool, rawStateSyncEnabled, err)
	}
	return stateSyncEnabled, nil
}
