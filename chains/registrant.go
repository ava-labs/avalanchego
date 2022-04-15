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

package chains

import (
	"github.com/chain4travel/caminogo/snow/engine/common"
)

// Registrant can register the existence of a chain
type Registrant interface {
	// Called when the chain described by [engine] is created
	// This function is called before the chain starts processing messages
	// [engine] should be an avalanche.Engine or snowman.Engine
	RegisterChain(name string, engine common.Engine)
}
