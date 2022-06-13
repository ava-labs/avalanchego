// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"fmt"

	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
)

func MakeStateful(statelessBlk stateless.Block, verifier Verifier, status choices.Status) (Block, error) {
	switch sb := statelessBlk.(type) {
	case *stateless.AbortBlock:
		return toStatefulAbortBlock(sb, verifier, false /*wasPreferred*/, status)

	case *stateless.AtomicBlock:
		return toStatefulAtomicBlock(sb, verifier, status)

	case *stateless.CommitBlock:
		return toStatefulCommitBlock(sb, verifier, false /*wasPreferred*/, status)

	case *stateless.ProposalBlock:
		return toStatefulProposalBlock(sb, verifier, status)

	case *stateless.StandardBlock:
		return toStatefulStandardBlock(sb, verifier, status)

	default:
		return nil, fmt.Errorf("couldn't make unknown block type %T stateful", statelessBlk)
	}
}
