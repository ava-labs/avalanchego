// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateless

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/stretchr/testify/assert"
)

var preFundedKeys = crypto.BuildTestKeys()

func TestStandardBlocks(t *testing.T) {
	// check Apricot standard block can be built and parsed
	assert := assert.New(t)
	blkTimestamp := uint64(time.Now().Unix())
	parentID := ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'I', 'D'}
	height := uint64(2022)
	txs, err := testDecisionTxs()
	assert.NoError(err)

	for _, cdc := range []codec.Manager{Codec, GenesisCodec} {
		// build block
		apricotStandardBlk, err := NewStandardBlock(
			uint16(ApricotVersion),
			blkTimestamp,
			parentID,
			height,
			txs,
		)
		assert.NoError(err)

		// parse block
		parsed, err := Parse(apricotStandardBlk.Bytes(), cdc)
		assert.NoError(err)

		// compare content
		assert.Equal(apricotStandardBlk.ID(), parsed.ID())
		assert.Equal(apricotStandardBlk.Bytes(), parsed.Bytes())
		assert.Equal(apricotStandardBlk.Parent(), parsed.Parent())
		assert.Equal(apricotStandardBlk.Height(), parsed.Height())
		assert.Equal(apricotStandardBlk.Version(), parsed.Version())

		// timestamp is not serialized in apricot blocks
		// no matter if block is built with a non-zero timestamp
		assert.Equal(int64(0), parsed.UnixTimestamp())

		parsedApricotStandardBlk, ok := parsed.(*ApricotStandardBlock)
		assert.True(ok)
		assert.Equal(txs, parsedApricotStandardBlk.DecisionTxs())

		// check that blueberry standard block can be built and parsed
		blueberryStandardBlk, err := NewStandardBlock(
			uint16(BlueberryVersion),
			blkTimestamp,
			parentID,
			height,
			txs,
		)
		assert.NoError(err)

		// parse block
		parsed, err = Parse(blueberryStandardBlk.Bytes(), cdc)
		assert.NoError(err)

		// compare content
		assert.Equal(blueberryStandardBlk.ID(), parsed.ID())
		assert.Equal(blueberryStandardBlk.Bytes(), parsed.Bytes())
		assert.Equal(blueberryStandardBlk.Parent(), blueberryStandardBlk.Parent())
		assert.Equal(blueberryStandardBlk.Height(), parsed.Height())
		assert.Equal(blueberryStandardBlk.Version(), parsed.Version())
		assert.Equal(blueberryStandardBlk.UnixTimestamp(), parsed.UnixTimestamp())
		parsedBlueberryStandardBlk, ok := parsed.(*BlueberryStandardBlock)
		assert.True(ok)
		assert.Equal(txs, parsedBlueberryStandardBlk.DecisionTxs())

		// backward compatibility check
		assert.Equal(parsedApricotStandardBlk.DecisionTxs(), parsedBlueberryStandardBlk.DecisionTxs())
	}
}

func TestProposalBlocks(t *testing.T) {
	// check Apricot proposal block can be built and parsed
	assert := assert.New(t)
	blkTimestamp := uint64(time.Now().Unix())
	parentID := ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'I', 'D'}
	height := uint64(2022)
	tx, err := testProposalTx()
	assert.NoError(err)

	for _, cdc := range []codec.Manager{Codec, GenesisCodec} {
		// build block
		apricotProposalBlk, err := NewProposalBlock(
			uint16(ApricotVersion),
			blkTimestamp,
			parentID,
			height,
			tx,
		)
		assert.NoError(err)

		// parse block
		parsed, err := Parse(apricotProposalBlk.Bytes(), cdc)
		assert.NoError(err)

		// compare content
		assert.Equal(apricotProposalBlk.ID(), parsed.ID())
		assert.Equal(apricotProposalBlk.Bytes(), parsed.Bytes())
		assert.Equal(apricotProposalBlk.Parent(), parsed.Parent())
		assert.Equal(apricotProposalBlk.Height(), parsed.Height())
		assert.Equal(apricotProposalBlk.Version(), parsed.Version())

		// timestamp is not serialized in apricot blocks
		// no matter if block is built with a non-zero timestamp
		assert.Equal(int64(0), parsed.UnixTimestamp())

		parsedApricotProposalBlk, ok := parsed.(*ApricotProposalBlock)
		assert.True(ok)
		assert.Equal(tx, parsedApricotProposalBlk.ProposalTx())

		// check that blueberry proposal block can be built and parsed
		blueberryProposalBlk, err := NewProposalBlock(
			uint16(BlueberryVersion),
			blkTimestamp,
			parentID,
			height,
			tx,
		)
		assert.NoError(err)

		// parse block
		parsed, err = Parse(blueberryProposalBlk.Bytes(), cdc)
		assert.NoError(err)

		// compare content
		assert.Equal(blueberryProposalBlk.ID(), parsed.ID())
		assert.Equal(blueberryProposalBlk.Bytes(), parsed.Bytes())
		assert.Equal(blueberryProposalBlk.Parent(), blueberryProposalBlk.Parent())
		assert.Equal(blueberryProposalBlk.Height(), parsed.Height())
		assert.Equal(blueberryProposalBlk.Version(), parsed.Version())
		assert.Equal(blueberryProposalBlk.UnixTimestamp(), parsed.UnixTimestamp())
		parsedBlueberryProposalBlk, ok := parsed.(*BlueberryProposalBlock)
		assert.True(ok)
		assert.Equal(tx, parsedBlueberryProposalBlk.ProposalTx())

		// backward compatibility check
		assert.Equal(parsedApricotProposalBlk.ProposalTx(), parsedBlueberryProposalBlk.ProposalTx())
	}
}

func TestCommitBlock(t *testing.T) {
	// check Apricot commit block can be built and parsed
	assert := assert.New(t)
	blkTimestamp := uint64(time.Now().Unix())
	parentID := ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'I', 'D'}
	height := uint64(2022)

	for _, cdc := range []codec.Manager{Codec, GenesisCodec} {
		// build block
		apricotCommitBlk, err := NewCommitBlock(
			uint16(ApricotVersion),
			blkTimestamp,
			parentID,
			height,
		)
		assert.NoError(err)

		// parse block
		parsed, err := Parse(apricotCommitBlk.Bytes(), cdc)
		assert.NoError(err)

		// compare content
		assert.Equal(apricotCommitBlk.ID(), parsed.ID())
		assert.Equal(apricotCommitBlk.Bytes(), parsed.Bytes())
		assert.Equal(apricotCommitBlk.Parent(), parsed.Parent())
		assert.Equal(apricotCommitBlk.Height(), parsed.Height())
		assert.Equal(apricotCommitBlk.Version(), parsed.Version())

		// timestamp is not serialized in apricot blocks
		// no matter if block is built with a non-zero timestamp
		assert.Equal(int64(0), parsed.UnixTimestamp())

		// check that blueberry commit block can be built and parsed
		blueberryCommitBlk, err := NewCommitBlock(
			uint16(BlueberryVersion),
			blkTimestamp,
			parentID,
			height,
		)
		assert.NoError(err)

		// parse block
		parsed, err = Parse(blueberryCommitBlk.Bytes(), cdc)
		assert.NoError(err)

		// compare content
		assert.Equal(blueberryCommitBlk.ID(), parsed.ID())
		assert.Equal(blueberryCommitBlk.Bytes(), parsed.Bytes())
		assert.Equal(blueberryCommitBlk.Parent(), blueberryCommitBlk.Parent())
		assert.Equal(blueberryCommitBlk.Height(), parsed.Height())
		assert.Equal(blueberryCommitBlk.Version(), parsed.Version())
		assert.Equal(blueberryCommitBlk.UnixTimestamp(), parsed.UnixTimestamp())
	}
}

func TestAbortBlock(t *testing.T) {
	// check Apricot abort block can be built and parsed
	assert := assert.New(t)
	blkTimestamp := uint64(time.Now().Unix())
	parentID := ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'I', 'D'}
	height := uint64(2022)

	for _, cdc := range []codec.Manager{Codec, GenesisCodec} {
		// build block
		apricotAbortBlk, err := NewAbortBlock(
			uint16(ApricotVersion),
			blkTimestamp,
			parentID,
			height,
		)
		assert.NoError(err)

		// parse block
		parsed, err := Parse(apricotAbortBlk.Bytes(), cdc)
		assert.NoError(err)

		// compare content
		assert.Equal(apricotAbortBlk.ID(), parsed.ID())
		assert.Equal(apricotAbortBlk.Bytes(), parsed.Bytes())
		assert.Equal(apricotAbortBlk.Parent(), parsed.Parent())
		assert.Equal(apricotAbortBlk.Height(), parsed.Height())
		assert.Equal(apricotAbortBlk.Version(), parsed.Version())

		// timestamp is not serialized in apricot blocks
		// no matter if block is built with a non-zero timestamp
		assert.Equal(int64(0), parsed.UnixTimestamp())

		// check that blueberry abort block can be built and parsed
		blueberryAbortBlk, err := NewAbortBlock(
			uint16(BlueberryVersion),
			blkTimestamp,
			parentID,
			height,
		)
		assert.NoError(err)

		// parse block
		parsed, err = Parse(blueberryAbortBlk.Bytes(), cdc)
		assert.NoError(err)

		// compare content
		assert.Equal(blueberryAbortBlk.ID(), parsed.ID())
		assert.Equal(blueberryAbortBlk.Bytes(), parsed.Bytes())
		assert.Equal(blueberryAbortBlk.Parent(), blueberryAbortBlk.Parent())
		assert.Equal(blueberryAbortBlk.Height(), parsed.Height())
		assert.Equal(blueberryAbortBlk.Version(), parsed.Version())
		assert.Equal(blueberryAbortBlk.UnixTimestamp(), parsed.UnixTimestamp())
	}
}

func TestAtomicBlocks(t *testing.T) {
	// check atomic block can be built and parsed
	assert := assert.New(t)
	parentID := ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'I', 'D'}
	height := uint64(2022)
	tx, err := testAtomicTx()
	assert.NoError(err)

	for _, cdc := range []codec.Manager{Codec, GenesisCodec} {
		// build block
		atomicBlk, err := NewAtomicBlock(
			parentID,
			height,
			tx,
		)
		assert.NoError(err)

		// parse block
		parsed, err := Parse(atomicBlk.Bytes(), cdc)
		assert.NoError(err)

		// compare content
		assert.Equal(atomicBlk.ID(), parsed.ID())
		assert.Equal(atomicBlk.Bytes(), parsed.Bytes())
		assert.Equal(atomicBlk.Parent(), parsed.Parent())
		assert.Equal(atomicBlk.Height(), parsed.Height())
		assert.Equal(atomicBlk.Version(), parsed.Version())
		assert.Equal(int64(0), parsed.UnixTimestamp())

		parsedAtomicBlk, ok := parsed.(*AtomicBlock)
		assert.True(ok)
		assert.Equal(tx, parsedAtomicBlk.AtomicTx())
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
