// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utxo

import (
	"testing"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/stretchr/testify/require"
)

func TestVerifySpendUTXOsWithLocked(t *testing.T) {
	fx := &secp256k1fx.Fx{}

	err := fx.InitializeVM(&secp256k1fx.TestVM{})
	require.NoError(t, err)

	err = fx.Bootstrapped()
	require.NoError(t, err)

	testHandler := &handler{
		ctx: snow.DefaultContextTest(),
		clk: &mockable.Clock{},
		utxosReader: avax.NewUTXOState(
			memdb.New(),
			txs.Codec,
		),
		fx: fx,
	}
	assetID := testHandler.ctx.AVAXAssetID

	tx := &dummyUnsignedTx{txs.BaseTx{}}
	tx.Initialize([]byte{0})

	outputOwners, cred1 := generateOwnersAndSig(tx)
	sigIndices := []uint32{0}
	lockTxID := ids.GenerateTestID()

	// Note that setting [chainTimestamp] also set's the VM's clock.
	// Adjust input/output locktimes accordingly.
	tests := map[string]struct {
		utxos           []*avax.UTXO
		ins             []*avax.TransferableInput
		outs            []*avax.TransferableOutput
		creds           []verify.Verifiable
		producedAmounts map[ids.ID]uint64
		expectErr       bool
	}{
		"ok": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, assetID, 10, outputOwners, ids.Empty, ids.Empty),
			},
			ins: []*avax.TransferableInput{
				generateTestIn(assetID, 10, ids.Empty, ids.Empty, sigIndices),
			},
			outs: []*avax.TransferableOutput{
				generateTestOut(assetID, 10, outputOwners, ids.Empty, ids.Empty),
			},
			producedAmounts: map[ids.ID]uint64{},
			creds:           []verify.Verifiable{cred1},
			expectErr:       false,
		},
		"utxos have locked.Out": {
			utxos: []*avax.UTXO{
				generateTestUTXO(ids.ID{1}, assetID, 10, outputOwners, lockTxID, ids.Empty),
			},
			ins: []*avax.TransferableInput{
				generateTestIn(assetID, 10, ids.Empty, ids.Empty, sigIndices),
			},
			outs: []*avax.TransferableOutput{
				generateTestOut(assetID, 10, outputOwners, ids.Empty, ids.Empty),
			},
			producedAmounts: map[ids.ID]uint64{},
			creds:           []verify.Verifiable{cred1},
			expectErr:       true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := testHandler.VerifySpendUTXOs(
				tx,
				test.utxos,
				test.ins,
				test.outs,
				test.creds,
				test.producedAmounts,
			)
			require.True(t, test.expectErr == (err != nil))
		})
	}
}
