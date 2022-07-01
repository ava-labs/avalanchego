// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"fmt"

	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

func MakeStateful(
	statelessBlk stateless.Block,
	manager Manager,
	txExecutorBackend executor.Backend,
	status choices.Status,
) (Block, error) {
	switch sb := statelessBlk.(type) {
	case *stateless.AbortBlock:
		return toStatefulAbortBlock(
			sb,
			manager,
			txExecutorBackend,
			false, /*wasPreferred*/
			status,
		)

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
