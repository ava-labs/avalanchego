// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var _ chain = &VM{}

type chain interface {
	block.ChainVM
	block.BuildBlockWithContextChainVM
	block.SetPreferenceWithContextChainVM
}

type VM struct {
	transitionTime time.Time
	transitionLock sync.RWMutex
	transitioned   bool
	current        *current
}

type current struct {
	chain        chain
	requests     *requests
	connections  *connections
	httpHandlers *httpHandlers
}

func (v *VM) Initialize(ctx context.Context, chainCtx *snow.Context, db database.Database, genesisBytes []byte, upgradeBytes []byte, configBytes []byte, fxs []*common.Fx, appSender common.AppSender) error {
	return errUnimplemented
}

func (v *VM) transition(ctx context.Context) error {
	return errUnimplemented
}

func (v *VM) SetState(ctx context.Context, state snow.State) error {
	return errUnimplemented
}

func (v *VM) WaitForEvent(ctx context.Context) (common.Message, error) {
	return 0, errUnimplemented
}
