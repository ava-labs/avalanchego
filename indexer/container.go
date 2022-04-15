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

package indexer

import "github.com/chain4travel/caminogo/ids"

// Container is something that gets accepted
// (a block, transaction or vertex)
type Container struct {
	// ID of this container
	ID ids.ID `serialize:"true"`
	// Byte representation of this container
	Bytes []byte `serialize:"true"`
	// Unix time, in nanoseconds, at which this container was accepted by this node
	Timestamp int64 `serialize:"true"`
}
