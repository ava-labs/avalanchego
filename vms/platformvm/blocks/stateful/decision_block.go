// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import "github.com/ava-labs/avalanchego/vms/platformvm/state"

type decisionBlock struct {
	*commonBlock

	// state of the chain if this block is accepted
	onAcceptState state.Diff

	// to be executed if this block is accepted
	onAcceptFunc func() error
}

// From CommonDecisionBlock
func (d *decisionBlock) free() {
	d.commonBlock.free()
	d.onAcceptState = nil
}

func (d *decisionBlock) setBaseState() {
	d.onAcceptState.SetBase(d.verifier)
}

func (d *decisionBlock) OnAccept() state.Chain {
	if d.Status().Decided() || d.onAcceptState == nil {
		return d.verifier
	}
	return d.onAcceptState
}
