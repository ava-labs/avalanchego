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

func TestBuildGenesisReturnsSortedValidators(t *testing.T) {
	id := ids.NewShortID([20]byte{1})
	account := APIAccount{
		Address: id,
		Balance: 123456789,
	}

	weight := json.Uint64(987654321)
	validator1 := APIDefaultSubnetValidator{
		APIValidator: APIValidator{
			StartTime: 0,
			EndTime:   20,
			Weight:    &weight,
			ID:        id,
		},
		Destination: id,
	}

	validator2 := APIDefaultSubnetValidator{
		APIValidator: APIValidator{
			StartTime: 3,
			EndTime:   15,
			Weight:    &weight,
			ID:        id,
		},
		Destination: id,
	}

	validator3 := APIDefaultSubnetValidator{
		APIValidator: APIValidator{
			StartTime: 1,
			EndTime:   10,
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
			validator1,
			validator2,
			validator3,
		},
		Time: 5,
	}
	reply := BuildGenesisReply{}

	ss := StaticService{}
	if err := ss.BuildGenesis(nil, &args, &reply); err != nil {
		t.Fatalf("BuildGenesis should not have errored")
	}

	genesis := &Genesis{}
	if err := Codec.Unmarshal(reply.Bytes.Bytes, genesis); err != nil {
		t.Fatal(err)
	}
	validators := genesis.Validators
	if validators.Len() == 0 {
		t.Fatal("Validators should contain 3 validators")
	}
	currentValidator := validators.Remove()
	for validators.Len() > 0 {
		nextValidator := validators.Remove()
		if currentValidator.EndTime().Unix() > nextValidator.EndTime().Unix() {
			t.Fatalf("Validators returned by genesis should be a min heap sorted by end time")
		}
		currentValidator = nextValidator
	}
}
