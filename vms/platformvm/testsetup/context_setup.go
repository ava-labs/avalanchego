// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testsetup

import (
	"testing"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/snowtest"
)

type MutableSharedMemory struct {
	atomic.SharedMemory
}

func Context(tb testing.TB, baseDB database.Database) (*snow.Context, *MutableSharedMemory) {
	ctx := snowtest.Context(tb, snowtest.PChainID)
	m := atomic.NewMemory(baseDB)
	msm := &MutableSharedMemory{
		SharedMemory: m.NewSharedMemory(ctx.ChainID),
	}
	ctx.SharedMemory = msm

	return ctx, msm
}
