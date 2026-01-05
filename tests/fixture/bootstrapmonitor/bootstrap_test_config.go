// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
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

// The sync mode of a bootstrap configuration should be as explicit as possible to ensure
// unambiguous names if/when new sync modes become supported.
//
// For example, at time of writing only the C-Chain supports state sync. The temptation
// might be to call this mode that tests C-Chain state sync `state-sync`. But if the P-
// or X-Chains start supporting state sync in the future, a name like
// `c-chain-state-sync` ensures that logs and metrics for historical results can be
// differentiated from new results that involve state sync of the other chains.
type SyncMode string

const (
	FullSync           SyncMode = "full-sync"
	CChainStateSync    SyncMode = "c-chain-state-sync"     // aka state sync
	OnlyPChainFullSync SyncMode = "p-chain-full-sync-only" // aka partial sync

	VersionsAnnotationKey = "avalanche.avax.network/avalanchego-versions"
)

var (
	chainConfigContentEnvName        = config.EnvVarName(config.EnvPrefix, config.ChainConfigContentKey)
	networkEnvName                   = config.EnvVarName(config.EnvPrefix, config.NetworkNameKey)
	partialSyncPrimaryNetworkEnvName = config.EnvVarName(config.EnvPrefix, config.PartialSyncPrimaryNetworkKey)

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
	Network  string            `json:"network"`
	SyncMode SyncMode          `json:"syncMode"`
	Image    string            `json:"image"`
	Versions *version.Versions `json:"versions,omitempty"`
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

	// Determine the sync mode from the env vars
	syncMode, err := syncModeFromEnvVars(nodeContainer.Env)
	if err != nil {
		return nil, err
	}

	testConfig := &BootstrapTestConfig{
		Network:  network,
		SyncMode: syncMode,
		Image:    nodeContainer.Image,
	}

	// Attempt to retrieve the image versions from a pod annotation. The annotation may not be populated in
	// the case of a newly-created bootstrap test using an image tagged `master` that hasn't yet had a
	// chance to discover the versions.
	if versionsAnnotation := pod.Annotations[VersionsAnnotationKey]; len(versionsAnnotation) > 0 {
		if err := json.Unmarshal([]byte(versionsAnnotation), &testConfig.Versions); err != nil {
			return nil, fmt.Errorf("%w: %w", errFailedToUnmarshalAnnoation, err)
		}
	}

	return testConfig, nil
}

// syncModeFromEnvVars derives the bootstrap sync mode from the provided environment variables.
func syncModeFromEnvVars(env []corev1.EnvVar) (SyncMode, error) {
	partialSyncPrimaryNetwork, err := partialSyncEnabledFromEnvVars(env)
	if err != nil {
		return "", err
	}
	if partialSyncPrimaryNetwork {
		// If partial sync is enabled, only the P-Chain will be synced so the state sync
		// configuration of the C-Chain is irrelevant.
		return OnlyPChainFullSync, nil
	}
	stateSyncEnabled, err := stateSyncEnabledFromEnvVars(env)
	if err != nil {
		return "", err
	}
	if !stateSyncEnabled {
		// Full sync is enabled
		return FullSync, nil
	}

	// C-Chain state sync is assumed if the other modes are not explicitly enabled
	return CChainStateSync, nil
}

// partialSyncEnabledFromEnvVars determines whether the env vars configure partial sync
// for a node container. Partial sync is assumed to be enabled if the
// AVAGO_PARTIAL_SYNC_PRIMARY_NETWORK env var is set and evaluates to true.
func partialSyncEnabledFromEnvVars(env []corev1.EnvVar) (bool, error) {
	var rawPartialSyncPrimaryNetwork string
	for _, envVar := range env {
		if envVar.Name == partialSyncPrimaryNetworkEnvName {
			rawPartialSyncPrimaryNetwork = envVar.Value
			break
		}
	}
	if len(rawPartialSyncPrimaryNetwork) == 0 {
		return false, nil
	}

	partialSyncPrimaryNetwork, err := cast.ToBoolE(rawPartialSyncPrimaryNetwork)
	if err != nil {
		return false, fmt.Errorf("%w (%v): %w", errFailedToCastToBool, rawPartialSyncPrimaryNetwork, err)
	}
	return partialSyncPrimaryNetwork, nil
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
