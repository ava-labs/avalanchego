// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

type testVerifiable struct{ err error }

func (v *testVerifiable) Verify() error { return v.err }

type TestTransferable struct {
	testVerifiable

	Val uint64 `serialize:"true"`
}

func (t *TestTransferable) Amount() uint64 { return t.Val }

type testAddressable struct {
	TestTransferable `serialize:"true"`

	Addrs [][]byte `serialize:"true"`
}

func (a *testAddressable) Addresses() [][]byte { return a.Addrs }
