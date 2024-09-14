// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapmonitor

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/version"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBootstrapTestConfigForPod(t *testing.T) {
	networkName := "network"
	nodeContainerName := "node"
	imageName := "image"
	validVersionsString := `{"application": "avalanchego/1.11.11", "database": "v1.4.5", "rpcchainvm": 37, "commit": "5bcfb0fb30cc311adb22173daabb56eae736fac3","go": "1.21.12" }`
	invalidVersionsString := "invalid"

	tests := []struct {
		name           string
		pod            *corev1.Pod
		expectedConfig *BootstrapTestConfig
		expectErr      bool
	}{
		{
			name: "container not found",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{},
			},
			expectErr: true,
		},
		{
			name: "missing network id env var",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: nodeContainerName,
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "valid configuration without versions and state sync disabled",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  nodeContainerName,
							Image: imageName,
							Env: []corev1.EnvVar{
								{
									Name:  networkEnvName,
									Value: networkName,
								},
								{
									Name: chainConfigContentEnvName,
									// Sets state-sync-enabled:false for the C-Chain
									Value: "eyJDIjp7IkNvbmZpZyI6ImV5SnpkR0YwWlMxemVXNWpMV1Z1WVdKc1pXUWlPbVpoYkhObGZRPT0iLCJVcGdyYWRlIjpudWxsfX0=",
								},
							},
						},
					},
				},
			},
			expectedConfig: &BootstrapTestConfig{
				Network:          networkName,
				StateSyncEnabled: false,
				Image:            imageName,
			},
		},
		{
			name: "valid configuration with valid versions",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						versionsAnnotationKey: validVersionsString,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  nodeContainerName,
							Image: imageName,
							Env: []corev1.EnvVar{
								{
									Name:  networkEnvName,
									Value: networkName,
								},
							},
						},
					},
				},
			},
			expectedConfig: &BootstrapTestConfig{
				Network:          networkName,
				StateSyncEnabled: true,
				Image:            imageName,
				Versions: &version.Versions{
					Application: "avalanchego/1.11.11",
					Database:    "v1.4.5",
					RPCChainVM:  37,
					Commit:      "5bcfb0fb30cc311adb22173daabb56eae736fac3",
					Go:          "1.21.12",
				},
			},
		},
		{
			name: "invalid configuration due to invalid versions",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						versionsAnnotationKey: invalidVersionsString,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: nodeContainerName,
							Env: []corev1.EnvVar{
								{
									Name:  networkEnvName,
									Value: networkName,
								},
							},
						},
					},
				},
			},
			expectErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			config, err := bootstrapTestConfigForPod(test.pod, nodeContainerName)
			if test.expectErr {
				require.Error(err)
				return
			}
			require.NoError(err)
			require.Equal(test.expectedConfig, config)
		})
	}
}

func marshalAndEncode(t *testing.T, chainConfigs map[string]chains.ChainConfig) string {
	chainConfigContent, err := json.Marshal(chainConfigs)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(chainConfigContent)
}

func TestStateSyncEnabledFromEnvVars(t *testing.T) {
	invalidJSON := "asdf"
	tests := []struct {
		name               string
		chainConfigContent string
		expectedEnabled    bool
		expectErr          bool
	}{
		{
			name:               "no chain config",
			chainConfigContent: "",
			expectedEnabled:    true,
		},
		{
			name: "no C-Chain config",
			chainConfigContent: marshalAndEncode(t,
				map[string]chains.ChainConfig{
					"Not-C": chains.ChainConfig{},
				},
			),
			expectedEnabled: true,
		},
		{
			name:               "invalid encoded content",
			chainConfigContent: invalidJSON,
			expectErr:          true,
		},
		{
			name:               "invalid json content",
			chainConfigContent: base64.StdEncoding.EncodeToString([]byte(invalidJSON)),
			expectErr:          true,
		},
		{
			name: "invalid C-Chain config",
			chainConfigContent: marshalAndEncode(t,
				map[string]chains.ChainConfig{
					"C": chains.ChainConfig{
						Config: []byte(invalidJSON)},
				},
			),
			expectErr: true,
		},
		{
			name: "empty C-Chain config",
			chainConfigContent: marshalAndEncode(t,
				map[string]chains.ChainConfig{
					"C": chains.ChainConfig{
						Config: []byte("{}"),
					},
				},
			),
			expectedEnabled: true,
		},
		{
			name: "invalid state sync value",
			chainConfigContent: marshalAndEncode(t,
				map[string]chains.ChainConfig{
					"C": chains.ChainConfig{
						Config: []byte("{\"state-sync-enabled\":1234}"),
					},
				},
			),
			expectErr: true,
		},
		{
			name: "C-Chain config with state sync enabled",
			chainConfigContent: marshalAndEncode(t,
				map[string]chains.ChainConfig{
					"C": chains.ChainConfig{
						Config: []byte("{\"state-sync-enabled\":true}"),
					},
				},
			),
			expectedEnabled: true,
		},
		{
			name: "C-Chain config with state sync disabled",
			chainConfigContent: marshalAndEncode(t,
				map[string]chains.ChainConfig{
					"C": chains.ChainConfig{
						Config: []byte("{\"state-sync-enabled\":false}"),
					},
				},
			),
			expectedEnabled: false,
		},
		{
			name: "C-Chain config with state sync disabled with string bool",
			chainConfigContent: marshalAndEncode(t,
				map[string]chains.ChainConfig{
					"C": chains.ChainConfig{
						Config: []byte("{\"state-sync-enabled\":\"false\"}"),
					},
				},
			),
			expectedEnabled: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			env := []corev1.EnvVar{
				{
					Name:  chainConfigContentEnvName,
					Value: test.chainConfigContent,
				},
			}
			enabled, err := stateSyncEnabledFromEnvVars(env)
			if test.expectErr {
				require.Error(err)
				return
			}
			require.NoError(err)
			require.Equal(test.expectedEnabled, enabled)
		})
	}
}
