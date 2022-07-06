// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ Block = &ProposalBlock{}

	ErrAdvanceTimeTxCannotBeIncluded = errors.New("advance time tx cannot be included in block")
)

// ProposalBlock is a proposal to change the chain's state.
//
// A proposal may be to:
// 	1. Advance the chain's timestamp (*AdvanceTimeTx)
//  2. Remove a staker from the staker set (*RewardStakerTx)
//  3. Add a new staker to the set of pending (future) stakers
//     (*AddValidatorTx, *AddDelegatorTx, *AddSubnetValidatorTx)
//
// The proposal will be enacted (change the chain's state) if the proposal block
// is accepted and followed by an accepted Commit block
type ProposalBlock struct {
	stateless.ProposalBlockIntf
	*commonBlock

	// Following Blueberry activation, onBlueberryBaseOptionsState is the base state
	// over which both commit and abort states are built
	onBlueberryBaseOptionsState state.Diff
	// The state that the chain will have if this block's proposal is committed
	onCommitState state.Diff
	// The state that the chain will have if this block's proposal is aborted
	onAbortState state.Diff

	prefersCommit bool

	manager Manager
}

func (pb *ProposalBlock) ExpectedChildVersion() uint16 {
	forkTime := pb.manager.GetConfig().BlueberryTime
	if pb.Timestamp().Before(forkTime) {
		return stateless.ApricotVersion
	}
	return stateless.BlueberryVersion
}

// NewProposalBlock creates a new block that proposes to issue a transaction.
// The parent of this block has ID [parentID].
// The parent must be a decision block.
func NewProposalBlock(
	version uint16,
	timestamp uint64,
	manager Manager,
	ctx *snow.Context,
	parentID ids.ID,
	height uint64,
	tx *txs.Tx,
) (*ProposalBlock, error) {
	statelessBlk, err := stateless.NewProposalBlock(version, timestamp, parentID, height, tx)
	if err != nil {
		return nil, err
	}

	return toStatefulProposalBlock(statelessBlk, manager, ctx, choices.Processing)
}

func toStatefulProposalBlock(
	statelessBlk stateless.ProposalBlockIntf,
	manager Manager,
	ctx *snow.Context,
	status choices.Status,
) (*ProposalBlock, error) {
	pb := &ProposalBlock{
		ProposalBlockIntf: statelessBlk,
		commonBlock: &commonBlock{
			baseBlk:         statelessBlk,
			status:          status,
			timestampGetter: manager,
			lastAccepteder:  manager,
		},
		manager: manager,
	}

	pb.ProposalTx().Unsigned.InitCtx(ctx)
	return pb, nil
}

func (pb *ProposalBlock) free() {
	pb.manager.freeProposalBlock(pb)
}

func (pb *ProposalBlock) Verify() error {
	return pb.manager.verifyProposalBlock(pb)
}

func (pb *ProposalBlock) Accept() error {
	return pb.manager.acceptProposalBlock(pb)
}

func (pb *ProposalBlock) Reject() error {
	return pb.manager.rejectProposalBlock(pb)
}

func (pb *ProposalBlock) conflicts(s ids.Set) (bool, error) {
	return pb.manager.conflictsProposalBlock(pb, s)
}

// Options returns the possible children of this block in preferential order.
func (pb *ProposalBlock) Options() ([2]snowman.Block, error) {
	blkID := pb.ID()
	nextHeight := pb.Height() + 1

	commit, err := NewCommitBlock(
		pb.Version(),
		uint64(pb.Timestamp().Unix()),
		pb.manager,
		blkID,
		nextHeight,
		pb.prefersCommit,
	)
	if err != nil {
		return [2]snowman.Block{}, fmt.Errorf(
			"failed to create commit block: %w",
			err,
		)
	}
	abort, err := NewAbortBlock(
		pb.Version(),
		uint64(pb.Timestamp().Unix()),
		pb.manager,
		blkID,
		nextHeight,
		!pb.prefersCommit,
	)
	if err != nil {
		return [2]snowman.Block{}, fmt.Errorf(
			"failed to create abort block: %w",
			err,
		)
	}

	if pb.prefersCommit {
		return [2]snowman.Block{commit, abort}, nil
	}
	return [2]snowman.Block{abort, commit}, nil
}

func (pb *ProposalBlock) setBaseState() {
	pb.manager.setBaseStateProposalBlock(pb)
}
