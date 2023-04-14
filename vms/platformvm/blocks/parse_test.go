// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var preFundedKeys = secp256k1.TestKeys()

func TestStandardBlocks(t *testing.T) {
	// check Apricot standard block can be built and parsed
	require := require.New(t)
	blkTimestamp := time.Now()
	parentID := ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'I', 'D'}
	height := uint64(2022)

	for _, ver := range []uint16{Version, ContinuousStakingVersion} {
		txs, err := testDecisionTxs(ver)
		require.NoError(err)

		for _, cdc := range []codec.Manager{Codec, GenesisCodec} {
			if ver == Version { // Apricot blocks are built only with Version
				// build block
				apricotStandardBlk, err := NewApricotStandardBlock(parentID, height, txs)
				require.NoError(err)

				// parse block
				parsed, err := Parse(cdc, apricotStandardBlk.Bytes())
				require.NoError(err)

				// compare content
				require.Equal(apricotStandardBlk.ID(), parsed.ID())
				require.Equal(apricotStandardBlk.Bytes(), parsed.Bytes())
				require.Equal(apricotStandardBlk.Parent(), parsed.Parent())
				require.Equal(apricotStandardBlk.Height(), parsed.Height())

				_, ok := parsed.(*ApricotStandardBlock)
				require.True(ok)
				require.Equal(txs, parsed.Txs())
			}

			// check that banff standard block can be built and parsed
			banffStandardBlk, err := NewBanffStandardBlock(
				ver,
				blkTimestamp,
				parentID,
				height,
				txs,
			)
			require.NoError(err)

			// parse block
			parsed, err := Parse(cdc, banffStandardBlk.Bytes())
			require.NoError(err)

			// compare content
			require.Equal(banffStandardBlk.ID(), parsed.ID())
			require.Equal(banffStandardBlk.Bytes(), parsed.Bytes())
			require.Equal(banffStandardBlk.Parent(), parsed.Parent())
			require.Equal(banffStandardBlk.Height(), parsed.Height())
			parsedBanffStandardBlk, ok := parsed.(*BanffStandardBlock)
			require.True(ok)
			require.Equal(txs, parsedBanffStandardBlk.Txs())

			// timestamp check for banff blocks only
			require.Equal(banffStandardBlk.Timestamp(), parsedBanffStandardBlk.Timestamp())
		}
	}
}

func TestProposalBlocks(t *testing.T) {
	// check Apricot proposal block can be built and parsed
	require := require.New(t)
	blkTimestamp := time.Now()
	parentID := ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'I', 'D'}
	height := uint64(2022)

	for _, ver := range []uint16{Version, ContinuousStakingVersion} {
		tx, err := testProposalTx(ver)
		require.NoError(err)

		for _, cdc := range []codec.Manager{Codec, GenesisCodec} {
			if ver == Version { // Apricot blocks are built only with Version
				// build block
				apricotProposalBlk, err := NewApricotProposalBlock(
					parentID,
					height,
					tx,
				)
				require.NoError(err)

				// parse block
				parsed, err := Parse(cdc, apricotProposalBlk.Bytes())
				require.NoError(err)

				// compare content
				require.Equal(apricotProposalBlk.ID(), parsed.ID())
				require.Equal(apricotProposalBlk.Bytes(), parsed.Bytes())
				require.Equal(apricotProposalBlk.Parent(), parsed.Parent())
				require.Equal(apricotProposalBlk.Height(), parsed.Height())

				parsedApricotProposalBlk, ok := parsed.(*ApricotProposalBlock)
				require.True(ok)
				require.Equal([]*txs.Tx{tx}, parsedApricotProposalBlk.Txs())
			}

			// check that banff proposal block can be built and parsed
			banffProposalBlk, err := NewBanffProposalBlock(
				ver,
				blkTimestamp,
				parentID,
				height,
				tx,
			)
			require.NoError(err)

			// parse block
			parsed, err := Parse(cdc, banffProposalBlk.Bytes())
			require.NoError(err)

			// compare content
			require.Equal(banffProposalBlk.ID(), parsed.ID())
			require.Equal(banffProposalBlk.Bytes(), parsed.Bytes())
			require.Equal(banffProposalBlk.Parent(), banffProposalBlk.Parent())
			require.Equal(banffProposalBlk.Height(), parsed.Height())
			parsedBanffProposalBlk, ok := parsed.(*BanffProposalBlock)
			require.True(ok)
			require.Equal([]*txs.Tx{tx}, parsedBanffProposalBlk.Txs())

			// timestamp check for banff blocks only
			require.Equal(banffProposalBlk.Timestamp(), parsedBanffProposalBlk.Timestamp())
		}
	}
}

func TestCommitBlock(t *testing.T) {
	// check Apricot commit block can be built and parsed
	require := require.New(t)
	blkTimestamp := time.Now()
	parentID := ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'I', 'D'}
	height := uint64(2022)

	for _, ver := range []uint16{Version, ContinuousStakingVersion} {
		for _, cdc := range []codec.Manager{Codec, GenesisCodec} {
			if ver == Version { // Apricot blocks are built only with Version
				// build block
				apricotCommitBlk, err := NewApricotCommitBlock(parentID, height)
				require.NoError(err)

				// parse block
				parsed, err := Parse(cdc, apricotCommitBlk.Bytes())
				require.NoError(err)

				// compare content
				require.Equal(apricotCommitBlk.ID(), parsed.ID())
				require.Equal(apricotCommitBlk.Bytes(), parsed.Bytes())
				require.Equal(apricotCommitBlk.Parent(), parsed.Parent())
				require.Equal(apricotCommitBlk.Height(), parsed.Height())
			}

			// check that banff commit block can be built and parsed
			banffCommitBlk, err := NewBanffCommitBlock(
				ver,
				blkTimestamp,
				parentID,
				height,
			)
			require.NoError(err)

			// parse block
			parsed, err := Parse(cdc, banffCommitBlk.Bytes())
			require.NoError(err)

			// compare content
			require.Equal(banffCommitBlk.ID(), parsed.ID())
			require.Equal(banffCommitBlk.Bytes(), parsed.Bytes())
			require.Equal(banffCommitBlk.Parent(), banffCommitBlk.Parent())
			require.Equal(banffCommitBlk.Height(), parsed.Height())

			// timestamp check for banff blocks only
			parsedBanffCommitBlk, ok := parsed.(*BanffCommitBlock)
			require.True(ok)
			require.Equal(banffCommitBlk.Timestamp(), parsedBanffCommitBlk.Timestamp())
		}
	}
}

func TestAbortBlock(t *testing.T) {
	// check Apricot abort block can be built and parsed
	require := require.New(t)
	blkTimestamp := time.Now()
	parentID := ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'I', 'D'}
	height := uint64(2022)

	for _, ver := range []uint16{Version, ContinuousStakingVersion} {
		for _, cdc := range []codec.Manager{Codec, GenesisCodec} {
			if ver == Version { // Apricot blocks are built only with Version
				apricotAbortBlk, err := NewApricotAbortBlock(parentID, height)
				require.NoError(err)

				// parse block
				parsed, err := Parse(cdc, apricotAbortBlk.Bytes())
				require.NoError(err)

				// compare content
				require.Equal(apricotAbortBlk.ID(), parsed.ID())
				require.Equal(apricotAbortBlk.Bytes(), parsed.Bytes())
				require.Equal(apricotAbortBlk.Parent(), parsed.Parent())
				require.Equal(apricotAbortBlk.Height(), parsed.Height())
			}

			// check that banff abort block can be built and parsed
			banffAbortBlk, err := NewBanffAbortBlock(
				ver,
				blkTimestamp,
				parentID,
				height,
			)
			require.NoError(err)

			// parse block
			parsed, err := Parse(cdc, banffAbortBlk.Bytes())
			require.NoError(err)

			// compare content
			require.Equal(banffAbortBlk.ID(), parsed.ID())
			require.Equal(banffAbortBlk.Bytes(), parsed.Bytes())
			require.Equal(banffAbortBlk.Parent(), banffAbortBlk.Parent())
			require.Equal(banffAbortBlk.Height(), parsed.Height())

			// timestamp check for banff blocks only
			parsedBanffAbortBlk, ok := parsed.(*BanffAbortBlock)
			require.True(ok)
			require.Equal(banffAbortBlk.Timestamp(), parsedBanffAbortBlk.Timestamp())
		}
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
		atomicBlk, err := NewApricotAtomicBlock(
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

		parsedAtomicBlk, ok := parsed.(*ApricotAtomicBlock)
		require.True(ok)
		require.Equal([]*txs.Tx{tx}, parsedAtomicBlk.Txs())
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
	signers := [][]*secp256k1.PrivateKey{{preFundedKeys[0]}}
	return txs.NewSigned(utx, txs.Codec, txs.Version, signers)
}

func testDecisionTxs(version uint16) ([]*txs.Tx, error) {
	countTxs := 2
	decisionTxs := make([]*txs.Tx, 0, countTxs)
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

		signers := [][]*secp256k1.PrivateKey{{preFundedKeys[0]}}
		tx, err := txs.NewSigned(utx, txs.Codec, version, signers)
		if err != nil {
			return nil, err
		}
		decisionTxs = append(decisionTxs, tx)
	}
	return decisionTxs, nil
}

func testProposalTx(version uint16) (*txs.Tx, error) {
	utx := &txs.RewardValidatorTx{
		TxID: ids.ID{'r', 'e', 'w', 'a', 'r', 'd', 'I', 'D'},
	}

	signers := [][]*secp256k1.PrivateKey{{preFundedKeys[0]}}
	return txs.NewSigned(utx, txs.Codec, version, signers)
}
