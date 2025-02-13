// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import "github.com/ava-labs/avalanchego/ids"

type Header interface {
	ChainID() ids.ID
	ParentID() ids.ID
	BodyID() ids.ID
	Bytes() []byte
}

type statelessHeader struct {
	Chain  ids.ID `serialize:"true"`
	Parent ids.ID `serialize:"true"`
	Body   ids.ID `serialize:"true"`

	bytes []byte
}

func (h *statelessHeader) ChainID() ids.ID {
	return h.Chain
}

func (h *statelessHeader) ParentID() ids.ID {
	return h.Parent
}

func (h *statelessHeader) BodyID() ids.ID {
	return h.Body
}

func (h *statelessHeader) Bytes() []byte {
	return h.bytes
}
