// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

type option struct {
	PrntID     ids.ID `serialize:"true"`
	InnerBytes []byte `serialize:"true"`

	id    ids.ID
	bytes []byte
}

func (b *option) ID() ids.ID {
	return b.id
}

func (b *option) ParentID() ids.ID {
	return b.PrntID
}

func (b *option) Block() []byte {
	return b.InnerBytes
}

func (b *option) Bytes() []byte {
	return b.bytes
}

func (b *option) initializeID() error {
	b.id = hashing.ComputeHash256Array(b.bytes)
	return nil
}

func (b *option) initialize(bytes []byte) error {
	b.bytes = bytes
	return b.initializeID()
}

func (*option) verify(ids.ID) error {
	return nil
}
