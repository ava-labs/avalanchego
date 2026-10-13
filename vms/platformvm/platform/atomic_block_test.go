// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platform

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

func TestNewApricotAtomicBlock(t *testing.T) {
	require := require.New(t)

	parentID := ids.GenerateTestID()
	height := uint64(1337)
	tx := &Tx{
		Unsigned: &ImportTx{
			BaseTx: BaseTx{
				BaseTx: avax.BaseTx{
					Ins:  []*avax.TransferableInput{},
					Outs: []*avax.TransferableOutput{},
				},
			},
			ImportedInputs: []*avax.TransferableInput{},
		},
		Creds: []verify.Verifiable{},
	}
	require.NoError(tx.Initialize(Codec))

	blk, err := NewApricotAtomicBlock(
		parentID,
		height,
		tx,
	)
	require.NoError(err)

	// Make sure the block and tx are initialized
	require.NotEmpty(blk.Bytes())
	require.NotEmpty(blk.Tx.Bytes())
	require.NotEqual(ids.Empty, blk.Tx.ID())
	require.Equal(tx.Bytes(), blk.Tx.Bytes())
	require.Equal(parentID, blk.Parent())
	require.Equal(height, blk.Height())
}
