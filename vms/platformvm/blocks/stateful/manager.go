// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

var _ Manager = &manager{}

type Manager interface {
	verifier
	acceptor
	rejector
	conflictChecker
	freer
}

// TODO: implement
func NewManager(state state.State) Manager {
	return &manager{
		state: state,
	}
}

type manager struct {
	state state.State
	verifier
	acceptor
	rejector
	conflictChecker
	freer
}
