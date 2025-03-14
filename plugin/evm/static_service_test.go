// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/stretchr/testify/assert"
)

var testGenesisJSON = `{"config":{"chainId":43111,"homesteadBlock":0,"eip150Block":0,"eip155Block":0,"eip158Block":0,"byzantiumBlock":0,"constantinopleBlock":0,"petersburgBlock":0,"istanbulBlock":0,"muirGlacierBlock":0,"subnetEVMTimestamp":0},"nonce":"0x0","timestamp":"0x0","extraData":"0x00","gasLimit":"0x5f5e100","difficulty":"0x0","mixHash":"0x0000000000000000000000000000000000000000000000000000000000000000","coinbase":"0x0000000000000000000000000000000000000000","alloc":{"0100000000000000000000000000000000000000":{"code":"0x7300000000000000000000000000000000000000003014608060405260043610603d5760003560e01c80631e010439146042578063b6510bb314606e575b600080fd5b605c60048036036020811015605657600080fd5b503560b1565b60408051918252519081900360200190f35b818015607957600080fd5b5060af60048036036080811015608e57600080fd5b506001600160a01b03813516906020810135906040810135906060013560b6565b005b30cd90565b836001600160a01b031681836108fc8690811502906040516000604051808303818888878c8acf9550505050505015801560f4573d6000803e3d6000fd5b505050505056fea26469706673582212201eebce970fe3f5cb96bf8ac6ba5f5c133fc2908ae3dcd51082cfee8f583429d064736f6c634300060a0033","balance":"0x0"}},"number":"0x0","gasUsed":"0x0","parentHash":"0x0000000000000000000000000000000000000000000000000000000000000000"}`

func TestBuildGenesis(t *testing.T) {
	ss := CreateStaticService()

	genesis := &core.Genesis{}
	if err := json.Unmarshal([]byte(testGenesisJSON), genesis); err != nil {
		t.Fatalf("Problem unmarshaling genesis JSON: %s", err)
	}

	// add test allocs
	testAlloc := types.GenesisAlloc{
		testEthAddrs[0]: {Balance: genesisBalance},
		testEthAddrs[1]: {Balance: genesisBalance},
	}
	genesis.Alloc = testAlloc
	params.GetExtra(genesis.Config).FeeConfig = params.DefaultFeeConfig
	testGasLimit := big.NewInt(999999)
	params.GetExtra(genesis.Config).FeeConfig.GasLimit = testGasLimit
	genesis.GasLimit = testGasLimit.Uint64()

	args := &BuildGenesisArgs{GenesisData: genesis}
	reply := &BuildGenesisReply{}
	err := ss.BuildGenesis(nil, args, reply)
	if err != nil {
		t.Fatalf("Failed to create test genesis")
	}
	// now decode
	genesisBytes, err := formatting.Decode(reply.Encoding, reply.GenesisBytes)
	if err != nil {
		t.Fatalf("Failed to decode genesis bytes: %s", err)
	}
	// unmarshal it again
	decodedGenesis := &core.Genesis{}
	decodedGenesis.UnmarshalJSON(genesisBytes)
	// test
	assert.Equal(t, testGasLimit, params.GetExtra(decodedGenesis.Config).FeeConfig.GasLimit)
	assert.Equal(t, testAlloc, decodedGenesis.Alloc)
}

func TestDecodeGenesis(t *testing.T) {
	ss := CreateStaticService()

	genesis := &core.Genesis{}
	if err := json.Unmarshal([]byte(testGenesisJSON), genesis); err != nil {
		t.Fatalf("Problem unmarshaling genesis JSON: %s", err)
	}

	// add test allocs
	testAlloc := types.GenesisAlloc{
		testEthAddrs[0]: {Balance: genesisBalance},
		testEthAddrs[1]: {Balance: genesisBalance},
	}
	genesis.Alloc = testAlloc
	params.GetExtra(genesis.Config).FeeConfig = params.DefaultFeeConfig
	testGasLimit := big.NewInt(999999)
	params.GetExtra(genesis.Config).FeeConfig.GasLimit = testGasLimit
	genesis.GasLimit = testGasLimit.Uint64()

	args := &BuildGenesisArgs{GenesisData: genesis}
	reply := &BuildGenesisReply{}
	err := ss.BuildGenesis(nil, args, reply)
	if err != nil {
		t.Fatalf("Failed to create test genesis")
	}

	// now decode
	decArgs := &DecodeGenesisArgs{GenesisBytes: reply.GenesisBytes}
	decReply := &DecodeGenesisReply{}
	err = ss.DecodeGenesis(nil, decArgs, decReply)
	if err != nil {
		t.Fatalf("Failed to create test genesis")
	}
	decodedGenesis := decReply.Genesis

	// test
	assert.Equal(t, testGasLimit, params.GetExtra(decodedGenesis.Config).FeeConfig.GasLimit)
	assert.Equal(t, testAlloc, decodedGenesis.Alloc)
}
