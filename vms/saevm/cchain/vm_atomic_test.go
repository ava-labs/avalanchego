// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package corethvm

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/rpc"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
)

// TestExportTx adds an atomic export and verifies that the exported UTXO is in shared memory.
func TestExportTx(t *testing.T) {
	t.Skip("failing") // TODO: FIXME

	sut := newSUT(t)

	tests := []struct {
		name      string
		amount    uint64
		destChain ids.ID
	}{
		{
			name:      "P Chain",
			amount:    1,
			destChain: sut.snowCtx.SubnetID,
		},
		{
			name:      "X Chain",
			amount:    1,
			destChain: sut.snowCtx.XChainID,
		},
		{
			name:      "Random Chain",
			amount:    1,
			destChain: ids.GenerateTestID(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exportTx := sut.issueExportTx(t, tt.destChain, tt.amount)

			// Tx added build time
			b := sut.buildAndVerifyBlock(t, nil)
			opts, err := sut.vm.hooks.EndOfBlockOps(b.EthBlock())
			require.NoError(t, err)
			require.Len(t, opts, 1)

			// Result should be available after execution
			sut.acceptAndExecuteBlock(t, b)
			sm := sut.atomicMemory.NewSharedMemory(tt.destChain)
			indexedValues, _, _, err := sm.Indexed(sut.snowCtx.ChainID, [][]byte{sut.atomicKey.Address().Bytes()}, nil, nil, 3)
			require.NoError(t, err)
			require.Len(t, indexedValues, 1)

			// Exact UTXO should be in shared memory
			_, req, err := exportTx.AtomicRequests() // codec isn't exported, can still get UTXO marshaled like this
			require.NoError(t, err)
			require.Len(t, req.PutRequests, 1)
			require.Equal(t, req.PutRequests[0].Value, indexedValues[0])

			// Check nonce for address, it should be incremented by 1
			prevBlock := new(big.Int).Sub(b.Number(), common.Big1)
			startNonce, err := sut.client.NonceAt(sut.ctx, sut.atomicKey.EthAddress(), prevBlock)
			require.NoError(t, err)
			endNonce, err := sut.client.NonceAt(sut.ctx, sut.atomicKey.EthAddress(), b.Number())
			require.NoError(t, err)
			require.Equal(t, startNonce+1, endNonce)
		})
	}
}

func (s *SUT) issueExportTx(t *testing.T, chainID ids.ID, amount uint64) *tx.Tx {
	id, err := s.vm.LastAccepted(s.ctx)
	require.NoError(t, err)

	b := s.vm.GethRPCBackends()
	state, _, err := b.StateAndHeaderByNumberOrHash(
		s.ctx, rpc.BlockNumberOrHashWithHash(common.Hash(id), true),
	)
	require.NoErrorf(t, err, "%T.StateAndHeaderByNumberOrHash()", b)

	// TODO: Failing because eth_baseFee is returning null
	var hex hexutil.Big
	require.NoError(t, s.client.Client().CallContext(s.ctx, &hex, "eth_baseFee"), "eth_baseFee")
	baseFee := (*big.Int)(&hex)

	rules := params.TestDurangoChainConfig.Rules(new(big.Int), params.IsMergeTODO, 0)

	// TODO(alarso16): Use new atomic code. This is identical after marshaled.
	corethTx, err := atomic.NewExportTx(
		s.snowCtx,
		*params.GetRulesExtra(rules),
		extstate.New(state),
		s.snowCtx.AVAXAssetID,
		amount,
		chainID,
		s.atomicKey.Address(),
		baseFee,
		[]*secp256k1.PrivateKey{s.atomicKey},
	)
	require.NoError(t, err)
	txBytes := corethTx.SignedBytes()

	res := &api.JSONTxID{}
	txStr, err := formatting.Encode(formatting.Hex, txBytes)
	require.NoError(t, err)
	require.NoError(t, s.avaxClient.Call(res, "avax.issueTx", &api.FormattedTx{
		Tx:       txStr,
		Encoding: formatting.Hex,
	}))

	// Copy into new Tx type for better testing.
	newTx, err := tx.Parse(txBytes)
	require.NoError(t, err)

	return newTx
}
