// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/test"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
)

func newCaminoBuilder(
	t *testing.T,
	state state.State,
	sharedMemory atomic.SharedMemory,
	phase test.Phase,
) *caminoBuilder {
	t.Helper()

	config := test.Config(t, phase)
	clk := test.Clock()
	baseDB := versiondb.New(memdb.New())
	ctx := test.ContextWithSharedMemory(t, baseDB)
	fx := test.Fx(t, clk, ctx.Log, true)
	if sharedMemory != nil {
		ctx.SharedMemory = sharedMemory
	}

	txBuilder := NewCamino(
		ctx,
		config,
		clk,
		fx,
		state,
		avax.NewAtomicUTXOManager(ctx.SharedMemory, txs.Codec),
		utxo.NewHandler(ctx, clk, fx),
	)

	caminoBuilder, ok := txBuilder.(*caminoBuilder)
	require.True(t, ok, "not camino builder")

	t.Cleanup(func() {
		require.NoError(t, baseDB.Close())
	})

	return caminoBuilder
}
