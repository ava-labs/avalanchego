// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spdagvm

import (
	"testing"

	"github.com/ava-labs/gecko/ids"
)

func TestBuildGenesis(t *testing.T) {
	expected := "0x000000020000000000000000000000000000000000000000000000000000000000000000000000000000000200000000000000003b9aca0000000000000000000000000100000001015cce6c55d6b509845c8c4e30bed98d391ae7f000000001000000003b9aca0000000000000000000000000100000001015cce6c55d6b509845c8c4e30bed98d391ae7f000000007915ecbc4000000000000000000000000"

	addr, _ := ids.ShortFromString("8CrVPQZ4VSqgL8zTdvL14G8HqAfrBr4z")

	outputPayment := APIOutput{
		Amount:    1000000000,
		Locktime:  0,
		Threshold: 1,
		Addresses: []ids.ShortID{
			addr,
		},
	}
	outputTakeOrLeave := APIOutput{
		Amount:    1000000000,
		Locktime:  0,
		Threshold: 1,
		Addresses: []ids.ShortID{
			addr,
		},
		Locktime2:  32503679940,
		Threshold2: 0,
		Addresses2: []ids.ShortID{},
	}

	args := BuildGenesisArgs{
		Outputs: []APIOutput{
			outputPayment,
			outputTakeOrLeave,
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

func TestBuildGenesisInvalidOutput(t *testing.T) {
	output := APIOutput{
		Amount:    0,
		Locktime:  0,
		Threshold: 0,
		Addresses: []ids.ShortID{},
	}

	args := BuildGenesisArgs{
		Outputs: []APIOutput{
			output,
		},
	}
	reply := BuildGenesisReply{}

	ss := StaticService{}
	if err := ss.BuildGenesis(nil, &args, &reply); err == nil {
		t.Fatalf("Should have failed with an invalid output")
	}
}
