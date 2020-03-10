// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spdagvm

import (
	"testing"

	"github.com/ava-labs/gecko/ids"
)

func TestBuildGenesis(t *testing.T) {
	expected := "111GZiNYug8np6hdorSEF5daDtep3Zc1BxWNc9UoxNkXhKK9xcvTbAbe3DX5bbAZ34BS4cHcKsQZ8SmDfi1CEYRaQVHf3ishkzbEsde67GM3KVfhwKMmyz33Ax8e1iwGcWftnsNPgRSGNkvAX9mdDgRszhXJG9Vp6RPRgW14hcufkQjq8ZGV1CajkgHLMvscex7yDsVRikwM2swra3Hrdmp32Ut8jR"

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
