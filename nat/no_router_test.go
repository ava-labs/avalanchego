// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nat

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNoRouter(t *testing.T) {
	t.Parallel()

	ip := netip.MustParseAddr("1.2.3.4")

	tests := []struct {
		name    string
		router  noRouter
		wantIP  netip.Addr
		wantErr error
	}{
		{
			name:   "with resolved ip",
			router: noRouter{ip: ip},
			wantIP: ip,
		},
		{
			name:    "with resolution error",
			router:  noRouter{ipErr: errTest},
			wantErr: errTest,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require := require.New(t)

			require.False(tt.router.SupportsNAT())

			err := tt.router.MapPort(testIntPort, testExtPort, testDesc, mapTimeout)
			require.ErrorIs(err, errNoRouterCantMapPorts)

			require.NoError(tt.router.UnmapPort(testIntPort, testExtPort))

			externalIP, err := tt.router.ExternalIP()
			require.ErrorIs(err, tt.wantErr)
			require.Equal(tt.wantIP, externalIP)
		})
	}
}
