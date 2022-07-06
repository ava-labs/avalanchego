// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/stretchr/testify/assert"
)

func TestBlueberryCommitBlockTimestampChecks(t *testing.T) {
	assert := assert.New(t)

	h := newTestHelpersCollection(t, nil)
	defer func() {
		if err := internalStateShutdown(h); err != nil {
			t.Fatal(err)
		}
	}()

	now := defaultGenesisTime.Add(time.Hour)
	h.clk.Set(now)
	blkVersion := uint16(stateless.BlueberryVersion)

	tests := []struct {
		description string
		parentTime  time.Time
		childTime   time.Time
		result      error
	}{
		{
			description: "commit block timestamp matching parent's one",
			parentTime:  now,
			childTime:   now,
			result:      nil,
		},
		{
			description: "commit block timestamp before parent's one",
			childTime:   now.Add(-1 * time.Second),
			parentTime:  now,
			result:      ErrOptionBlockTimestampNotMatchingParent,
		},
		{
			description: "commit block timestamp after parent's one",
			parentTime:  now,
			childTime:   now.Add(time.Second),
			result:      ErrOptionBlockTimestampNotMatchingParent,
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			h.cfg.BlueberryTime = time.Time{} // activate Blueberry

			// setup and store parent block
			// it's a standard block for simplicity
			parentVersion := blkVersion
			parentHeight := uint64(2022)
			parentTx, err := testProposalTx()
			assert.NoError(err)

			blueberryParentBlk, err := NewProposalBlock(
				parentVersion,
				uint64(test.parentTime.Unix()),
				h.blkManager,
				h.ctx,
				ids.Empty, // does not matter
				parentHeight,
				parentTx,
			)
			assert.NoError(err)
			assert.NoError(err)
			h.fullState.AddStatelessBlock(blueberryParentBlk, choices.Accepted)

			// build and verify child block
			childVersion := blkVersion
			childHeight := parentHeight + 1
			blk, err := NewCommitBlock(
				childVersion,
				uint64(test.childTime.Unix()),
				h.blkManager,
				blueberryParentBlk.ID(),
				childHeight,
				true, // wasPreferred
			)
			assert.NoError(err)

			// call verify on it
			err = h.blkManager.(*manager).verifier.(*verifierImpl).verifyCommonBlock(blk.decisionBlock.commonBlock, false /*enforceStrictness*/)
			assert.ErrorIs(err, test.result)
		})
	}
}
