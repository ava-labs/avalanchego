// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"context"
	"time"
)

// Context that does not inherit the cancellation of its parent context.
// Cannot cancel this context, and this context never returns on "Done".
// Only used for context key-values.
type detachedContext struct {
	ctx context.Context
}

func Detach(ctx context.Context) context.Context {
	return &detachedContext{
		ctx: ctx,
	}
}

func (*detachedContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (*detachedContext) Done() <-chan struct{} {
	return nil
}

func (*detachedContext) Err() error {
	return nil
}

func (c *detachedContext) Value(key any) any {
	return c.ctx.Value(key)
}
