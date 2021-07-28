// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import "github.com/ava-labs/avalanchego/snow"

// TestVerifiable ...
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

// Amount ...
func (t *TestTransferable) Amount() uint64 { return t.Val }

type TestAddressable struct {
	TestTransferable `serialize:"true"`

	Addrs [][]byte `serialize:"true"`
}

func (a *TestAddressable) Addresses() [][]byte { return a.Addrs }
