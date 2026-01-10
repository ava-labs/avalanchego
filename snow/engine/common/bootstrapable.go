// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import "context"

type BootstrapableEngine interface {
	Engine

	// Clear removes all containers to be processed upon bootstrapping
	Clear(ctx context.Context) error

	// HasProgress returns true if there are fetched blocks from a previous
	// bootstrapping run that would be lost if Clear is called.
	// This is used to preserve bootstrap progress when state sync fails.
	HasProgress(ctx context.Context) (bool, error)
}
