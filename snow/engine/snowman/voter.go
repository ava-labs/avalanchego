// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/job"
	"github.com/ava-labs/avalanchego/utils/bag"
)

var _ job.Job[ids.ID] = (*voter)(nil)

// Voter records chits received from [nodeID] once its dependencies are met.
type voter struct {
	e         *Engine
	nodeID    ids.NodeID
	requestID uint32
	vote      ids.ID
}

// Execute runs once the block [vote] waits on has been issued or abandoned.
// The vote is bubbled to its nearest processing ancestor before it is applied;
// if there is none, or if [vote] is empty, the poll records a drop for [nodeID].
func (v *voter) Execute(ctx context.Context, _ []ids.ID, _ []ids.ID) error {
	var (
		vote       ids.ID
		shouldVote bool
	)
	if v.vote != ids.Empty {
		vote, shouldVote = v.e.getProcessingAncestor(v.vote)
	}

	var results []bag.Bag[ids.ID]
	if shouldVote {
		results = v.e.polls.Vote(v.requestID, v.nodeID, vote)
	} else {
		results = v.e.polls.Drop(v.requestID, v.nodeID)
	}

	if len(results) == 0 {
		return nil
	}

	for _, result := range results {
		if err := v.e.Consensus.RecordPoll(ctx, result); err != nil {
			return err
		}
	}

	pref, _ := v.e.Consensus.Preference()
	if err := v.e.VM.SetPreference(ctx, pref); err != nil {
		return err
	}

	if v.e.Consensus.NumProcessing() == 0 {
		v.e.Ctx.Log.Debug("Snowman engine can quiesce")
		return nil
	}

	v.e.Ctx.Log.Debug("Snowman engine can't quiesce")
	v.e.repoll(ctx)
	return nil
}
