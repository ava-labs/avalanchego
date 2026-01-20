// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bind_test

import (
	"bytes"
	"math/big"
	"os"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/accounts/abi/bind"
	"github.com/ava-labs/avalanchego/graft/coreth/accounts/abi/bind/backends"
	"github.com/ava-labs/avalanchego/graft/coreth/core"
	"github.com/ava-labs/avalanchego/graft/coreth/eth/ethconfig"
	"github.com/ava-labs/avalanchego/graft/coreth/ethclient/simulated"
	"github.com/ava-labs/avalanchego/graft/coreth/node"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/libevm/accounts/abi"
)

func TestMain(m *testing.M) {
	core.RegisterExtras()
	customtypes.Register()
	params.RegisterExtras()
	os.Exit(m.Run())
}

// TestGetSenderNativeAssetCall checks that the NativeAssetCall proxies the
// caller address This behavior is disabled on the network and is only to test
// previous behavior. Note the test uses [params.TestApricotPhase2Config].
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
	const rawABI = `[
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
	const setSenderMethodName = "setSender"
	const getSenderMethodName = "getSender"

	parsedABI, err := abi.JSON(bytes.NewReader([]byte(rawABI)))
	require.NoError(t, err, "Failed to parse ABI")

	// Generate a new random account and a funded simulator
	key, err := crypto.GenerateKey()
	require.NoError(t, err, "Failed to generate key")
	auth, err := bind.NewKeyedTransactorWithChainID(key, big.NewInt(1337))
	require.NoError(t, err, "Failed to create transactor")
	alloc := types.GenesisAlloc{auth.From: {Balance: big.NewInt(1000000000000000000)}}
	atApricotPhase2 := func(_ *node.Config, ethConf *ethconfig.Config) {
		chainConfig := *params.TestApricotPhase2Config
		chainConfig.ChainID = big.NewInt(1337)
		ethConf.Genesis.Config = &chainConfig
	}
	b := simulated.NewBackend(alloc, simulated.WithBlockGasLimit(10000000), atApricotPhase2)
	sim := &backends.SimulatedBackend{
		Backend: b,
		Client:  b.Client(),
	}
	t.Cleanup(func() {
		err = sim.Close()
		require.NoError(t, err, "Failed to close simulator")
	})

	// Deploy the get/setSender contract
	_, _, interactor, err := bind.DeployContract(auth, parsedABI, bytecode, sim)
	require.NoError(t, err, "Failed to deploy interactor contract")
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
	_, err = interactor.Transact(opts, setSenderMethodName)
	require.NoError(t, err, "Failed to set sender")
	sim.Commit(true)

	var results []any
	err = interactor.Call(nil, &results, getSenderMethodName)
	require.NoError(t, err, "Failed to get sender")
	require.Len(t, results, 1)
	addr, ok := results[0].(common.Address)
	require.Truef(t, ok, "Expected %T, got %T", common.Address{}, results[0])
	require.Equal(t, addr, auth.From, "Address mismatch")
}
