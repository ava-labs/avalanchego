// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	chainsatomic "github.com/ava-labs/avalanchego/chains/atomic"
)

type countingState struct {
	libevm.StateReader
	reads int
}

func (s *countingState) GetState(addr common.Address, key common.Hash, opts ...stateconf.StateDBStateOption) common.Hash {
	s.reads++
	return s.StateReader.GetState(addr, key, opts...)
}

func TestImportContractCredential(t *testing.T) {
	var (
		cChainID    = ids.GenerateTestID()
		avaxAssetID = ids.GenerateTestID()
		helper      = common.Address{0xaa}
		owner       = common.Address{1}
		other       = ids.ShortID{2}
		utxoID      = avax.UTXOID{TxID: ids.GenerateTestID()}
	)
	tests := []struct {
		name    string
		mutate  func(*Tx, *secp256k1fx.TransferOutput, *ImportAuth, *state.StateDB)
		wantErr error
	}{
		{name: "valid"},
		{name: "locktime boundary", mutate: func(_ *Tx, out *secp256k1fx.TransferOutput, a *ImportAuth, _ *state.StateDB) {
			out.Locktime = a.Timestamp
		}},
		{name: "timelocked", mutate: func(_ *Tx, out *secp256k1fx.TransferOutput, a *ImportAuth, _ *state.StateDB) {
			out.Locktime = a.Timestamp + 1
		}, wantErr: secp256k1fx.ErrTimelocked},
		{name: "owner mismatch", mutate: func(_ *Tx, out *secp256k1fx.TransferOutput, _ *ImportAuth, _ *state.StateDB) {
			out.Addrs[0] = other
		}, wantErr: errImportOwnerMismatch},
		{name: "multiple owners", mutate: func(_ *Tx, out *secp256k1fx.TransferOutput, _ *ImportAuth, _ *state.StateDB) {
			out.Addrs = append(out.Addrs, other)
		}, wantErr: errImportOwnerMismatch},
		{name: "zero threshold", mutate: func(_ *Tx, out *secp256k1fx.TransferOutput, _ *ImportAuth, _ *state.StateDB) {
			out.Threshold = 0
			out.Addrs = nil
		}, wantErr: errImportOwnerMismatch},
		{name: "amount mismatch", mutate: func(_ *Tx, out *secp256k1fx.TransferOutput, _ *ImportAuth, _ *state.StateDB) {
			out.Amt++
		}, wantErr: secp256k1fx.ErrMismatchedAmounts},
		{name: "higher fee", mutate: func(tx *Tx, _ *secp256k1fx.TransferOutput, _ *ImportAuth, _ *state.StateDB) {
			tx.Unsigned.(*Import).Outs[0].Amount--
		}, wantErr: errUnauthorizedImport},
		{name: "lower fee", mutate: func(tx *Tx, _ *secp256k1fx.TransferOutput, _ *ImportAuth, _ *state.StateDB) {
			tx.Unsigned.(*Import).Outs[0].Amount++
		}, wantErr: errUnauthorizedImport},
		{name: "other recipient", mutate: func(tx *Tx, _ *secp256k1fx.TransferOutput, _ *ImportAuth, _ *state.StateDB) {
			tx.Unsigned.(*Import).Outs[0].Address = common.Address(other)
		}, wantErr: errUnauthorizedImport},
		{name: "other input", mutate: func(tx *Tx, _ *secp256k1fx.TransferOutput, _ *ImportAuth, _ *state.StateDB) {
			tx.Unsigned.(*Import).ImportedInputs[0].UTXOID.OutputIndex++
		}, wantErr: errUnauthorizedImport},
		{name: "other network", mutate: func(tx *Tx, _ *secp256k1fx.TransferOutput, _ *ImportAuth, _ *state.StateDB) {
			tx.Unsigned.(*Import).NetworkID++
		}, wantErr: errUnauthorizedImport},
		{name: "other destination", mutate: func(tx *Tx, _ *secp256k1fx.TransferOutput, _ *ImportAuth, _ *state.StateDB) {
			tx.Unsigned.(*Import).BlockchainID = ids.GenerateTestID()
		}, wantErr: errUnauthorizedImport},
		{name: "not P-chain", mutate: func(tx *Tx, _ *secp256k1fx.TransferOutput, _ *ImportAuth, _ *state.StateDB) {
			tx.Unsigned.(*Import).SourceChain = ids.GenerateTestID()
		}, wantErr: errContractImportShape},
		{name: "multiple outputs", mutate: func(tx *Tx, _ *secp256k1fx.TransferOutput, _ *ImportAuth, _ *state.StateDB) {
			imp := tx.Unsigned.(*Import)
			imp.Outs = append(imp.Outs, imp.Outs[0])
		}, wantErr: errContractImportShape},
		{name: "unknown helper", mutate: func(_ *Tx, _ *secp256k1fx.TransferOutput, a *ImportAuth, _ *state.StateDB) {
			a.Helper = common.Address{0xbb}
		}, wantErr: errUnauthorizedImport},
		{name: "disabled helper", mutate: func(_ *Tx, _ *secp256k1fx.TransferOutput, a *ImportAuth, _ *state.StateDB) {
			a.Helper = common.Address{}
		}, wantErr: errUnauthorizedImport},
		{name: "missing state", mutate: func(_ *Tx, _ *secp256k1fx.TransferOutput, a *ImportAuth, _ *state.StateDB) {
			a.State = nil
		}, wantErr: errUnauthorizedImport},
		{name: "invalid signature index", mutate: func(tx *Tx, _ *secp256k1fx.TransferOutput, _ *ImportAuth, s *state.StateDB) {
			tx.Unsigned.(*Import).ImportedInputs[0].In.(*secp256k1fx.TransferInput).SigIndices[0] = 1
			unsigned, err := UnsignedBytes(tx.Unsigned)
			require.NoError(t, err)
			s.SetState(helper, ImportApprovalSlot(unsigned), common.Hash{31: 1})
		}, wantErr: errImportOwnerMismatch},
		{name: "noncanonical approval value", mutate: func(tx *Tx, _ *secp256k1fx.TransferOutput, _ *ImportAuth, s *state.StateDB) {
			unsigned, err := UnsignedBytes(tx.Unsigned)
			require.NoError(t, err)
			s.SetState(helper, ImportApprovalSlot(unsigned), common.Hash{31: 2})
		}, wantErr: errUnauthorizedImport},
		{name: "no approval", mutate: func(tx *Tx, _ *secp256k1fx.TransferOutput, _ *ImportAuth, s *state.StateDB) {
			unsigned, err := UnsignedBytes(tx.Unsigned)
			require.NoError(t, err)
			s.SetState(helper, ImportApprovalSlot(unsigned), common.Hash{})
		}, wantErr: errUnauthorizedImport},
		{name: "nil credential", mutate: func(tx *Tx, _ *secp256k1fx.TransferOutput, _ *ImportAuth, _ *state.StateDB) {
			tx.Creds[0] = (*ContractCredential)(nil)
		}, wantErr: errNilContractCredential},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imp := &Import{
				BlockchainID: cChainID,
				SourceChain:  constants.PlatformChainID,
				ImportedInputs: []*avax.TransferableInput{{
					UTXOID: utxoID, Asset: avax.Asset{ID: avaxAssetID},
					In: &secp256k1fx.TransferInput{Amt: 100, Input: secp256k1fx.Input{SigIndices: []uint32{0}}},
				}},
				Outs: []Output{{Address: owner, Amount: 99, AssetID: avaxAssetID}},
			}
			tx := &Tx{Unsigned: imp, Creds: []Credential{&ContractCredential{}}}
			out := &secp256k1fx.TransferOutput{Amt: 100, OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1, Addrs: []ids.ShortID{ids.ShortID(owner)},
			}}
			s, err := state.New(types.EmptyRootHash, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
			require.NoError(t, err)
			unsigned, err := UnsignedBytes(imp)
			require.NoError(t, err)
			s.SetState(helper, ImportApprovalSlot(unsigned), common.Hash{31: 1})
			auth := &ImportAuth{State: s, Helper: helper, Timestamp: 1234}
			if tt.mutate != nil {
				tt.mutate(tx, out, auth, s)
			}
			memory := chainsatomic.NewMemory(memdb.New())
			utxoBytes, err := MarshalUTXO(&avax.UTXO{UTXOID: utxoID, Asset: avax.Asset{ID: avaxAssetID}, Out: out})
			require.NoError(t, err)
			inputID := utxoID.InputID()
			require.NoError(t, memory.NewSharedMemory(constants.PlatformChainID).Apply(map[ids.ID]*chainsatomic.Requests{
				cChainID: {PutRequests: []*chainsatomic.Element{{Key: inputID[:], Value: utxoBytes}}},
			}))
			err = tx.VerifyCredentials(memory.NewSharedMemory(cChainID), auth)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestContractCredentialSizeAndReads(t *testing.T) {
	for _, n := range []int{1, 10, 100} {
		owner, helper := common.Address{1}, common.Address{2}
		chainID, assetID := ids.GenerateTestID(), ids.GenerateTestID()
		imp := &Import{BlockchainID: chainID, SourceChain: constants.PlatformChainID,
			Outs: []Output{{Address: owner, Amount: 1, AssetID: assetID}}}
		tx := &Tx{Unsigned: imp}
		memory := chainsatomic.NewMemory(memdb.New())
		for range n {
			utxo := &avax.UTXO{
				UTXOID: avax.UTXOID{TxID: ids.GenerateTestID()}, Asset: avax.Asset{ID: assetID},
				Out: &secp256k1fx.TransferOutput{Amt: 100, OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1, Addrs: []ids.ShortID{ids.ShortID(owner)},
				}},
			}
			imp.ImportedInputs = append(imp.ImportedInputs, &avax.TransferableInput{
				UTXOID: utxo.UTXOID, Asset: utxo.Asset,
				In: &secp256k1fx.TransferInput{Amt: 100, Input: secp256k1fx.Input{SigIndices: []uint32{0}}},
			})
			tx.Creds = append(tx.Creds, &ContractCredential{})
			b, err := MarshalUTXO(utxo)
			require.NoError(t, err)
			id := utxo.InputID()
			require.NoError(t, memory.NewSharedMemory(constants.PlatformChainID).Apply(map[ids.ID]*chainsatomic.Requests{
				chainID: {PutRequests: []*chainsatomic.Element{{Key: id[:], Value: b}}},
			}))
		}
		unsigned, err := UnsignedBytes(imp)
		require.NoError(t, err)
		encoded, err := tx.Bytes()
		require.NoError(t, err)
		// A four-byte count, then only a four-byte type ID for each input.
		require.Len(t, encoded, len(unsigned)+4+4*n)
		parsed, err := Parse(encoded)
		require.NoError(t, err)
		require.Equal(t, tx.ID(), parsed.ID())
		_, err = Parse(append(append([]byte{}, encoded...), 0, 0, 0, 0))
		require.Error(t, err, "the marker must not accept a payload-length field")
		s, err := state.New(types.EmptyRootHash, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
		require.NoError(t, err)
		s.SetState(helper, ImportApprovalSlot(unsigned), common.Hash{31: 1})
		counted := &countingState{StateReader: s}
		auth := &ImportAuth{State: counted, Helper: helper}
		require.NoError(t, parsed.VerifyCredentials(memory.NewSharedMemory(chainID), auth))
		require.Equal(t, 1, counted.reads)
		baseGas, err := gasUsed(imp)
		require.NoError(t, err)
		op, err := tx.AsOp(assetID)
		require.NoError(t, err)
		require.Equal(t, baseGas+contractAuthGas, op.Gas)
		if n > 1 {
			for _, j := range []int{0, n - 1} {
				parsed.Creds[j] = &secp256k1fx.Credential{}
				require.ErrorIs(t, parsed.VerifyCredentials(memory.NewSharedMemory(chainID), auth), errWrongCredentialType)
				parsed.Creds[j] = &ContractCredential{}
			}
		}
	}
}
