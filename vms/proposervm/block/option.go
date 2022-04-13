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

package block

import (
	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/utils/hashing"
)

type option struct {
	PrntID     ids.ID `serialize:"true"`
	InnerBytes []byte `serialize:"true"`

	id    ids.ID
	bytes []byte
}

func (b *option) ID() ids.ID       { return b.id }
func (b *option) ParentID() ids.ID { return b.PrntID }
func (b *option) Block() []byte    { return b.InnerBytes }
func (b *option) Bytes() []byte    { return b.bytes }

func (b *option) initialize(bytes []byte) error {
	b.id = hashing.ComputeHash256Array(bytes)
	b.bytes = bytes
	return nil
}
