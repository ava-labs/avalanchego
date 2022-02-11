// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"github.com/ava-labs/avalanchego/ids"
)

type BootstrapableEngine interface {
	Bootstrapable
	Engine
}

// Bootstrapable defines the functionality required to support bootstrapping
type Bootstrapable interface {
	// Force the provided containers to be accepted. Only returns fatal errors
	// if they occur.
	ForceAccepted(acceptedContainerIDs []ids.ID) error

	// Clear remove all containers to be processed upon bootstrapping
	Clear() error
}
