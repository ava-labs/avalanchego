// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"errors"

	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

var _ Manager = &manager{}

type Manager interface {
	verifier
	acceptor
	rejector
	conflictChecker
	freer
	MakeStateful(
		statelessBlk stateless.Block,
		txExecutorBackend executor.Backend,
		status choices.Status,
	) (Block, error)
}

// TODO: implement
func NewManager() Manager {
	return &manager{}
}

type manager struct {
	verifier
	acceptor
	rejector
	conflictChecker
	freer
}

func (m *manager) MakeStateful(
	statelessBlk stateless.Block,
	txExecutorBackend executor.Backend,
	status choices.Status,
) (Block, error) {
	return nil, errors.New("TODO")
}
