// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var (
	_ block.ChainVM                = (*TestVM)(nil)
	_ block.GetInitialPreferenceVM = (*TestVM)(nil)
)

type TestVM struct {
	*block.TestVM
	GetInitialPreferenceF func(ctx context.Context) (ids.ID, error)
}

func (t *TestVM) GetInitialPreference(ctx context.Context) (ids.ID, error) {
	if t.GetInitialPreferenceF != nil {
		return t.GetInitialPreferenceF(ctx)
	}

	return ids.Empty, nil
}
