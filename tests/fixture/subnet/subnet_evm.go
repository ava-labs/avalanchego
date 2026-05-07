// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnet

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/crypto"

	_ "embed"

	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
)

var (
	//go:embed subnet-genesis.json
	subnetGenesis               []byte
	subnetEVMDefaultChainConfig = map[string]any{
		"log-level":         "debug",
		"local-txs-enabled": true,
	}

	errGenesisAlloc = errors.New("subnet genesis alloc is not object")
)

func NewSubnetEVMSubnetOrPanic(name string, preFundedKeys []*secp256k1.PrivateKey, nodes ...*tmpnet.Node) *tmpnet.Subnet {
	if len(nodes) == 0 {
		panic("a subnet must be validated by at least one node")
	}

	chainConfigBytes, err := json.Marshal(subnetEVMDefaultChainConfig)
	if err != nil {
		panic(err)
	}
	fundAmount := new(big.Int).Mul(big.NewInt(200), big.NewInt(params.Ether))
	subnetEVMGenesisWithAlloc, err := withPrefundedAlloc(subnetGenesis, preFundedKeys, fundAmount)
	if err != nil {
		panic(err)
	}
	return &tmpnet.Subnet{
		Name: name,
		Chains: []*tmpnet.Chain{
			{
				VMID:         constants.SubnetEVMID,
				Genesis:      subnetEVMGenesisWithAlloc,
				Config:       string(chainConfigBytes),
				PreFundedKey: preFundedKeys[0],
			},
		},
		ValidatorIDs: tmpnet.NodesToIDs(nodes...),
	}
}

// withPrefundedAlloc adds the given keys to the genesis alloc with the given balance.
// This intentionally avoids unmarshalling the genesis bytes to avoid any potential issues
// with requiring subnet-evm register hooks.
func withPrefundedAlloc(genesisBytes []byte, keys []*secp256k1.PrivateKey, balance *big.Int) ([]byte, error) {
	var genesisMap map[string]any
	if err := json.Unmarshal(genesisBytes, &genesisMap); err != nil {
		return nil, fmt.Errorf("unmarshal subnet genesis: %w", err)
	}

	allocAny, ok := genesisMap["alloc"]
	if !ok {
		allocAny = map[string]any{}
		genesisMap["alloc"] = allocAny
	}
	alloc, ok := allocAny.(map[string]any)
	if !ok {
		return nil, errGenesisAlloc
	}

	balanceHex := "0x" + balance.Text(16)
	for _, key := range keys {
		addr := crypto.PubkeyToAddress(key.ToECDSA().PublicKey).Hex()
		alloc[addr] = map[string]any{
			"balance": balanceHex,
		}
	}

	updatedGenesis, err := json.Marshal(genesisMap)
	if err != nil {
		return nil, fmt.Errorf("marshal subnet genesis: %w", err)
	}
	return updatedGenesis, nil
}
