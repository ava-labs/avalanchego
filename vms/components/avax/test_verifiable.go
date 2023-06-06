// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import "github.com/ava-labs/avalanchego/snow"

type TestState struct{ Err error }

func (*TestState) InitCtx(*snow.Context) {}

func (v *TestState) Verify() error {
	return v.Err
}

func (*TestState) IsState() {}

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
