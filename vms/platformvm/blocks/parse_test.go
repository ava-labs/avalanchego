// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	transactions "github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var preFundedKeys = crypto.BuildTestKeys()

func TestStandardBlocks(t *testing.T) {
	// check Apricot standard block can be built and parsed
	assert := assert.New(t)
	blkTimestamp := time.Now()
	parentID := ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'I', 'D'}
	height := uint64(2022)
	txs, err := testDecisionTxs()
	assert.NoError(err)

	for _, cdc := range []codec.Manager{Codec, GenesisCodec} {
		// build block
		apricotStandardBlk, err := NewApricotStandardBlock(parentID, height, txs)
		assert.NoError(err)

		// parse block
		parsedBlueberry, err := Parse(cdc, apricotStandardBlk.Bytes())
		assert.NoError(err)

		// compare content
		assert.Equal(apricotStandardBlk.ID(), parsedBlueberry.ID())
		assert.Equal(apricotStandardBlk.Bytes(), parsedBlueberry.Bytes())
		assert.Equal(apricotStandardBlk.Parent(), parsedBlueberry.Parent())
		assert.Equal(apricotStandardBlk.Height(), parsedBlueberry.Height())

		// timestamp is not serialized in apricot blocks
		// no matter if block is built with a non-zero timestamp
		assert.Equal(time.Unix(0, 0), parsedBlueberry.BlockTimestamp())

		parsedApricot, ok := parsedBlueberry.(*ApricotStandardBlock)
		assert.True(ok)
		assert.Equal(txs, parsedApricot.Txs())

		// check that blueberry standard block can be built and parsed
		blueberryStandardBlk, err := NewBlueberryStandardBlock(blkTimestamp, parentID, height, txs)
		assert.NoError(err)

		// parse block
		parsedBlueberry, err = Parse(cdc, blueberryStandardBlk.Bytes())
		assert.NoError(err)

		// compare content
		assert.Equal(blueberryStandardBlk.ID(), parsedBlueberry.ID())
		assert.Equal(blueberryStandardBlk.Bytes(), parsedBlueberry.Bytes())
		assert.Equal(blueberryStandardBlk.Parent(), blueberryStandardBlk.Parent())
		assert.Equal(blueberryStandardBlk.Height(), parsedBlueberry.Height())
		assert.Equal(blueberryStandardBlk.BlockTimestamp(), parsedBlueberry.BlockTimestamp())
		parsedBlueberryStandardBlk, ok := parsedBlueberry.(*BlueberryStandardBlock)
		assert.True(ok)
		assert.Equal(txs, parsedBlueberryStandardBlk.Txs())

		// backward compatibility check
		assert.Equal(parsedApricot.Txs(), parsedBlueberryStandardBlk.Txs())
	}
}

func TestProposalBlocks(t *testing.T) {
	// check Apricot proposal block can be built and parsed
	assert := assert.New(t)
	blkTimestamp := time.Now()
	parentID := ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'I', 'D'}
	height := uint64(2022)
	tx, err := testProposalTx()
	assert.NoError(err)

	for _, cdc := range []codec.Manager{Codec, GenesisCodec} {
		// build block
		apricotProposalBlk, err := NewApricotProposalBlock(
			parentID,
			height,
			tx,
		)
		assert.NoError(err)

		// parse block
		parsed, err := Parse(cdc, apricotProposalBlk.Bytes())
		assert.NoError(err)

		// compare content
		assert.Equal(apricotProposalBlk.ID(), parsed.ID())
		assert.Equal(apricotProposalBlk.Bytes(), parsed.Bytes())
		assert.Equal(apricotProposalBlk.Parent(), parsed.Parent())
		assert.Equal(apricotProposalBlk.Height(), parsed.Height())

		// timestamp is not serialized in apricot blocks
		// no matter if block is built with a non-zero timestamp
		assert.Equal(time.Unix(0, 0), parsed.BlockTimestamp())

		parsedApricotProposalBlk, ok := parsed.(*ApricotProposalBlock)
		assert.True(ok)
		assert.Equal([]*transactions.Tx{tx}, parsedApricotProposalBlk.Txs())

		// check that blueberry proposal block can be built and parsed
		blueberryProposalBlk, err := NewBlueberryProposalBlock(
			blkTimestamp,
			parentID,
			height,
			tx,
		)
		assert.NoError(err)

		// parse block
		parsed, err = Parse(cdc, blueberryProposalBlk.Bytes())
		assert.NoError(err)

		// compare content
		assert.Equal(blueberryProposalBlk.ID(), parsed.ID())
		assert.Equal(blueberryProposalBlk.Bytes(), parsed.Bytes())
		assert.Equal(blueberryProposalBlk.Parent(), blueberryProposalBlk.Parent())
		assert.Equal(blueberryProposalBlk.Height(), parsed.Height())
		assert.Equal(blueberryProposalBlk.BlockTimestamp(), parsed.BlockTimestamp())
		parsedBlueberryProposalBlk, ok := parsed.(*BlueberryProposalBlock)
		assert.True(ok)
		assert.Equal([]*transactions.Tx{tx}, parsedBlueberryProposalBlk.Txs())

		// backward compatibility check
		assert.Equal(parsedApricotProposalBlk.Txs(), parsedBlueberryProposalBlk.Txs())
	}
}

func TestCommitBlock(t *testing.T) {
	// check Apricot commit block can be built and parsed
	assert := assert.New(t)
	blkTimestamp := time.Now()
	parentID := ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'I', 'D'}
	height := uint64(2022)

	for _, cdc := range []codec.Manager{Codec, GenesisCodec} {
		// build block
		apricotCommitBlk, err := NewApricotCommitBlock(parentID, height)
		assert.NoError(err)

		// parse block
		parsed, err := Parse(cdc, apricotCommitBlk.Bytes())
		assert.NoError(err)

		// compare content
		assert.Equal(apricotCommitBlk.ID(), parsed.ID())
		assert.Equal(apricotCommitBlk.Bytes(), parsed.Bytes())
		assert.Equal(apricotCommitBlk.Parent(), parsed.Parent())
		assert.Equal(apricotCommitBlk.Height(), parsed.Height())

		// timestamp is not serialized in apricot blocks
		// no matter if block is built with a non-zero timestamp
		assert.Equal(time.Unix(0, 0), parsed.BlockTimestamp())

		// check that blueberry commit block can be built and parsed
		blueberryCommitBlk, err := NewBlueberryCommitBlock(blkTimestamp, parentID, height)
		assert.NoError(err)

		// parse block
		parsed, err = Parse(cdc, blueberryCommitBlk.Bytes())
		assert.NoError(err)

		// compare content
		assert.Equal(blueberryCommitBlk.ID(), parsed.ID())
		assert.Equal(blueberryCommitBlk.Bytes(), parsed.Bytes())
		assert.Equal(blueberryCommitBlk.Parent(), blueberryCommitBlk.Parent())
		assert.Equal(blueberryCommitBlk.Height(), parsed.Height())
		assert.Equal(blueberryCommitBlk.BlockTimestamp(), parsed.BlockTimestamp())
	}
}

func TestAbortBlock(t *testing.T) {
	// check Apricot abort block can be built and parsed
	assert := assert.New(t)
	blkTimestamp := time.Now()
	parentID := ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'I', 'D'}
	height := uint64(2022)

	for _, cdc := range []codec.Manager{Codec, GenesisCodec} {
		// build block
		apricotAbortBlk, err := NewApricotAbortBlock(parentID, height)
		assert.NoError(err)

		// parse block
		parsed, err := Parse(cdc, apricotAbortBlk.Bytes())
		assert.NoError(err)

		// compare content
		assert.Equal(apricotAbortBlk.ID(), parsed.ID())
		assert.Equal(apricotAbortBlk.Bytes(), parsed.Bytes())
		assert.Equal(apricotAbortBlk.Parent(), parsed.Parent())
		assert.Equal(apricotAbortBlk.Height(), parsed.Height())

		// timestamp is not serialized in apricot blocks
		// no matter if block is built with a non-zero timestamp
		assert.Equal(time.Unix(0, 0), parsed.BlockTimestamp())

		// check that blueberry abort block can be built and parsed
		blueberryAbortBlk, err := NewBlueberryAbortBlock(blkTimestamp, parentID, height)
		assert.NoError(err)

		// parse block
		parsed, err = Parse(cdc, blueberryAbortBlk.Bytes())
		assert.NoError(err)

		// compare content
		assert.Equal(blueberryAbortBlk.ID(), parsed.ID())
		assert.Equal(blueberryAbortBlk.Bytes(), parsed.Bytes())
		assert.Equal(blueberryAbortBlk.Parent(), blueberryAbortBlk.Parent())
		assert.Equal(blueberryAbortBlk.Height(), parsed.Height())
		assert.Equal(blueberryAbortBlk.BlockTimestamp(), parsed.BlockTimestamp())
	}
}

func TestAtomicBlock(t *testing.T) {
	// check atomic block can be built and parsed
	assert := assert.New(t)
	parentID := ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'I', 'D'}
	height := uint64(2022)
	tx, err := testAtomicTx()
	assert.NoError(err)

	for _, cdc := range []codec.Manager{Codec, GenesisCodec} {
		// build block
		atomicBlk, err := NewApricotAtomicBlock(
			parentID,
			height,
			tx,
		)
		assert.NoError(err)

		// parse block
		parsed, err := Parse(cdc, atomicBlk.Bytes())
		assert.NoError(err)

		// compare content
		assert.Equal(atomicBlk.ID(), parsed.ID())
		assert.Equal(atomicBlk.Bytes(), parsed.Bytes())
		assert.Equal(atomicBlk.Parent(), parsed.Parent())
		assert.Equal(atomicBlk.Height(), parsed.Height())
		assert.Equal(time.Unix(0, 0), parsed.BlockTimestamp())

		parsedAtomicBlk, ok := parsed.(*ApricotAtomicBlock)
		assert.True(ok)
		assert.Equal([]*transactions.Tx{tx}, parsedAtomicBlk.Txs())
	}
}

func testAtomicTx() (*transactions.Tx, error) {
	utx := &transactions.ImportTx{
		BaseTx: transactions.BaseTx{BaseTx: avax.BaseTx{
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
	return transactions.NewSigned(utx, transactions.Codec, signers)
}

func testDecisionTxs() ([]*transactions.Tx, error) {
	countTxs := 2
	txs := make([]*transactions.Tx, 0, countTxs)
	for i := 0; i < countTxs; i++ {
		// Create the tx
		utx := &transactions.CreateChainTx{
			BaseTx: transactions.BaseTx{BaseTx: avax.BaseTx{
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
		tx, err := transactions.NewSigned(utx, transactions.Codec, signers)
		if err != nil {
			return nil, err
		}
		txs = append(txs, tx)
	}
	return txs, nil
}

func testProposalTx() (*transactions.Tx, error) {
	utx := &transactions.RewardValidatorTx{
		TxID: ids.ID{'r', 'e', 'w', 'a', 'r', 'd', 'I', 'D'},
	}

	signers := [][]*crypto.PrivateKeySECP256K1R{{preFundedKeys[0]}}
	return transactions.NewSigned(utx, transactions.Codec, signers)
}
