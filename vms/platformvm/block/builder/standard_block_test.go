// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/MetalBlockchain/metalgo/chains/atomic"
	"github.com/MetalBlockchain/metalgo/database/prefixdb"
	"github.com/MetalBlockchain/metalgo/ids"
	"github.com/MetalBlockchain/metalgo/upgrade/upgradetest"
	"github.com/MetalBlockchain/metalgo/utils/units"
	"github.com/MetalBlockchain/metalgo/vms/components/avax"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/genesis/genesistest"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/status"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/txs"
	"github.com/MetalBlockchain/metalgo/vms/secp256k1fx"
)

func TestAtomicTxImports(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t, upgradetest.Latest)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	addr := genesistest.DefaultFundedKeys[0].Address()
	owner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{addr},
	}

	m := atomic.NewMemory(prefixdb.New([]byte{5}, env.baseDB))

	env.msm.SharedMemory = m.NewSharedMemory(env.ctx.ChainID)
	peerSharedMemory := m.NewSharedMemory(env.ctx.XChainID)
	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID:        ids.GenerateTestID(),
			OutputIndex: 1,
		},
		Asset: avax.Asset{ID: env.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt:          70 * units.MicroAvax,
			OutputOwners: *owner,
		},
	}
	utxoBytes, err := txs.Codec.Marshal(txs.CodecVersion, utxo)
	require.NoError(err)

	inputID := utxo.InputID()
	require.NoError(peerSharedMemory.Apply(map[ids.ID]*atomic.Requests{
		env.ctx.ChainID: {PutRequests: []*atomic.Element{{
			Key:   inputID[:],
			Value: utxoBytes,
			Traits: [][]byte{
				addr.Bytes(),
			},
		}}},
	}))

	wallet := newWallet(t, env, walletConfig{})

	tx, err := wallet.IssueImportTx(
		env.ctx.XChainID,
		owner,
	)
	require.NoError(err)

	require.NoError(env.Builder.Add(tx))
	b, err := env.Builder.BuildBlock(t.Context())
	require.NoError(err)
	// Test multiple verify calls work
	require.NoError(b.Verify(t.Context()))
	require.NoError(b.Accept(t.Context()))
	_, txStatus, err := env.state.GetTx(tx.ID())
	require.NoError(err)
	// Ensure transaction is in the committed state
	require.Equal(status.Committed, txStatus)
}
