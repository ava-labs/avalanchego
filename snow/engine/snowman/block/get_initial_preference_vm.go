// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
)

// GetInitialPreferenceVM allows VMs to optionally define an initial preference
// when starting consensus.
type GetInitialPreferenceVM interface {
	// GetInitialPreference returns the initial preference of the VM when
	// starting consensus.
	GetInitialPreference(ctx context.Context) (ids.ID, error)
}
