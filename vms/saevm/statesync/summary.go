// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
)

var _ adaptor.SummaryProperties = (*summary)(nil)

type summary struct{}

// Height implements [adaptor.SummaryProperties].
func (*summary) Height() uint64 {
	panic("unimplemented")
}

// ID implements [adaptor.SummaryProperties].
func (*summary) ID() ids.ID {
	panic("unimplemented")
}

// Bytes implements [adaptor.SummaryProperties].
func (*summary) Bytes() []byte {
	panic("unimplemented")
}
