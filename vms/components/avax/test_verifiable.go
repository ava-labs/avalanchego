// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import "github.com/ava-labs/avalanchego/snow"

type TestVerifiable struct{ Err error }

func (*TestVerifiable) InitCtx(*snow.Context) {}

func (v *TestVerifiable) Verify() error {
	return v.Err
}

func (v *TestVerifiable) VerifyState() error {
	return v.Err
}

type TestTransferable struct {
	TestVerifiable

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
