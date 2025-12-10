// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nodetest

import (
	"testing"
	"github.com/stretchr/testify/require"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm"
	"net/netip"
	"github.com/ava-labs/avalanchego/ids"
)

func TestFoo(t *testing.T) {
	evm.RegisterAllLibEVMExtras()

	bootstrapper := New(t, Config{})
	go func() {
		require.NoError(t, bootstrapper.Start(t.Context()))
	}()

	n := New(t, Config{
		BootstrapperIPs: []netip.AddrPort{bootstrapper.IP()},
		BootstrapperIDs: []ids.NodeID{bootstrapper.ID()},
	})

	// ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	// defer cancel()

	// require.NoError(t, n.Start(ctx))
	require.NoError(t, n.Start(t.Context()))
}
