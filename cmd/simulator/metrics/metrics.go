// (c) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/ethclient"
	"github.com/ethereum/go-ethereum/params"
)

const (
	checkInterval = 2 * time.Second
)

// MonitorTPS periodically prints metrics related to transaction activity on
// a given network.
func MonitorTPS(ctx context.Context, client ethclient.Client) error {
	lastBlockNumber, err := client.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("failed to get block number: %w", err)
	}
	block, err := client.BlockByNumber(ctx, new(big.Int).SetUint64(lastBlockNumber))
	if err != nil {
		return fmt.Errorf("failed to get block at number %d: %w", lastBlockNumber, err)
	}

	startTime := block.Time()
	currentTime := startTime
	totalTxs := 0
	timeTxs := make(map[uint64]int)
	for ctx.Err() == nil {
		newBlockNumber, err := client.BlockNumber(ctx)
		if err != nil {
			log.Printf("failed to get block number: %s", err)
			time.Sleep(checkInterval)
			continue
		}

		if newBlockNumber <= lastBlockNumber {
			time.Sleep(checkInterval)
			continue
		}

		var block *types.Block
		for i := lastBlockNumber + 1; i <= newBlockNumber; i++ {
			for ctx.Err() == nil {
				block, err = client.BlockByNumber(ctx, new(big.Int).SetUint64(i))
				if err != nil {
					log.Printf("failed to get block at number %d: %s", i, err)
					time.Sleep(checkInterval)
					continue
				}
				log.Printf("[block created] index: %d base fee: %d block gas cost: %d block txs: %d\n", i, block.BaseFee().Div(block.BaseFee(), big.NewInt(params.GWei)), block.BlockGasCost(), len(block.Body().Transactions))
				timeTxs[block.Time()] += len(block.Transactions())
				totalTxs += len(block.Transactions())
				if i == 1 { // genesis is usually the unix epoch
					startTime = block.Time()
				}
				break
			}
		}
		lastBlockNumber = newBlockNumber
		currentTime = block.Time()

		// log TPS
		tenStxs := 0
		for i := uint64(currentTime - 10); i < currentTime; i++ { // ensure only looking at closed times
			tenStxs += timeTxs[i]
		}
		if currentTime-startTime > 0 {
			log.Printf(
				"[stats] historical TPS: %f last 10s TPS: %f total txs: %d total time(s): %v\n",
				float64(totalTxs)/float64(currentTime-startTime), float64(tenStxs)/float64(10),
				totalTxs, currentTime-startTime,
			)
		}
	}
	return ctx.Err()
}
