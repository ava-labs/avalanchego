// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
)

var _ adaptor.SummaryProperties = (*Summary)(nil)

type Summary struct{}

func (*Summary) Height() uint64 {
	panic("unimplemented")
}

func (*Summary) ID() ids.ID {
	panic("unimplemented")
}

func (*Summary) Bytes() []byte {
	panic("unimplemented")
}
