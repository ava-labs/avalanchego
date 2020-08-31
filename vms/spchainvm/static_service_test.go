// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spchainvm

import (
	"testing"

	"github.com/ava-labs/gecko/ids"
)

func TestBuildGenesis(t *testing.T) {
	expected := "0x00000001015cce6c55d6b509845c8c4e30bed98d391ae7f0000000000000000000000000075bcd15"

	addr, _ := ids.ShortFromString("8CrVPQZ4VSqgL8zTdvL14G8HqAfrBr4z")

	account := APIAccount{
		Address: addr,
		Balance: 123456789,
	}

	args := BuildGenesisArgs{
		Accounts: []APIAccount{
			account,
		},
	}
	reply := BuildGenesisReply{}

	ss := StaticService{}
	if err := ss.BuildGenesis(nil, &args, &reply); err != nil {
		t.Fatal(err)
	}

	if reply.Bytes.String() != expected {
		t.Fatalf("StaticService.BuildGenesis:\nReturned: %s\nExpected: %s", reply.Bytes, expected)
	}
}

func TestBuildGenesisInvalidAmount(t *testing.T) {
	addr, _ := ids.ShortFromString("8CrVPQZ4VSqgL8zTdvL14G8HqAfrBr4z")

	account := APIAccount{
		Address: addr,
		Balance: 0,
	}

	args := BuildGenesisArgs{
		Accounts: []APIAccount{
			account,
		},
	}
	reply := BuildGenesisReply{}

	ss := StaticService{}
	if err := ss.BuildGenesis(nil, &args, &reply); err == nil {
		t.Fatalf("Should have errored due to an invlaid amount")
	}
}
