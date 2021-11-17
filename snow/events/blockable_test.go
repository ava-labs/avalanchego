// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package events

import (
	"github.com/ava-labs/avalanchego/ids"
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
