// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var _ VM = (*TestVM)(nil)

type TestVM struct {
	*block.TestVM
	GetPreferenceF func() ids.ID
}

func (t *TestVM) GetPreference() ids.ID {
	if t.GetPreferenceF != nil {
		return t.GetPreferenceF()
	}

	return ids.Empty
}
