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
	verifier Verifier,
	txExecutorBackend executor.Backend,
	status choices.Status,
) (Block, error) {
	switch sb := statelessBlk.(type) {
	case *stateless.AbortBlock:
		return toStatefulAbortBlock(sb, verifier, txExecutorBackend, false /*wasPreferred*/, status)

	case *stateless.AtomicBlock:
		return toStatefulAtomicBlock(sb, verifier, txExecutorBackend, status)

	case *stateless.CommitBlock:
		return toStatefulCommitBlock(sb, verifier, txExecutorBackend, false /*wasPreferred*/, status)

	case *stateless.ProposalBlock:
		return toStatefulProposalBlock(sb, verifier, txExecutorBackend, status)

	case *stateless.StandardBlock:
		return toStatefulStandardBlock(sb, verifier, txExecutorBackend, status)

	default:
		return nil, fmt.Errorf("couldn't make unknown block type %T stateful", statelessBlk)
	}
}
