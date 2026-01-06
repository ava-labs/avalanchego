// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/message"
)

var _ Executor = (*staticExecutor)(nil)

// staticExecutor runs sync steps sequentially and then accepts the synced state into the VM.
type staticExecutor struct {
	client *client
}

func newStaticExecutor(c *client) *staticExecutor {
	return &staticExecutor{
		client: c,
	}
}

func (e *staticExecutor) Execute(ctx context.Context, summary message.Syncable) error {
	if err := e.client.stateSync(ctx, summary); err != nil {
		return err
	}
	return e.client.AcceptSync(ctx, summary)
}
