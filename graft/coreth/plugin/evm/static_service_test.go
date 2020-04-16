// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"math/big"
	"testing"

	"github.com/ava-labs/go-ethereum/common"
	"github.com/ava-labs/go-ethereum/params"

	"github.com/ava-labs/coreth/core"
)

func TestBuildGenesis(t *testing.T) {
	expected := "3wP629bGfSGj9trh1UNBp5qGRGCcma5d8ezLeSmd9hnUJjSMUJesHHoxbZNcVUC9CjH7PEGNA96htNTd1saZCMt1Mf1dZFG7JDhcYNok6RS4TZufejXdxbVVgquohSa7nCCcrXpiVeiRFwzLJAxyQbXzYRhaCRtcDDfCcqfaVdtkFsPbNeQ49pDTbEC5hVkmfopeQ2Zz8tAG5QXKBdbYBCukR3xNHJ4xDxeixmEwPr1odb42yQRYrL7xREKNn2LFoFwAWUjBTsCkf5GPNgY2GvvN9o8wFWXTroW5fp754DhpdxHYxkMTfuE9DGyNWHTyrEbrUHutUdsfitcSHVj5ctFtkN2wGCs3cyv1eRRNvFFMggWTbarjne6AYaeCrJ631qAu3CbrUtrTH5N2E6G2yQKX4sT4Sk3qWPJdsGXuT95iKKcgNn1u5QRHHw9DXXuGPpJjkcKQRGUCuqpXy61iF5RNPEwAwKDa8f2Y25WMmNgWynUuLj8iSAyePj7USPWk54QFUr86ApVzqAdzzdD1qSVScpmudGnGbz9UNXdzHqSot6XLrNTYsgkabiu6TGntFm7qywbCRmtNdBuT9aznGQdUVimjt5QzUz68HXhUxBzTkrz7yXfVGV5JcWxVHQXYS4oc41U5yu83mH3A7WBrZLVq6UyNrvQVbim5nDxeKKbALPxwzVwywjgY5cp39AvzGnY8CX2AtuBNnKmZaAvG8JWAkx3yxjnJrwWhLgpDQYcCvRp2jg1EPBqN8FKJxSPE6eedjDHDJfB57mNzyEtmg22BPnem3eLdiovX8awkhBUHdE7uPrapNSVprnS85u1saW2Kwza3FsS2jAM3LckGW8KdtfPTpHBTRKAUo49zZLuPsyGL5WduedGyAdaM3a2KPoyXuz4UbexTVUWFNypFvvgyoDS8FMxDCNoMMaD7y4yVnoDpSpVFEVZD6EuSGHe9U8Ew57xLPbjhepDx6"

	balance, success := new(big.Int).SetString("33b2e3c9fd0804000000000", 16)
	if !success {
		t.Fatal("Failed to initialize balance")
	}

	args := core.Genesis{
		Config: &params.ChainConfig{
			ChainID:             big.NewInt(43110),
			HomesteadBlock:      big.NewInt(0),
			DAOForkBlock:        big.NewInt(0),
			DAOForkSupport:      true,
			EIP150Block:         big.NewInt(0),
			EIP150Hash:          common.HexToHash("0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0"),
			EIP155Block:         big.NewInt(0),
			EIP158Block:         big.NewInt(0),
			ByzantiumBlock:      big.NewInt(0),
			ConstantinopleBlock: big.NewInt(0),
			PetersburgBlock:     big.NewInt(0),
		},
		Nonce:      0,
		Timestamp:  0,
		ExtraData:  []byte{},
		GasLimit:   100000000,
		Difficulty: big.NewInt(0),
		Mixhash:    common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
		Coinbase:   common.HexToAddress("0x0000000000000000000000000000000000000000"),
		Alloc: core.GenesisAlloc{
			common.HexToAddress("751a0b96e1042bee789452ecb20253fba40dbe85"): core.GenesisAccount{
				Balance: balance,
			},
		},
		Number:     0,
		GasUsed:    0,
		ParentHash: common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000"),
	}

	ss := StaticService{}
	result, err := ss.BuildGenesis(nil, &args)
	if err != nil {
		t.Fatal(err)
	}

	if result.String() != expected {
		t.Fatalf("StaticService.BuildGenesis:\nReturned: %s\nExpected: %s", result, expected)
	}
}
