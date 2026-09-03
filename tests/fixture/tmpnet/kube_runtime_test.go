// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"

	"github.com/ava-labs/avalanchego/config"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
)

func TestHasReadyServiceEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		endpoint discoveryv1.Endpoint
		port     *int32
		expected bool
	}{
		{
			name:     "ready endpoint for HTTP port",
			endpoint: discoveryv1.Endpoint{Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)}},
			port:     ptr.To(int32(config.DefaultHTTPPort)),
			expected: true,
		},
		{
			name:     "unready endpoint",
			endpoint: discoveryv1.Endpoint{Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(false)}},
			port:     ptr.To(int32(config.DefaultHTTPPort)),
		},
		{
			name:     "endpoint for another port",
			endpoint: discoveryv1.Endpoint{Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)}},
			port:     ptr.To(int32(config.DefaultHTTPPort + 1)),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			slices := []discoveryv1.EndpointSlice{{
				AddressType: discoveryv1.AddressTypeIPv4,
				Endpoints:   []discoveryv1.Endpoint{test.endpoint},
				Ports: []discoveryv1.EndpointPort{{
					Name:     ptr.To("http"),
					Port:     test.port,
					Protocol: ptr.To(corev1.ProtocolTCP),
				}},
			}}

			require.Equal(t, test.expected, hasReadyServiceEndpoint(slices, config.DefaultHTTPPort))
		})
	}
}
