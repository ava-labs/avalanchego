// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/stretchr/testify/assert"
)

func TestPostForkStandardBlockTimestampChecks(t *testing.T) {
	assert := assert.New(t)

	h := newTestHelpersCollection(t)
	defer func() {
		if err := internalStateShutdown(h); err != nil {
			t.Fatal(err)
		}
	}()

	// setup relevant timestamp relationship
	nextStakerChangeTime, err := h.fullState.GetNextStakerChangeTime()
	assert.NoError(err)
	blkVersion := uint16(stateless.PostForkVersion)

	tests := []struct {
		description string
		localTime   time.Time
		parentTime  time.Time
		childTime   time.Time
		result      error
	}{
		{
			// -----|--------|------X------------|----------------------|
			//      ^        ^      ^            ^                      ^
			//  localTime    |      |  localTime + syncBound            |
			//           parentTime |                          nextStakerChangeTime
			//                  childTime
			description: "valid timestamp",
			localTime:   nextStakerChangeTime.Add(-59 * time.Second),
			parentTime:  nextStakerChangeTime.Add(-60 * time.Second),
			childTime:   nextStakerChangeTime.Add(-55 * time.Second),
			result:      nil,
		},
		{
			// -----|--------X------|------------|----------------------|
			//      ^        ^      ^            ^                      ^
			//  localTime    |      |  localTime + syncBound            |
			//           childTime  |                          nextStakerChangeTime
			//                  parentTime
			description: "block timestamp cannot be before parent timestamp",
			localTime:   nextStakerChangeTime.Add(-59 * time.Second),
			childTime:   nextStakerChangeTime.Add(-60 * time.Second),
			parentTime:  nextStakerChangeTime.Add(-55 * time.Second),
			result:      executor.ErrChildBlockEarlierThanParent,
		},
		{
			// -----|--------|------X------------|-------------|-----x
			//      ^        ^      ^            ^             ^     ^
			//  localTime    |      |  localTime + syncBound   |  childTime
			//           parentTime |                 nextStakerChangeTime
			description: "blk timestamp cannot be after next staker change time",
			localTime:   nextStakerChangeTime.Add(-59 * time.Second),
			parentTime:  nextStakerChangeTime.Add(-60 * time.Second),
			childTime:   nextStakerChangeTime.Add(5 * time.Second),
			result:      executor.ErrChildBlockAfterStakerChangeTime,
		},
		{
			// -----|--------|---------|---------X----------------------|
			//      ^        ^      ^            ^                      ^
			//  localTime    |      |         childTime                 |
			//           parentTime |                          nextStakerChangeTime
			//                  localTime + syncBound
			description: "blk timestamp beyond local time sync",
			localTime:   nextStakerChangeTime.Add(-59 * time.Second),
			parentTime:  nextStakerChangeTime.Add(-60 * time.Second),
			childTime: nextStakerChangeTime.Add(-59 * time.Second).
				Add(executor.SyncBound).Add(time.Second),
			result: executor.ErrChildBlockBeyondSyncBound,
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			// activate advance time tx removal fork
			h.cfg.AdvanceTimeTxRemovalTime = time.Time{}
			h.clk.Set(test.localTime)

			// setup and store parent block
			// it's a standard block for simplicity
			parentVersion := blkVersion
			parentHeight := uint64(2022)
			parentTxs, err := testDecisionTxs()
			assert.NoError(err)

			postForkParentBlk, err := stateless.NewStandardBlock(
				parentVersion,
				uint64(test.parentTime.Unix()),
				ids.Empty, // does not matter
				parentHeight,
				parentTxs,
			)
			assert.NoError(err)
			h.fullState.AddStatelessBlock(postForkParentBlk, choices.Accepted)

			// build and verify child block
			childVersion := blkVersion
			childHeight := parentHeight + 1
			childTxs, err := testDecisionTxs()
			assert.NoError(err)
			blk, err := NewStandardBlock(
				childVersion,
				uint64(test.childTime.Unix()),
				h.blkVerifier,
				h.txExecBackend,
				postForkParentBlk.ID(),
				childHeight,
				childTxs,
			)
			assert.NoError(err)

			// call verify on it
			err = blk.decisionBlock.verify()
			assert.ErrorIs(err, test.result)
		})
	}
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
