// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
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

func TestRepriceUnsigned(t *testing.T) {
	var (
		avaxAssetID = ids.GenerateTestID()
		owner       = common.Address{1}
		amount      = uint64(units.Avax)
	)
	imp := func(out uint64) *Tx {
		return &Tx{Unsigned: &Import{
			SourceChain: constants.PlatformChainID,
			ImportedInputs: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()},
				Asset:  avax.Asset{ID: avaxAssetID},
				In:     &secp256k1fx.TransferInput{Amt: amount},
			}},
			Outs: []Output{{Address: owner, Amount: out, AssetID: avaxAssetID}},
		}}
	}
	baseFee := uint256.NewInt(1_500_000_000) // 1.5 nAVAX per gas

	posted := imp(amount - MaxUnsignedImportBurn) // poster asked for the maximum fee
	canonical, err := posted.RepriceUnsigned(baseFee)
	require.NoError(t, err)
	gas, err := gasUsed(canonical.Unsigned)
	require.NoError(t, err)
	wantBurn := (uint64(gas)*baseFee.Uint64() + _x2cRate - 1) / _x2cRate
	require.Equal(t, amount-wantBurn, canonical.Unsigned.(*Import).Outs[0].Amount)
	require.Less(t, wantBurn, MaxUnsignedImportBurn/100, "base-fee pricing is far below the posted cap")

	// Repricing the canonical tx again is a no-op, which is what lets verifiers
	// rebuild a block byte for byte.
	again, err := canonical.RepriceUnsigned(baseFee)
	require.NoError(t, err)
	require.Equal(t, canonical.ID(), again.ID())

	// Signed txs pass through untouched.
	signed := imp(amount - 1)
	signed.Creds = []Credential{&secp256k1fx.Credential{}}
	same, err := signed.RepriceUnsigned(baseFee)
	require.NoError(t, err)
	require.Same(t, signed, same)

	// A UTXO that cannot cover the fee is rejected.
	dust := &Tx{Unsigned: &Import{
		SourceChain:    constants.PlatformChainID,
		ImportedInputs: []*avax.TransferableInput{{UTXOID: avax.UTXOID{}, Asset: avax.Asset{ID: avaxAssetID}, In: &secp256k1fx.TransferInput{Amt: 10}}},
		Outs:           []Output{{Address: owner, Amount: 1, AssetID: avaxAssetID}},
	}}
	_, err = dust.RepriceUnsigned(baseFee)
	require.ErrorIs(t, err, errUnsignedDust)
}
