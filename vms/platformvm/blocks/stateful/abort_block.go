// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
)

var (
	_ Block    = &AbortBlock{}
	_ Decision = &AbortBlock{}
)

// AbortBlock being accepted results in the proposal of its parent (which must
// be a proposal block) being rejected.
type AbortBlock struct {
	stateless.OptionBlock
	*decisionBlock

	wasPreferred bool

	manager Manager
}

// NewAbortBlock returns a new *AbortBlock where the block's parent, a proposal
// block, has ID [parentID]. Additionally the block will track if it was
// originally preferred or not for metrics.
func NewAbortBlock(
	version uint16,
	timestamp uint64,
	manager Manager,
	parentID ids.ID,
	height uint64,
	wasPreferred bool,
) (*AbortBlock, error) {
	statelessBlk, err := stateless.NewAbortBlock(version, timestamp, parentID, height)
	if err != nil {
		return nil, err
	}
	return toStatefulAbortBlock(
		statelessBlk,
		manager,
		wasPreferred,
		choices.Processing,
	)
}

func toStatefulAbortBlock(
	statelessBlk stateless.OptionBlock,
	manager Manager,
	wasPreferred bool,
	status choices.Status,
) (*AbortBlock, error) {
	abort := &AbortBlock{
		OptionBlock: statelessBlk,
		decisionBlock: &decisionBlock{
			chainState: manager,
			commonBlock: &commonBlock{
				baseBlk:         statelessBlk,
				status:          status,
				timestampGetter: manager,
				lastAccepteder:  manager,
			},
		},
		wasPreferred: wasPreferred,
		manager:      manager,
	}

	return abort, nil
}

func (a *AbortBlock) Verify() error {
	return a.manager.verifyAbortBlock(a)
}

func (a *AbortBlock) Accept() error {
	return a.manager.acceptAbortBlock(a)
}

func (a *AbortBlock) Reject() error {
	return a.manager.rejectAbortBlock(a)
}

func (a *AbortBlock) conflicts(s ids.Set) (bool, error) {
	return a.manager.conflictsAbortBlock(a, s)
}

func (a *AbortBlock) free() {
	a.manager.freeAbortBlock(a)
}

func (a *AbortBlock) setBaseState() {
	a.manager.setBaseStateAbortBlock(a)
}
