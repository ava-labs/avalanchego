// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"github.com/chain4travel/caminogo/ids"
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
