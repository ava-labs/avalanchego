// (c) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bind_test

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ava-labs/coreth/accounts/abi"
	"github.com/ava-labs/coreth/accounts/abi/bind"
	"github.com/ava-labs/coreth/accounts/abi/bind/backends"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/eth/ethconfig"
	"github.com/ava-labs/coreth/ethclient/simulated"
	"github.com/ava-labs/coreth/node"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/crypto"
)

// TestGetSenderNativeAssetCall checks that the NativeAssetCall proxies the
// caller address This behavior is disabled on the network and is only to test
// previous behavior. Note the test uses ApricotPhase2Config.
func TestGetSenderNativeAssetCall(t *testing.T) {
	// pragma solidity >=0.8.0 <0.9.0;
	// contract GetSenderNativeAssetCall {
	// 	address _sender;
	// 	function getSender() public view returns (address){
	// 			return _sender;
	// 	}
	// 	function setSender() public {
	// 			_sender = msg.sender;
	// 	}
	// }
	rawABI := `[
			{
					"inputs": [],
				"name": "getSender",
				"outputs": [ { "internalType": "address", "name": "", "type": "address" } ],
				"stateMutability": "view",
				"type": "function"
			},
			{
				"inputs": [],
				"name": "setSender",
				"outputs": [],
				"stateMutability": "nonpayable",
				"type": "function"
			}
	]`
	bytecode := common.FromHex(`6080604052348015600f57600080fd5b506101608061001f6000396000f3fe608060405234801561001057600080fd5b50600436106100365760003560e01c806350c36a521461003b5780635e01eb5a14610045575b600080fd5b610043610063565b005b61004d6100a5565b60405161005a919061010f565b60405180910390f35b336000806101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff160217905550565b60008060009054906101000a900473ffffffffffffffffffffffffffffffffffffffff16905090565b600073ffffffffffffffffffffffffffffffffffffffff82169050919050565b60006100f9826100ce565b9050919050565b610109816100ee565b82525050565b60006020820190506101246000830184610100565b9291505056fea26469706673582212209023ce54f38e749b58f44e8da750354578080ce16df95037b7305ed7e480c36d64736f6c634300081b0033`)
	setSenderMethodName := "setSender"
	getSenderMethodName := "getSender"

	parsedABI, err := abi.JSON(bytes.NewReader([]byte(rawABI)))
	if err != nil {
		t.Fatalf("Failed to parse ABI: %v", err)
	}

	// Generate a new random account and a funded simulator
	key, _ := crypto.GenerateKey()
	auth, _ := bind.NewKeyedTransactorWithChainID(key, big.NewInt(1337))
	alloc := types.GenesisAlloc{auth.From: {Balance: big.NewInt(1000000000000000000)}}
	atApricotPhase2 := func(nodeConf *node.Config, ethConf *ethconfig.Config) {
		chainConfig := *params.TestApricotPhase2Config
		chainConfig.ChainID = big.NewInt(1337)
		ethConf.Genesis.Config = &chainConfig
	}
	b := simulated.NewBackend(alloc, simulated.WithBlockGasLimit(10000000), atApricotPhase2)
	sim := &backends.SimulatedBackend{
		Backend: b,
		Client:  b.Client(),
	}
	defer sim.Close()

	// Deploy the get/setSender contract
	_, _, interactor, err := bind.DeployContract(auth, parsedABI, bytecode, sim)
	if err != nil {
		t.Fatalf("Failed to deploy interactor contract: %v", err)
	}
	sim.Commit(false)

	// Setting NativeAssetCall in the transact opts will proxy the call through
	// the NativeAssetCall precompile
	opts := &bind.TransactOpts{
		From:   auth.From,
		Signer: auth.Signer,
		NativeAssetCall: &bind.NativeAssetCallOpts{
			AssetAmount: big.NewInt(0),
		},
	}
	if _, err := interactor.Transact(opts, setSenderMethodName); err != nil {
		t.Fatalf("Failed to set sender: %v", err)
	}
	sim.Commit(true)

	var results []interface{}
	if err := interactor.Call(nil, &results, getSenderMethodName); err != nil {
		t.Fatalf("Failed to get sender: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Expected one result, got %d", len(results))
	}
	if addr, ok := results[0].(common.Address); !ok {
		t.Fatalf("Expected address, got %T", results[0])
	} else if addr != auth.From {
		t.Fatalf("Address mismatch: have '%v'", addr)
	}
}
