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

func TestPostForkCommitBlockTimestampChecks(t *testing.T) {
	assert := assert.New(t)

	h := newTestHelpersCollection(t, nil)
	defer func() {
		if err := internalStateShutdown(h); err != nil {
			t.Fatal(err)
		}
	}()

	now := defaultGenesisTime.Add(time.Hour)
	h.clk.Set(now)
	blkVersion := uint16(stateless.PostForkVersion)

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
			// activate Apricot Phase 6
			h.cfg.ApricotPhase6Time = time.Time{}

			// setup and store parent block
			// it's a standard block for simplicity
			parentVersion := blkVersion
			parentHeight := uint64(2022)
			parentTx, err := testProposalTx()
			assert.NoError(err)

			postForkParentBlk, err := NewProposalBlock(
				parentVersion,
				uint64(test.parentTime.Unix()),
				h.blkVerifier,
				h.txExecBackend,
				ids.Empty, // does not matter
				parentHeight,
				parentTx,
			)
			assert.NoError(err)
			assert.NoError(err)
			h.fullState.AddStatelessBlock(postForkParentBlk, choices.Accepted)

			// build and verify child block
			childVersion := blkVersion
			childHeight := parentHeight + 1
			blk, err := NewCommitBlock(
				childVersion,
				uint64(test.childTime.Unix()),
				h.blkVerifier,
				h.txExecBackend,
				postForkParentBlk.ID(),
				childHeight,
				true, // wasPreferred
			)
			assert.NoError(err)

			// call verify on it
			err = blk.commonBlock.verify(false /*enforceStrictness*/)
			assert.ErrorIs(err, test.result)
		})
	}
}
