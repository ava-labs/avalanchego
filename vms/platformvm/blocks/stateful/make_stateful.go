// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"fmt"

	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
)

func MakeStateful(
	statelessBlk stateless.CommonBlockIntf,
	verifier Verifier,
	status choices.Status,
) (Block, error) {
	switch sb := statelessBlk.(type) {
	case stateless.AtomicBlockIntf:
		return toStatefulAtomicBlock(sb, verifier, status)

	case stateless.ProposalBlockIntf:
		return toStatefulProposalBlock(sb, verifier, status)

	case stateless.StandardBlockIntf:
		return toStatefulStandardBlock(sb, verifier, status)

	case stateless.OptionBlock:
		switch sb.(type) {
		case *stateless.AbortBlock:
			return toStatefulAbortBlock(sb, verifier, false /*wasPreferred*/, status)
		case *stateless.CommitBlock:
			return toStatefulCommitBlock(sb, verifier, false /*wasPreferred*/, status)
		default:
			return nil, fmt.Errorf("couldn't make unknown block type %T stateful", statelessBlk)
		}

	default:
		return nil, fmt.Errorf("couldn't make unknown block type %T stateful", statelessBlk)
	}
}
