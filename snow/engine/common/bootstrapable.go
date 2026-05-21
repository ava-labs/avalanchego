// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import "context"

type BootstrapableEngine interface {
	Engine

	// Clear removes all containers to be processed upon bootstrapping
	Clear(ctx context.Context) error
}
