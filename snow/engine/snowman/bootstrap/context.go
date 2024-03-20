// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/snow/engine/common"
)

var (
	_ context.Context = (*haltableContext)(nil)

	errHalted = errors.New("halted")
)

type haltableContext struct {
	context.Context
	common.Haltable
}

func (c *haltableContext) Err() error {
	if c.Halted() {
		return errHalted
	}
	return c.Context.Err()
}
