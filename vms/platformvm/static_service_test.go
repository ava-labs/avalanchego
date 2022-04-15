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

package platformvm

import (
	"testing"

	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/utils/constants"
	"github.com/chain4travel/caminogo/utils/formatting"
	"github.com/chain4travel/caminogo/utils/json"
)

func TestBuildGenesisInvalidUTXOBalance(t *testing.T) {
	id := ids.ShortID{1, 2, 3}
	nodeID := id.PrefixedString(constants.NodeIDPrefix)
	hrp := constants.NetworkIDToHRP[testNetworkID]
	addr, err := formatting.FormatBech32(hrp, id.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	utxo := APIUTXO{
		Address: addr,
		Amount:  0,
	}
	weight := json.Uint64(987654321)
	validator := APIPrimaryValidator{
		APIStaker: APIStaker{
			EndTime: 15,
			Weight:  &weight,
			NodeID:  nodeID,
		},
		RewardOwner: &APIOwner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []APIUTXO{{
			Amount:  weight,
			Address: addr,
		}},
	}

	args := BuildGenesisArgs{
		UTXOs: []APIUTXO{
			utxo,
		},
		Validators: []APIPrimaryValidator{
			validator,
		},
		Time:     5,
		Encoding: formatting.Hex,
	}
	reply := BuildGenesisReply{}

	ss := StaticService{}
	if err := ss.BuildGenesis(nil, &args, &reply); err == nil {
		t.Fatalf("Should have errored due to an invalid balance")
	}
}

func TestBuildGenesisInvalidAmount(t *testing.T) {
	id := ids.ShortID{1, 2, 3}
	nodeID := id.PrefixedString(constants.NodeIDPrefix)
	hrp := constants.NetworkIDToHRP[testNetworkID]
	addr, err := formatting.FormatBech32(hrp, id.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	utxo := APIUTXO{
		Address: addr,
		Amount:  123456789,
	}
	weight := json.Uint64(0)
	validator := APIPrimaryValidator{
		APIStaker: APIStaker{
			StartTime: 0,
			EndTime:   15,
			NodeID:    nodeID,
		},
		RewardOwner: &APIOwner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []APIUTXO{{
			Amount:  weight,
			Address: addr,
		}},
	}

	args := BuildGenesisArgs{
		UTXOs: []APIUTXO{
			utxo,
		},
		Validators: []APIPrimaryValidator{
			validator,
		},
		Time:     5,
		Encoding: formatting.Hex,
	}
	reply := BuildGenesisReply{}

	ss := StaticService{}
	if err := ss.BuildGenesis(nil, &args, &reply); err == nil {
		t.Fatalf("Should have errored due to an invalid amount")
	}
}

func TestBuildGenesisInvalidEndtime(t *testing.T) {
	id := ids.ShortID{1, 2, 3}
	nodeID := id.PrefixedString(constants.NodeIDPrefix)
	hrp := constants.NetworkIDToHRP[testNetworkID]
	addr, err := formatting.FormatBech32(hrp, id.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	utxo := APIUTXO{
		Address: addr,
		Amount:  123456789,
	}

	weight := json.Uint64(987654321)
	validator := APIPrimaryValidator{
		APIStaker: APIStaker{
			StartTime: 0,
			EndTime:   5,
			NodeID:    nodeID,
		},
		RewardOwner: &APIOwner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []APIUTXO{{
			Amount:  weight,
			Address: addr,
		}},
	}

	args := BuildGenesisArgs{
		UTXOs: []APIUTXO{
			utxo,
		},
		Validators: []APIPrimaryValidator{
			validator,
		},
		Time:     5,
		Encoding: formatting.Hex,
	}
	reply := BuildGenesisReply{}

	ss := StaticService{}
	if err := ss.BuildGenesis(nil, &args, &reply); err == nil {
		t.Fatalf("Should have errored due to an invalid end time")
	}
}

func TestBuildGenesisReturnsSortedValidators(t *testing.T) {
	id := ids.ShortID{1}
	nodeID := id.PrefixedString(constants.NodeIDPrefix)
	hrp := constants.NetworkIDToHRP[testNetworkID]
	addr, err := formatting.FormatBech32(hrp, id.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	utxo := APIUTXO{
		Address: addr,
		Amount:  123456789,
	}

	weight := json.Uint64(987654321)
	validator1 := APIPrimaryValidator{
		APIStaker: APIStaker{
			StartTime: 0,
			EndTime:   20,
			NodeID:    nodeID,
		},
		RewardOwner: &APIOwner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []APIUTXO{{
			Amount:  weight,
			Address: addr,
		}},
	}

	validator2 := APIPrimaryValidator{
		APIStaker: APIStaker{
			StartTime: 3,
			EndTime:   15,
			NodeID:    nodeID,
		},
		RewardOwner: &APIOwner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []APIUTXO{{
			Amount:  weight,
			Address: addr,
		}},
	}

	validator3 := APIPrimaryValidator{
		APIStaker: APIStaker{
			StartTime: 1,
			EndTime:   10,
			NodeID:    nodeID,
		},
		RewardOwner: &APIOwner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []APIUTXO{{
			Amount:  weight,
			Address: addr,
		}},
	}

	args := BuildGenesisArgs{
		AvaxAssetID: ids.ID{'d', 'u', 'm', 'm', 'y', ' ', 'I', 'D'},
		UTXOs: []APIUTXO{
			utxo,
		},
		Validators: []APIPrimaryValidator{
			validator1,
			validator2,
			validator3,
		},
		Time:     5,
		Encoding: formatting.Hex,
	}
	reply := BuildGenesisReply{}

	ss := StaticService{}
	if err := ss.BuildGenesis(nil, &args, &reply); err != nil {
		t.Fatalf("BuildGenesis should not have errored but got error: %s", err)
	}

	genesisBytes, err := formatting.Decode(reply.Encoding, reply.Bytes)
	if err != nil {
		t.Fatalf("Problem decoding BuildGenesis response: %s", err)
	}

	genesis := &Genesis{}
	if _, err := Codec.Unmarshal(genesisBytes, genesis); err != nil {
		t.Fatal(err)
	}
	validators := genesis.Validators
	if len(validators) != 3 {
		t.Fatal("Validators should contain 3 validators")
	}
}
