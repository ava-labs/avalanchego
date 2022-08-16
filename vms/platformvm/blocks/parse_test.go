// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var preFundedKeys = crypto.BuildTestKeys()

func TestStandardBlock(t *testing.T) {
	// check standard block can be built and parsed
	require := require.New(t)
	parentID := ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'I', 'D'}
	height := uint64(2022)
	txs, err := testDecisionTxs()
	require.NoError(err)

	for _, cdc := range []codec.Manager{Codec, GenesisCodec} {
		// build block
		standardBlk, err := NewStandardBlock(
			parentID,
			height,
			txs,
		)
		require.NoError(err)

		// parse block
		parsed, err := Parse(cdc, standardBlk.Bytes())
		require.NoError(err)

		// compare content
		require.Equal(standardBlk.ID(), parsed.ID())
		require.Equal(standardBlk.Bytes(), parsed.Bytes())
		require.Equal(standardBlk.Parent(), parsed.Parent())
		require.Equal(standardBlk.Height(), parsed.Height())

		parsedStandardBlk, ok := parsed.(*StandardBlock)
		require.True(ok)
		require.Equal(txs, parsedStandardBlk.Transactions)
	}
}

func TestProposalBlock(t *testing.T) {
	// check proposal block can be built and parsed
	require := require.New(t)
	parentID := ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'I', 'D'}
	height := uint64(2022)
	tx, err := testProposalTx()
	require.NoError(err)

	for _, cdc := range []codec.Manager{Codec, GenesisCodec} {
		// build block
		proposalBlk, err := NewProposalBlock(
			parentID,
			height,
			tx,
		)
		require.NoError(err)

		// parse block
		parsed, err := Parse(cdc, proposalBlk.Bytes())
		require.NoError(err)

		// compare content
		require.Equal(proposalBlk.ID(), parsed.ID())
		require.Equal(proposalBlk.Bytes(), parsed.Bytes())
		require.Equal(proposalBlk.Parent(), parsed.Parent())
		require.Equal(proposalBlk.Height(), parsed.Height())

		parsedProposalBlk, ok := parsed.(*ProposalBlock)
		require.True(ok)
		require.Equal(tx, parsedProposalBlk.Tx)
	}
}

func TestCommitBlock(t *testing.T) {
	// check commit block can be built and parsed
	require := require.New(t)
	parentID := ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'I', 'D'}
	height := uint64(2022)

	for _, cdc := range []codec.Manager{Codec, GenesisCodec} {
		// build block
		commitBlk, err := NewCommitBlock(parentID, height)
		require.NoError(err)

		// parse block
		parsed, err := Parse(cdc, commitBlk.Bytes())
		require.NoError(err)

		// compare content
		require.Equal(commitBlk.ID(), parsed.ID())
		require.Equal(commitBlk.Bytes(), parsed.Bytes())
		require.Equal(commitBlk.Parent(), parsed.Parent())
		require.Equal(commitBlk.Height(), parsed.Height())
	}
}

func TestAbortBlock(t *testing.T) {
	// check abort block can be built and parsed
	require := require.New(t)
	parentID := ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'I', 'D'}
	height := uint64(2022)

	for _, cdc := range []codec.Manager{Codec, GenesisCodec} {
		// build block
		abortBlk, err := NewAbortBlock(parentID, height)
		require.NoError(err)

		// parse block
		parsed, err := Parse(cdc, abortBlk.Bytes())
		require.NoError(err)

		// compare content
		require.Equal(abortBlk.ID(), parsed.ID())
		require.Equal(abortBlk.Bytes(), parsed.Bytes())
		require.Equal(abortBlk.Parent(), parsed.Parent())
		require.Equal(abortBlk.Height(), parsed.Height())
	}
}

func TestAtomicBlock(t *testing.T) {
	// check atomic block can be built and parsed
	require := require.New(t)
	parentID := ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'I', 'D'}
	height := uint64(2022)
	tx, err := testAtomicTx()
	require.NoError(err)

	for _, cdc := range []codec.Manager{Codec, GenesisCodec} {
		// build block
		atomicBlk, err := NewAtomicBlock(
			parentID,
			height,
			tx,
		)
		require.NoError(err)

		// parse block
		parsed, err := Parse(cdc, atomicBlk.Bytes())
		require.NoError(err)

		// compare content
		require.Equal(atomicBlk.ID(), parsed.ID())
		require.Equal(atomicBlk.Bytes(), parsed.Bytes())
		require.Equal(atomicBlk.Parent(), parsed.Parent())
		require.Equal(atomicBlk.Height(), parsed.Height())

		parsedAtomicBlk, ok := parsed.(*AtomicBlock)
		require.True(ok)
		require.Equal(tx, parsedAtomicBlk.Tx)
	}
}

func testAtomicTx() (*txs.Tx, error) {
	utx := &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    10,
			BlockchainID: ids.ID{'c', 'h', 'a', 'i', 'n', 'I', 'D'},
			Outs: []*avax.TransferableOutput{{
				Asset: avax.Asset{ID: ids.ID{'a', 's', 's', 'e', 'r', 't'}},
				Out: &secp256k1fx.TransferOutput{
					Amt: uint64(1234),
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{preFundedKeys[0].PublicKey().Address()},
					},
				},
			}},
			Ins: []*avax.TransferableInput{{
				UTXOID: avax.UTXOID{
					TxID:        ids.ID{'t', 'x', 'I', 'D'},
					OutputIndex: 2,
				},
				Asset: avax.Asset{ID: ids.ID{'a', 's', 's', 'e', 'r', 't'}},
				In: &secp256k1fx.TransferInput{
					Amt:   uint64(5678),
					Input: secp256k1fx.Input{SigIndices: []uint32{0}},
				},
			}},
			Memo: []byte{1, 2, 3, 4, 5, 6, 7, 8},
		}},
		SourceChain: ids.ID{'c', 'h', 'a', 'i', 'n'},
		ImportedInputs: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{
				TxID:        ids.Empty.Prefix(1),
				OutputIndex: 1,
			},
			Asset: avax.Asset{ID: ids.ID{'a', 's', 's', 'e', 'r', 't'}},
			In: &secp256k1fx.TransferInput{
				Amt:   50000,
				Input: secp256k1fx.Input{SigIndices: []uint32{0}},
			},
		}},
	}
	signers := [][]*crypto.PrivateKeySECP256K1R{{preFundedKeys[0]}}
	return txs.NewSigned(utx, txs.Codec, signers)
}

func testDecisionTxs() ([]*txs.Tx, error) {
	countTxs := 2
	txes := make([]*txs.Tx, 0, countTxs)
	for i := 0; i < countTxs; i++ {
		// Create the tx
		utx := &txs.CreateChainTx{
			BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    10,
				BlockchainID: ids.ID{'c', 'h', 'a', 'i', 'n', 'I', 'D'},
				Outs: []*avax.TransferableOutput{{
					Asset: avax.Asset{ID: ids.ID{'a', 's', 's', 'e', 'r', 't'}},
					Out: &secp256k1fx.TransferOutput{
						Amt: uint64(1234),
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{preFundedKeys[0].PublicKey().Address()},
						},
					},
				}},
				Ins: []*avax.TransferableInput{{
					UTXOID: avax.UTXOID{
						TxID:        ids.ID{'t', 'x', 'I', 'D'},
						OutputIndex: 2,
					},
					Asset: avax.Asset{ID: ids.ID{'a', 's', 's', 'e', 'r', 't'}},
					In: &secp256k1fx.TransferInput{
						Amt:   uint64(5678),
						Input: secp256k1fx.Input{SigIndices: []uint32{0}},
					},
				}},
				Memo: []byte{1, 2, 3, 4, 5, 6, 7, 8},
			}},
			SubnetID:    ids.ID{'s', 'u', 'b', 'n', 'e', 't', 'I', 'D'},
			ChainName:   "a chain",
			VMID:        ids.GenerateTestID(),
			FxIDs:       []ids.ID{ids.GenerateTestID()},
			GenesisData: []byte{'g', 'e', 'n', 'D', 'a', 't', 'a'},
			SubnetAuth:  &secp256k1fx.Input{SigIndices: []uint32{1}},
		}

		signers := [][]*crypto.PrivateKeySECP256K1R{{preFundedKeys[0]}}
		tx, err := txs.NewSigned(utx, txs.Codec, signers)
		if err != nil {
			return nil, err
		}
		txes = append(txes, tx)
	}
	return txes, nil
}

func testProposalTx() (*txs.Tx, error) {
	utx := &txs.RewardValidatorTx{
		TxID: ids.ID{'r', 'e', 'w', 'a', 'r', 'd', 'I', 'D'},
	}

	signers := [][]*crypto.PrivateKeySECP256K1R{{preFundedKeys[0]}}
	return txs.NewSigned(utx, txs.Codec, signers)
}
