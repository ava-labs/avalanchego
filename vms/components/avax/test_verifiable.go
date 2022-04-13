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

package avax

import "github.com/chain4travel/caminogo/snow"

type TestVerifiable struct{ Err error }

func (v *TestVerifiable) InitCtx(ctx *snow.Context) {}
func (v *TestVerifiable) Verify() error             { return v.Err }

func (v *TestVerifiable) VerifyState() error { return v.Err }

type TestTransferable struct {
	TestVerifiable

	Val uint64 `serialize:"true"`
}

func (t *TestTransferable) InitCtx(*snow.Context) {
	// no op
}

func (t *TestTransferable) Amount() uint64 { return t.Val }

func (t *TestTransferable) Cost() (uint64, error) { return 0, nil }

type TestAddressable struct {
	TestTransferable `serialize:"true"`

	Addrs [][]byte `serialize:"true"`
}

func (a *TestAddressable) Addresses() [][]byte { return a.Addrs }
