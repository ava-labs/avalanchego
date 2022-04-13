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

package events

import (
	"github.com/chain4travel/caminogo/ids"
)

var offset = uint64(0)

func GenerateID() ids.ID {
	offset++
	return ids.Empty.Prefix(offset)
}

type blockable struct {
	dependencies func() ids.Set
	fulfill      func(ids.ID)
	abandon      func(ids.ID)
	update       func()
}

func (b *blockable) Default() {
	*b = blockable{
		dependencies: func() ids.Set { return ids.Set{} },
		fulfill:      func(ids.ID) {},
		abandon:      func(ids.ID) {},
		update:       func() {},
	}
}

func (b *blockable) Dependencies() ids.Set { return b.dependencies() }
func (b *blockable) Fulfill(id ids.ID)     { b.fulfill(id) }
func (b *blockable) Abandon(id ids.ID)     { b.abandon(id) }
func (b *blockable) Update()               { b.update() }
