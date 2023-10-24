// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"log"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/wallet/chain/p"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

func main() {
	// Use the Fuji Testnet public api server as the uri
	uri := "https://api.avax-test.network/"

	// or
	// Define the URI of the local Avalanche API (Assuming it's running on default port 9650)
	// uri := primary.LocalAPIURI

	// Address you want to query
	addrStr := "P-local18jma8ppw3nhx5r4ap8clazz0dps7rv5u00z96u"

	// Parse the address string into an Avalanche ID
	addr, err := address.ParseToID(addrStr)
	if err != nil {
		log.Fatalf("failed to parse address: %s\n", err)
	}

	// Create a set of addresses for querying (only one in this case)
	addresses := set.Of(addr)

	// Create a context for the API request
	ctx := context.Background()

	// Measure the time it takes to fetch the state
	fetchStartTime := time.Now()

	// Fetch the state of the specified address
	state, err := primary.FetchState(ctx, uri, addresses)
	if err != nil {
		log.Fatalf("failed to fetch state: %s\n", err)
	}

	// Print the time it took to fetch the state
	log.Printf("fetched state of %s in %s\n", addrStr, time.Since(fetchStartTime))

	// Create a new UTXOs set for the platform chain
	pUTXOs := primary.NewChainUTXOs(constants.PlatformChainID, state.UTXOs)

	// Create a new platform chain backend
	pBackend := p.NewBackend(state.PCTX, pUTXOs, make(map[ids.ID]*txs.Tx))

	// Create a builder for querying the balance
	pBuilder := p.NewBuilder(addresses, pBackend)

	// Get the current balance for the specified address
	currentBalances, err := pBuilder.GetBalance()
	if err != nil {
		log.Fatalf("failed to get the balance: %s\n", err)
	}

	// Get the asset ID for AVAX
	avaxID := state.PCTX.AVAXAssetID()

	// Get the AVAX balance for the specified address
	avaxBalance := currentBalances[avaxID]

	// Print the AVAX balance
	log.Printf("current AVAX balance of %s is %d nAVAX\n", addrStr, avaxBalance)
}
