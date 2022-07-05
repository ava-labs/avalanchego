// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

var _ blockState = &blockStateImpl{}

func MakeStateful(
	statelessBlk stateless.Block,
	manager Manager,
	txExecutorBackend executor.Backend,
	status choices.Status,
) (Block, error) {
	switch sb := statelessBlk.(type) {
	case *stateless.AbortBlock:
		return toStatefulAbortBlock(sb, manager, txExecutorBackend, false /*wasPreferred*/, status)
	case *stateless.AtomicBlock:
		return toStatefulAtomicBlock(sb, manager, txExecutorBackend, status)
	case *stateless.CommitBlock:
		return toStatefulCommitBlock(sb, manager, txExecutorBackend, false /*wasPreferred*/, status)
	case *stateless.ProposalBlock:
		return toStatefulProposalBlock(sb, manager, txExecutorBackend, status)
	case *stateless.StandardBlock:
		return toStatefulStandardBlock(sb, manager, txExecutorBackend, status)
	default:
		return nil, fmt.Errorf("couldn't make unknown block type %T stateful", statelessBlk)
	}
}

type statelessBlockState interface {
	GetStatelessBlock(blockID ids.ID) (stateless.Block, choices.Status, error)
	AddStatelessBlock(block stateless.Block, status choices.Status)
}

type statefulBlockState interface {
	GetStatefulBlock(blkID ids.ID) (Block, error)
	pinVerifiedBlock(blk Block)
	unpinVerifiedBlock(id ids.ID)
}

type blockState interface {
	statefulBlockState
	statelessBlockState
}

type blockStateImpl struct {
	statelessBlockState
	// TODO is there a way to avoid having [manager] in here?
	// [blockStateImpl] is embedded in manager.
	manager           Manager
	verifiedBlks      map[ids.ID]Block
	txExecutorBackend executor.Backend
}

func (b *blockStateImpl) GetStatefulBlock(blkID ids.ID) (Block, error) {
	// If block is in memory, return it.
	if blk, exists := b.verifiedBlks[blkID]; exists {
		return blk, nil
	}

	statelessBlk, blkStatus, err := b.statelessBlockState.GetStatelessBlock(blkID)
	if err != nil {
		return nil, err
	}

	return MakeStateful(
		statelessBlk,
		b.manager,
		b.txExecutorBackend,
		blkStatus,
	)
}

func (b *blockStateImpl) pinVerifiedBlock(blk Block) {
	b.verifiedBlks[blk.ID()] = blk
}

func (b *blockStateImpl) unpinVerifiedBlock(id ids.ID) {
	delete(b.verifiedBlks, id)
}
