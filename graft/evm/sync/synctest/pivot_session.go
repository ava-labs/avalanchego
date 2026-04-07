// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"context"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/evm/sync/types"
)

var _ types.PivotSession = (*PivotSession)(nil)

// PivotSession is a no-op types.PivotSession for testing DynamicSyncer
// without a real sync backend.
type PivotSession struct{}

func (*PivotSession) Run(context.Context) error { return nil }
func (*PivotSession) Rebuild(common.Hash, uint64) (types.PivotSession, error) {
	return &PivotSession{}, nil
}
func (*PivotSession) ShouldPivot(common.Hash) bool { return true }
func (*PivotSession) OnSessionComplete() error     { return nil }
