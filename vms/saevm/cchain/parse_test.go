// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
)

// TestParseBlock verifies that the cchain ParseBlock override accepts
// well-formed blocks and rejects blocks whose extData does not match the
// ExtDataHash committed in the header.
func TestParseBlock(t *testing.T) {
	ctx, sut := newSUT(t)

	w := newWallet(txtest.NewKey(t), snowtest.Context(t, snowtest.CChainID), nil)
	tx1 := w.newMinimalTx(t)

	tests := []struct {
		name    string
		block   *types.Block
		wantErr error
		wantTxs []ids.ID
	}{
		{
			name:    "valid",
			block:   newBlock(t, 1, common.Hash{}, tx1),
			wantTxs: []ids.ID{tx1.ID()},
		},
		{
			name:    "valid_empty",
			block:   newBlock(t, 1, common.Hash{}),
			wantTxs: []ids.ID{},
		},
		{
			name:    "extdata_hash_mismatch",
			block:   newTamperedBlock(t, 1, common.Hash{}, tx1),
			wantErr: errExtDataHashMismatch,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf, err := rlp.EncodeToBytes(tt.block)
			require.NoError(t, err, "rlp.EncodeToBytes(block)")

			got, err := sut.ParseBlock(ctx, buf)
			require.ErrorIs(t, err, tt.wantErr, "vm.ParseBlock(buf)")
			if tt.wantErr != nil {
				return
			}

			gotTxs, err := tx.ParseSlice(customtypes.BlockExtData(got.EthBlock()))
			require.NoError(t, err, "tx.ParseSlice(extData)")

			gotIDs := make([]ids.ID, len(gotTxs))
			for i, txn := range gotTxs {
				gotIDs[i] = txn.ID()
			}
			require.Equal(t, tt.wantTxs, gotIDs, "parsed cross-chain tx IDs")
		})
	}
}
