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
	uri := primary.LocalAPIURI
	addrStr := "P-local18jma8ppw3nhx5r4ap8clazz0dps7rv5u00z96u"

	addr, err := address.ParseToID(addrStr)
	if err != nil {
		log.Fatalf("failed to parse address: %s\n", err)
	}

	addresses := set.Set[ids.ShortID]{}
	addresses.Add(addr)

	ctx := context.Background()

	fetchStartTime := time.Now()
	pCtx, _, utxos, err := primary.FetchState(ctx, uri, addresses)
	if err != nil {
		log.Fatalf("failed to fetch state: %s\n", err)
	}
	log.Printf("fetched state of %s in %s\n", addrStr, time.Since(fetchStartTime))

	pUTXOs := primary.NewChainUTXOs(constants.PlatformChainID, utxos)
	pBackend := p.NewBackend(pCtx, pUTXOs, make(map[ids.ID]*txs.Tx))
	pBuilder := p.NewBuilder(addresses, pBackend)

	currentBalances, err := pBuilder.GetBalance()
	if err != nil {
		log.Fatalf("failed to get the balance: %s\n", err)
	}

	avaxID := pCtx.AVAXAssetID()
	avaxBalance := currentBalances[avaxID]
	log.Printf("current AVAX balance of %s is %d nAVAX\n", addrStr, avaxBalance)
}
