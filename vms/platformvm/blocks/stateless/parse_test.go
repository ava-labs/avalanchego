// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateless

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/stretchr/testify/assert"
)

var preFundedKeys []*crypto.PrivateKeySECP256K1R

func init() {
	dummyCtx := snow.DefaultContextTest()
	preFundedKeys = make([]*crypto.PrivateKeySECP256K1R, 0)
	factory := crypto.FactorySECP256K1R{}
	for _, key := range []string{
		"24jUJ9vZexUM6expyMcT48LBx27k1m7xpraoV62oSQAHdziao5",
		"2MMvUMsxx6zsHSNXJdFD8yc5XkancvwyKPwpw4xUK3TCGDuNBY",
		"cxb7KpGWhDMALTjNNSJ7UQkkomPesyWAPUaWRGdyeBNzR6f35",
		"ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN",
		"2RWLv6YVEXDiWLpaCbXhhqxtLbnFaKQsWPSSMSPhpWo47uJAeV",
	} {
		privKeyBytes, err := formatting.Decode(formatting.CB58, key)
		dummyCtx.Log.AssertNoError(err)
		pk, err := factory.ToPrivateKey(privKeyBytes)
		dummyCtx.Log.AssertNoError(err)
		preFundedKeys = append(preFundedKeys, pk.(*crypto.PrivateKeySECP256K1R))
	}
}

func TestStandardBlocks(t *testing.T) {
	// check preFork standard block can be built and parsed
	assert := assert.New(t)
	preForkblkVersion := uint16(PreForkVersion)
	blkTimestamp := uint64(time.Now().Unix())
	parentID := ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'I', 'D'}
	height := uint64(2022)
	txs, err := testDecisionTxs()
	assert.NoError(err)

	// build block
	preForkStdBlk, err := NewStandardBlock(
		preForkblkVersion,
		blkTimestamp,
		parentID,
		height,
		txs,
	)
	assert.NoError(err)

	// parse block
	parsed, err := Parse(preForkStdBlk.Bytes())
	assert.NoError(err)

	// compare content
	assert.Equal(preForkStdBlk.ID(), parsed.ID())
	assert.Equal(preForkStdBlk.Bytes(), parsed.Bytes())
	assert.Equal(preForkStdBlk.Parent(), parsed.Parent())
	assert.Equal(preForkStdBlk.Height(), parsed.Height())
	assert.Equal(preForkStdBlk.Version(), parsed.Version())

	// timestamp is not serialized in pre fork blocks
	// no matter if block is built with a non-zero timestamp
	assert.Equal(int64(0), parsed.UnixTimestamp())

	parsedPreForkStdBlk, ok := parsed.(*StandardBlock)
	assert.True(ok)
	assert.Equal(txs, parsedPreForkStdBlk.Txs)

	// check that post fork standard block can be built and parsed
	postForkBlkVersion := uint16(PostForkVersion)

	// build block
	postForkStdBlk, err := NewStandardBlock(
		postForkBlkVersion,
		blkTimestamp,
		parentID,
		height,
		txs,
	)
	assert.NoError(err)

	// parse block
	parsed, err = Parse(postForkStdBlk.Bytes())
	assert.NoError(err)

	// compare content
	assert.Equal(postForkStdBlk.ID(), parsed.ID())
	assert.Equal(postForkStdBlk.Bytes(), parsed.Bytes())
	assert.Equal(postForkStdBlk.Parent(), postForkStdBlk.Parent())
	assert.Equal(postForkStdBlk.Height(), parsed.Height())
	assert.Equal(postForkStdBlk.Version(), parsed.Version())
	assert.Equal(postForkStdBlk.UnixTimestamp(), parsed.UnixTimestamp())
	parsedPostForkStdBlk, ok := parsed.(*PostForkStandardBlock)
	assert.True(ok)
	assert.Equal(txs, parsedPostForkStdBlk.Txs)

	// backward compatibility check
	assert.Equal(parsedPreForkStdBlk.Txs, parsedPostForkStdBlk.Txs)
}

func TestProposalBlocks(t *testing.T) {
	// check preFork standard block can be built and parsed
	assert := assert.New(t)
	preForkblkVersion := uint16(PreForkVersion)
	blkTimestamp := uint64(time.Now().Unix())
	parentID := ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'I', 'D'}
	height := uint64(2022)
	tx, err := testProposalTx()
	assert.NoError(err)

	// build block
	preForkProposalBlk, err := NewProposalBlock(
		preForkblkVersion,
		blkTimestamp,
		parentID,
		height,
		*tx,
	)
	assert.NoError(err)

	// parse block
	parsed, err := Parse(preForkProposalBlk.Bytes())
	assert.NoError(err)

	// compare content
	assert.Equal(preForkProposalBlk.ID(), parsed.ID())
	assert.Equal(preForkProposalBlk.Bytes(), parsed.Bytes())
	assert.Equal(preForkProposalBlk.Parent(), parsed.Parent())
	assert.Equal(preForkProposalBlk.Height(), parsed.Height())
	assert.Equal(preForkProposalBlk.Version(), parsed.Version())

	// timestamp is not serialized in pre fork blocks
	// no matter if block is built with a non-zero timestamp
	assert.Equal(int64(0), parsed.UnixTimestamp())

	parsedPreForkProposalBlk, ok := parsed.(*ProposalBlock)
	assert.True(ok)
	assert.Equal(*tx, parsedPreForkProposalBlk.Tx)

	// check that post fork standard block can be built and parsed
	postForkBlkVersion := uint16(PostForkVersion)

	// build block
	postForkProposalBlk, err := NewProposalBlock(
		postForkBlkVersion,
		blkTimestamp,
		parentID,
		height,
		*tx,
	)
	assert.NoError(err)

	// parse block
	parsed, err = Parse(postForkProposalBlk.Bytes())
	assert.NoError(err)

	// compare content
	assert.Equal(postForkProposalBlk.ID(), parsed.ID())
	assert.Equal(postForkProposalBlk.Bytes(), parsed.Bytes())
	assert.Equal(postForkProposalBlk.Parent(), postForkProposalBlk.Parent())
	assert.Equal(postForkProposalBlk.Height(), parsed.Height())
	assert.Equal(postForkProposalBlk.Version(), parsed.Version())
	assert.Equal(postForkProposalBlk.UnixTimestamp(), parsed.UnixTimestamp())
	parsedPostForkProposalBlk, ok := parsed.(*PostForkProposalBlock)
	assert.True(ok)
	assert.Equal(*tx, parsedPostForkProposalBlk.Tx)

	// backward compatibility check
	assert.Equal(parsedPreForkProposalBlk.Tx, parsedPostForkProposalBlk.Tx)
}

func TestCommitBlock(t *testing.T) {
	// check preFork standard block can be built and parsed
	assert := assert.New(t)
	preForkblkVersion := uint16(PreForkVersion)
	blkTimestamp := uint64(time.Now().Unix())
	parentID := ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'I', 'D'}
	height := uint64(2022)

	// build block
	preForkCommitBlk, err := NewCommitBlock(
		preForkblkVersion,
		blkTimestamp,
		parentID,
		height,
	)
	assert.NoError(err)

	// parse block
	parsed, err := Parse(preForkCommitBlk.Bytes())
	assert.NoError(err)

	// compare content
	assert.Equal(preForkCommitBlk.ID(), parsed.ID())
	assert.Equal(preForkCommitBlk.Bytes(), parsed.Bytes())
	assert.Equal(preForkCommitBlk.Parent(), parsed.Parent())
	assert.Equal(preForkCommitBlk.Height(), parsed.Height())
	assert.Equal(preForkCommitBlk.Version(), parsed.Version())

	// timestamp is not serialized in pre fork blocks
	// no matter if block is built with a non-zero timestamp
	assert.Equal(int64(0), parsed.UnixTimestamp())

	// check that post fork standard block can be built and parsed
	postForkBlkVersion := uint16(PostForkVersion)

	// build block
	postForkCommitBlk, err := NewCommitBlock(
		postForkBlkVersion,
		blkTimestamp,
		parentID,
		height,
	)
	assert.NoError(err)

	// parse block
	parsed, err = Parse(postForkCommitBlk.Bytes())
	assert.NoError(err)

	// compare content
	assert.Equal(postForkCommitBlk.ID(), parsed.ID())
	assert.Equal(postForkCommitBlk.Bytes(), parsed.Bytes())
	assert.Equal(postForkCommitBlk.Parent(), postForkCommitBlk.Parent())
	assert.Equal(postForkCommitBlk.Height(), parsed.Height())
	assert.Equal(postForkCommitBlk.Version(), parsed.Version())
	assert.Equal(postForkCommitBlk.UnixTimestamp(), parsed.UnixTimestamp())
}

func TestAbortBlock(t *testing.T) {
	// check preFork standard block can be built and parsed
	assert := assert.New(t)
	preForkblkVersion := uint16(PreForkVersion)
	blkTimestamp := uint64(time.Now().Unix())
	parentID := ids.ID{'p', 'a', 'r', 'e', 'n', 't', 'I', 'D'}
	height := uint64(2022)

	// build block
	preForkAbortBlk, err := NewAbortBlock(
		preForkblkVersion,
		blkTimestamp,
		parentID,
		height,
	)
	assert.NoError(err)

	// parse block
	parsed, err := Parse(preForkAbortBlk.Bytes())
	assert.NoError(err)

	// compare content
	assert.Equal(preForkAbortBlk.ID(), parsed.ID())
	assert.Equal(preForkAbortBlk.Bytes(), parsed.Bytes())
	assert.Equal(preForkAbortBlk.Parent(), parsed.Parent())
	assert.Equal(preForkAbortBlk.Height(), parsed.Height())
	assert.Equal(preForkAbortBlk.Version(), parsed.Version())

	// timestamp is not serialized in pre fork blocks
	// no matter if block is built with a non-zero timestamp
	assert.Equal(int64(0), parsed.UnixTimestamp())

	// check that post fork standard block can be built and parsed
	postForkBlkVersion := uint16(PostForkVersion)

	// build block
	postForkAbortBlk, err := NewAbortBlock(
		postForkBlkVersion,
		blkTimestamp,
		parentID,
		height,
	)
	assert.NoError(err)

	// parse block
	parsed, err = Parse(postForkAbortBlk.Bytes())
	assert.NoError(err)

	// compare content
	assert.Equal(postForkAbortBlk.ID(), parsed.ID())
	assert.Equal(postForkAbortBlk.Bytes(), parsed.Bytes())
	assert.Equal(postForkAbortBlk.Parent(), postForkAbortBlk.Parent())
	assert.Equal(postForkAbortBlk.Height(), parsed.Height())
	assert.Equal(postForkAbortBlk.Version(), parsed.Version())
	assert.Equal(postForkAbortBlk.UnixTimestamp(), parsed.UnixTimestamp())
}

func testDecisionTxs() ([]*signed.Tx, error) {
	countTxs := 2
	txs := make([]*signed.Tx, 0, countTxs)
	for i := 0; i < countTxs; i++ {
		// Create the tx
		utx := &unsigned.CreateChainTx{
			BaseTx: unsigned.BaseTx{BaseTx: avax.BaseTx{
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
		tx, err := signed.New(utx, unsigned.Codec, signers)
		if err != nil {
			return nil, err
		}
		txs = append(txs, tx)
	}
	return txs, nil
}

func testProposalTx() (*signed.Tx, error) {
	utx := &unsigned.RewardValidatorTx{
		TxID: ids.ID{'r', 'e', 'w', 'a', 'r', 'd', 'I', 'D'},
	}

	signers := [][]*crypto.PrivateKeySECP256K1R{{preFundedKeys[0]}}
	return signed.New(utx, unsigned.Codec, signers)
}
