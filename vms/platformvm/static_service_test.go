// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"testing"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/json"
)

func TestBuildGenesisInvalidAccountBalance(t *testing.T) {
	id, _ := ids.ShortFromString("8CrVPQZ4VSqgL8zTdvL14G8HqAfrBr4z")
	account := APIAccount{
		Address: id,
		Balance: 0,
	}
	weight := json.Uint64(987654321)
	validator := APIDefaultSubnetValidator{
		APIValidator: APIValidator{
			EndTime: 15,
			Weight:  &weight,
			ID:      id,
		},
		Destination: id,
	}

	args := BuildGenesisArgs{
		Accounts: []APIAccount{
			account,
		},
		Validators: []APIDefaultSubnetValidator{
			validator,
		},
		Time: 5,
	}
	reply := BuildGenesisReply{}

	ss := StaticService{}
	if err := ss.BuildGenesis(nil, &args, &reply); err == nil {
		t.Fatalf("Should have errored due to an invalid balance")
	}
}

func TestBuildGenesisInvalidAmount(t *testing.T) {
	id, _ := ids.ShortFromString("8CrVPQZ4VSqgL8zTdvL14G8HqAfrBr4z")
	account := APIAccount{
		Address: id,
		Balance: 123456789,
	}
	weight := json.Uint64(0)
	validator := APIDefaultSubnetValidator{
		APIValidator: APIValidator{
			StartTime: 0,
			EndTime:   15,
			Weight:    &weight,
			ID:        id,
		},
		Destination: id,
	}

	args := BuildGenesisArgs{
		Accounts: []APIAccount{
			account,
		},
		Validators: []APIDefaultSubnetValidator{
			validator,
		},
		Time: 5,
	}
	reply := BuildGenesisReply{}

	ss := StaticService{}
	if err := ss.BuildGenesis(nil, &args, &reply); err == nil {
		t.Fatalf("Should have errored due to an invalid amount")
	}
}

func TestBuildGenesisInvalidEndtime(t *testing.T) {
	id, _ := ids.ShortFromString("8CrVPQZ4VSqgL8zTdvL14G8HqAfrBr4z")
	account := APIAccount{
		Address: id,
		Balance: 123456789,
	}

	weight := json.Uint64(987654321)
	validator := APIDefaultSubnetValidator{
		APIValidator: APIValidator{
			StartTime: 0,
			EndTime:   5,
			Weight:    &weight,
			ID:        id,
		},
		Destination: id,
	}

	args := BuildGenesisArgs{
		Accounts: []APIAccount{
			account,
		},
		Validators: []APIDefaultSubnetValidator{
			validator,
		},
		Time: 5,
	}
	reply := BuildGenesisReply{}

	ss := StaticService{}
	if err := ss.BuildGenesis(nil, &args, &reply); err == nil {
		t.Fatalf("Should have errored due to an invalid end time")
	}
}
