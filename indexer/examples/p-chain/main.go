// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ava-labs/avalanchego/indexer"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

// This example program continuously polls for the next P-Chain block
// and prints the ID of the block and its transactions.
func main() {
	var (
		uri       = fmt.Sprintf("%s/ext/index/P/block", primary.LocalAPIURI)
		client    = indexer.NewClient(uri)
		ctx       = context.Background()
		nextIndex uint64
	)
	for {
		container, err := client.GetContainerByIndex(ctx, nextIndex)
		if err != nil {
			time.Sleep(time.Second)
			log.Printf("polling for next accepted block\n")
			continue
		}

		platformvmBlockBytes := container.Bytes
		proposerVMBlock, err := block.Parse(container.Bytes)
		if err == nil {
			platformvmBlockBytes = proposerVMBlock.Block()
		}

		platformvmBlock, err := blocks.Parse(blocks.Codec, platformvmBlockBytes)
		if err != nil {
			log.Fatalf("failed to parse platformvm block: %s\n", err)
		}

		acceptedTxs := platformvmBlock.Txs()
		log.Printf("accepted block %s with %d transactions\n", platformvmBlock.ID(), len(acceptedTxs))

		for _, tx := range acceptedTxs {
			log.Printf("accepted transaction %s\n", tx.ID())
		}

		nextIndex++
	}
}
