// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/stretchr/testify/assert"
)

func TestPostForkProposalBlockTimestampChecks(t *testing.T) {
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
			childTx, err := testProposalTx()
			assert.NoError(err)
			blk, err := NewProposalBlock(
				childVersion,
				uint64(test.childTime.Unix()),
				h.blkVerifier,
				h.txExecBackend,
				postForkParentBlk.ID(),
				childHeight,
				*childTx,
			)
			assert.NoError(err)

			// call verify on it
			err = blk.commonBlock.verify()
			assert.ErrorIs(err, test.result)
		})
	}
}

func testProposalTx() (*txs.Tx, error) {
	utx := &txs.RewardValidatorTx{
		TxID: ids.ID{'r', 'e', 'w', 'a', 'r', 'd', 'I', 'D'},
	}

	signers := [][]*crypto.PrivateKeySECP256K1R{{preFundedKeys[0]}}
	return txs.NewSigned(utx, txs.Codec, signers)
}
