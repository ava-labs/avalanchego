// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
)

const testNetworkID = 10 // To be used in tests

func TestBuildGenesisInvalidUTXOBalance(t *testing.T) {
	nodeID := ids.NodeID{1, 2, 3}
	hrp := constants.NetworkIDToHRP[testNetworkID]
	addr, err := address.FormatBech32(hrp, nodeID.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	utxo := UTXO{
		Address: addr,
		Amount:  0,
	}
	weight := json.Uint64(987654321)
	validator := PermissionlessValidator{
		Staker: Staker{
			EndTime: 15,
			Weight:  &weight,
			NodeID:  nodeID,
		},
		RewardOwner: &Owner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []UTXO{{
			Amount:  weight,
			Address: addr,
		}},
	}

	args := BuildGenesisArgs{
		UTXOs: []UTXO{
			utxo,
		},
		Validators: []PermissionlessValidator{
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
	nodeID := ids.NodeID{1, 2, 3}
	hrp := constants.NetworkIDToHRP[testNetworkID]
	addr, err := address.FormatBech32(hrp, nodeID.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	utxo := UTXO{
		Address: addr,
		Amount:  123456789,
	}
	weight := json.Uint64(0)
	validator := PermissionlessValidator{
		Staker: Staker{
			StartTime: 0,
			EndTime:   15,
			NodeID:    nodeID,
		},
		RewardOwner: &Owner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []UTXO{{
			Amount:  weight,
			Address: addr,
		}},
	}

	args := BuildGenesisArgs{
		UTXOs: []UTXO{
			utxo,
		},
		Validators: []PermissionlessValidator{
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
	nodeID := ids.NodeID{1, 2, 3}
	hrp := constants.NetworkIDToHRP[testNetworkID]
	addr, err := address.FormatBech32(hrp, nodeID.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	utxo := UTXO{
		Address: addr,
		Amount:  123456789,
	}

	weight := json.Uint64(987654321)
	validator := PermissionlessValidator{
		Staker: Staker{
			StartTime: 0,
			EndTime:   5,
			NodeID:    nodeID,
		},
		RewardOwner: &Owner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []UTXO{{
			Amount:  weight,
			Address: addr,
		}},
	}

	args := BuildGenesisArgs{
		UTXOs: []UTXO{
			utxo,
		},
		Validators: []PermissionlessValidator{
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
	nodeID := ids.NodeID{1}
	hrp := constants.NetworkIDToHRP[testNetworkID]
	addr, err := address.FormatBech32(hrp, nodeID.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	utxo := UTXO{
		Address: addr,
		Amount:  123456789,
	}

	weight := json.Uint64(987654321)
	validator1 := PermissionlessValidator{
		Staker: Staker{
			StartTime: 0,
			EndTime:   20,
			NodeID:    nodeID,
		},
		RewardOwner: &Owner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []UTXO{{
			Amount:  weight,
			Address: addr,
		}},
	}

	validator2 := PermissionlessValidator{
		Staker: Staker{
			StartTime: 3,
			EndTime:   15,
			NodeID:    nodeID,
		},
		RewardOwner: &Owner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []UTXO{{
			Amount:  weight,
			Address: addr,
		}},
	}

	validator3 := PermissionlessValidator{
		Staker: Staker{
			StartTime: 1,
			EndTime:   10,
			NodeID:    nodeID,
		},
		RewardOwner: &Owner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []UTXO{{
			Amount:  weight,
			Address: addr,
		}},
	}

	args := BuildGenesisArgs{
		AvaxAssetID: ids.ID{'d', 'u', 'm', 'm', 'y', ' ', 'I', 'D'},
		UTXOs: []UTXO{
			utxo,
		},
		Validators: []PermissionlessValidator{
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

	genesis, err := genesis.Parse(genesisBytes)
	if err != nil {
		t.Fatal(err)
	}
	validators := genesis.Validators
	if len(validators) != 3 {
		t.Fatal("Validators should contain 3 validators")
	}
}
