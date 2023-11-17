// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import "context"

type BootstrapableEngine interface {
	Bootstrapable
	Engine
}

// Bootstrapable defines the functionality required to support bootstrapping
type Bootstrapable interface {
	// Clear removes all containers to be processed upon bootstrapping
	Clear(ctx context.Context) error
}
