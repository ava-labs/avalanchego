// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import "github.com/ava-labs/gecko/vms/components/ava"

type testAddressable struct {
	ava.TestTransferable `serialize:"true"`

	Addrs [][]byte `serialize:"true"`
}

func (a *testAddressable) Addresses() [][]byte { return a.Addrs }
