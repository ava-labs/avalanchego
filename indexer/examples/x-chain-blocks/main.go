// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"log"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/indexer"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
	"github.com/ava-labs/avalanchego/wallet/chain/x/builder"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

// This example program continuously polls for the next X-Chain block
// and prints the ID of the block and its transactions.
func main() {
	var (
		uri       = primary.LocalAPIURI + "/ext/index/X/block"
		xChainID  = ids.FromStringOrPanic("2eNy1mUFdmaxXNj1eQHUe7Np4gju9sJsEtWQ4MX3ToiNKuADed")
		client    = indexer.NewClient(uri)
		ctx       = context.Background()
		nextIndex uint64
	)
	for {
		container, err := client.GetContainerByIndex(ctx, nextIndex)
		if err != nil {
			time.Sleep(time.Second)
			log.Println("polling for next accepted block")
			continue
		}

		proposerVMBlock, err := block.Parse(container.Bytes, xChainID)
		if err != nil {
			log.Fatalf("failed to parse proposervm block: %s\n", err)
		}

		avmBlockBytes := proposerVMBlock.Block()
		avmBlock, err := builder.Parser.ParseBlock(avmBlockBytes)
		if err != nil {
			log.Fatalf("failed to parse avm block: %s\n", err)
		}

		acceptedTxs := avmBlock.Txs()
		log.Printf("accepted block %s with %d transactions\n", avmBlock.ID(), len(acceptedTxs))

		for _, tx := range acceptedTxs {
			log.Printf("accepted transaction %s\n", tx.ID())
		}

		nextIndex++
	}
}
