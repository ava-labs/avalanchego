// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"encoding/json"
	"log"
	"math/big"
	"time"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/holiman/uint256"
)

func main() {
	key := genesis.EWOQKey
	uri := primary.LocalAPIURI
	kc := secp256k1fx.NewKeychain(key)
	subnetIDStr := "BKBZ6xXTnT86B4L5fp8rvtcmNSpvtNz8En9jG61ywV2uWyeHy"

	eoa := crypto.PubkeyToAddress(*key.PublicKey().ToECDSA())
	genesis := &core.Genesis{
		Config:     params.TestChainConfig,
		Timestamp:  1749420951,
		Difficulty: big.NewInt(0), // required by geth
		Alloc: types.GenesisAlloc{
			eoa: {
				Balance: new(uint256.Int).Not(uint256.NewInt(0)).ToBig(),
			},
		},
	}
	genesisBytes, err := json.Marshal(genesis)
	if err != nil {
		log.Fatalf("failed to marshal genesis: %s\n", err)
	}

	vmID := constants.StrEVMID
	name := "gofast"

	subnetID, err := ids.FromString(subnetIDStr)
	if err != nil {
		log.Fatalf("failed to parse subnet ID: %s\n", err)
	}

	ctx := context.Background()

	// MakePWallet fetches the available UTXOs owned by [kc] on the P-chain that
	// [uri] is hosting and registers [subnetID].
	walletSyncStartTime := time.Now()
	wallet, err := primary.MakePWallet(
		ctx,
		uri,
		kc,
		primary.WalletConfig{
			SubnetIDs: []ids.ID{subnetID},
		},
	)
	if err != nil {
		log.Fatalf("failed to initialize wallet: %s\n", err)
	}
	log.Printf("synced wallet in %s\n", time.Since(walletSyncStartTime))

	createChainStartTime := time.Now()
	createChainTx, err := wallet.IssueCreateChainTx(
		subnetID,
		genesisBytes,
		vmID,
		nil,
		name,
	)
	if err != nil {
		log.Fatalf("failed to issue create chain transaction: %s\n", err)
	}
	log.Printf("created new chain %s in %s\n", createChainTx.ID(), time.Since(createChainStartTime))
}
