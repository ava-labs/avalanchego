// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"log"
	"time"

	"github.com/ava-labs/avalanchego/indexer"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"

	platformvmblock "github.com/ava-labs/avalanchego/vms/platformvm/block"
	proposervmblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
)

// This example program continuously polls for the next P-Chain block
// and prints the ID of the block and its transactions.
func main() {
	var (
		uri       = primary.LocalAPIURI + "/ext/index/P/block"
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

		platformvmBlockBytes := container.Bytes
		proposerVMBlock, err := proposervmblock.Parse(container.Bytes, constants.PlatformChainID)
		if err == nil {
			platformvmBlockBytes = proposerVMBlock.Block()
		}

		platformvmBlock, err := platformvmblock.Parse(platformvmblock.Codec, platformvmBlockBytes)
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
