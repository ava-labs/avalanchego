// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	chainsatomic "github.com/ava-labs/avalanchego/chains/atomic"
)

func TestImportUnsigned(t *testing.T) {
	var (
		cChainID    = ids.GenerateTestID()
		avaxAssetID = ids.GenerateTestID()
		owner       = common.Address{1}
		utxoID      = avax.UTXOID{TxID: ids.GenerateTestID()}
		amount      = MaxUnsignedImportBurn * 10
	)
	utxo := func(locktime uint64, threshold uint32) *avax.UTXO {
		return &avax.UTXO{
			UTXOID: utxoID,
			Asset:  avax.Asset{ID: avaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: amount,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  locktime,
					Threshold: threshold,
					Addrs:     []ids.ShortID{ids.ShortID(owner)},
				},
			},
		}
	}
	imp := func(amt uint64, to common.Address, out uint64) *Tx {
		return &Tx{Unsigned: &Import{
			SourceChain: constants.PlatformChainID,
			ImportedInputs: []*avax.TransferableInput{{
				UTXOID: utxoID,
				Asset:  avax.Asset{ID: avaxAssetID},
				In:     &secp256k1fx.TransferInput{Amt: amt},
			}},
			Outs: []Output{{Address: to, Amount: out, AssetID: avaxAssetID}},
		}}
	}

	tests := []struct {
		name    string
		utxo    *avax.UTXO
		tx      *Tx
		wantErr error
	}{
		{name: "valid", utxo: utxo(EVMOwnerLocktime, 1), tx: imp(amount, owner, amount-1)},
		{name: "legacy_utxo_needs_signature", utxo: utxo(0, 1), tx: imp(amount, owner, amount-1), wantErr: errNotEVMOwned},
		{name: "multisig_utxo", utxo: utxo(EVMOwnerLocktime, 2), tx: imp(amount, owner, amount-1), wantErr: errNotEVMOwned},
		{name: "wrong_recipient", utxo: utxo(EVMOwnerLocktime, 1), tx: imp(amount, common.Address{2}, amount-1), wantErr: errUnsignedOutputOwner},
		{name: "burns_too_much", utxo: utxo(EVMOwnerLocktime, 1), tx: imp(amount, owner, amount-MaxUnsignedImportBurn-1), wantErr: errUnsignedBurnTooHigh},
		{name: "amount_mismatch", utxo: utxo(EVMOwnerLocktime, 1), tx: imp(amount-10, owner, amount-11), wantErr: errUnsignedAmount},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			memory := chainsatomic.NewMemory(memdb.New())
			utxoBytes, err := MarshalUTXO(tt.utxo)
			require.NoError(t, err)
			inputID := utxoID.InputID()
			require.NoError(t, memory.NewSharedMemory(constants.PlatformChainID).Apply(map[ids.ID]*chainsatomic.Requests{
				cChainID: {PutRequests: []*chainsatomic.Element{{Key: inputID[:], Value: utxoBytes}}},
			}))
			err = tt.tx.VerifyCredentials(memory.NewSharedMemory(cChainID))
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}
