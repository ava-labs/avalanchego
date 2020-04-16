// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"encoding/json"
	"testing"

	"github.com/ava-labs/coreth/core"
)

func TestParseGenesis(t *testing.T) {
	genesis := []byte(`{"config":{"chainId":43110,"homesteadBlock":0,"daoForkBlock":0,"daoForkSupport":true,"eip150Block":0,"eip150Hash":"0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0","eip155Block":0,"eip158Block":0,"byzantiumBlock":0,"constantinopleBlock":0,"petersburgBlock":0},"nonce":"0x0","timestamp":"0x0","extraData":"0x00","gasLimit":"0x5f5e100","difficulty":"0x0","mixHash":"0x0000000000000000000000000000000000000000000000000000000000000000","coinbase":"0x0000000000000000000000000000000000000000","alloc":{"751a0b96e1042bee789452ecb20253fba40dbe85":{"balance":"0x33b2e3c9fd0804000000000"}},"number":"0x0","gasUsed":"0x0","parentHash":"0x0000000000000000000000000000000000000000000000000000000000000000"}`)

	genesisBlock := new(core.Genesis)
	err := json.Unmarshal(genesis, genesisBlock)
	if err != nil {
		t.Fatal(err)
	}

	marshalledBytes, err := json.Marshal(genesisBlock)
	if err != nil {
		t.Fatal(err)
	}

	secondGenesisBlock := new(core.Genesis)
	err = json.Unmarshal(marshalledBytes, secondGenesisBlock)
	if err != nil {
		t.Fatal(err)
	}
}
