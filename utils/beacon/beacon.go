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

package beacon

import (
	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/utils"
)

var _ Beacon = &beacon{}

type Beacon interface {
	ID() ids.ShortID
	IP() utils.IPDesc
}

type beacon struct {
	id ids.ShortID
	ip utils.IPDesc
}

func New(id ids.ShortID, ip utils.IPDesc) Beacon {
	return &beacon{
		id: id,
		ip: ip,
	}
}

func (b *beacon) ID() ids.ShortID  { return b.id }
func (b *beacon) IP() utils.IPDesc { return b.ip }
