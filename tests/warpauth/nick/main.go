// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// nick prints the keyless (Nick's method) deployment of PChain.sol for a
// network: a presigned tx with a made-up signature, the deployer address it
// recovers to, the contract address, and the AVAX the deployer must hold.
//
//	go run ./tests/warpauth/nick -network mainnet
package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"math/big"
	"os"

	"github.com/ava-labs/libevm/crypto"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/tests/warpauth"
	"github.com/ava-labs/avalanchego/utils/constants"
)

func main() {
	network := flag.String("network", "fuji", "mainnet or fuji")
	flag.Parse()

	networkID, err := constants.NetworkID(*network)
	if err != nil {
		fatal(err)
	}
	_, avaxAssetID, err := genesis.FromConfig(genesis.GetConfig(networkID))
	if err != nil {
		fatal(err)
	}
	evmChainID := map[uint32]int64{constants.MainnetID: 43114, constants.FujiID: 43113}[networkID]
	if evmChainID == 0 {
		fatal(fmt.Errorf("no EVM chain ID for network %d", networkID))
	}

	deployTx, deployer, err := warpauth.NickDeployTx(big.NewInt(evmChainID), networkID, avaxAssetID)
	if err != nil {
		fatal(err)
	}
	raw, err := deployTx.MarshalBinary()
	if err != nil {
		fatal(err)
	}
	fmt.Printf("network:   %s (evm chain %d)\n", *network, evmChainID)
	fmt.Printf("deployer:  %s (fund with >= %s wei)\n", deployer, warpauth.NickDeployCost())
	fmt.Printf("contract:  %s\n", crypto.CreateAddress(deployer, 0))
	fmt.Printf("raw tx:    0x%s\n", hex.EncodeToString(raw))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
