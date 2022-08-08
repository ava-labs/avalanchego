// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

type Visitor interface {
	AtomicBlock(*AtomicBlock) error
	ProposalBlock(*ProposalBlock) error
	StandardBlock(*StandardBlock) error
	AbortBlock(*AbortBlock) error
	CommitBlock(*CommitBlock) error
}
