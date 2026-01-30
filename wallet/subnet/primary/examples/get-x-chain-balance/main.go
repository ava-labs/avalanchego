// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"log"
	"time"

	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/wallet/chain/x"
	"github.com/ava-labs/avalanchego/wallet/chain/x/builder"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

func main() {
	uri := primary.LocalAPIURI
	addrStr := "X-local18jma8ppw3nhx5r4ap8clazz0dps7rv5u00z96u"

	addr, err := address.ParseToID(addrStr)
	if err != nil {
		log.Fatalf("failed to parse address: %s\n", err)
	}

	addresses := set.Of(addr)

	ctx := context.Background()

	fetchStartTime := time.Now()
	state, err := primary.FetchState(ctx, uri, addresses)
	if err != nil {
		log.Fatalf("failed to fetch state: %s\n", err)
	}
	log.Printf("fetched state of %s in %s\n", addrStr, time.Since(fetchStartTime))

	xChainID := state.XCTX.BlockchainID

	xUTXOs := common.NewChainUTXOs(xChainID, state.UTXOs)
	xBackend := x.NewBackend(state.XCTX, xUTXOs)
	xBuilder := builder.New(addresses, state.XCTX, xBackend)

	currentBalances, err := xBuilder.GetFTBalance()
	if err != nil {
		log.Fatalf("failed to get the balance: %s\n", err)
	}

	avaxID := state.XCTX.AVAXAssetID
	avaxBalance := currentBalances[avaxID]
	log.Printf("current AVAX balance of %s is %d nAVAX\n", addrStr, avaxBalance)
}
