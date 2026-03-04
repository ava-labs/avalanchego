// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var (
	_ verify.State    = (*TestState)(nil)
	_ TransferableOut = (*TestTransferable)(nil)
	_ Addressable     = (*TestAddressable)(nil)
)

type TestState struct {
	verify.IsState `json:"-"`

	Err error
}

func (*TestState) InitCtx(*snow.Context) {}

func (v *TestState) Verify() error {
	return v.Err
}

type TestTransferable struct {
	TestState

	Val uint64 `serialize:"true"`
}

func (*TestTransferable) InitCtx(*snow.Context) {}

func (t *TestTransferable) Amount() uint64 {
	return t.Val
}

func (*TestTransferable) Cost() (uint64, error) {
	return 0, nil
}

type TestAddressable struct {
	TestTransferable `serialize:"true"`

	Addrs [][]byte `serialize:"true"`
}

func (a *TestAddressable) Addresses() [][]byte {
	return a.Addrs
}
