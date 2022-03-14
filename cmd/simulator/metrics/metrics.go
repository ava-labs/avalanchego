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

// MonitorTPS periodically prints metrics related to transaction activity on
// a given network.
func MonitorTPS(ctx context.Context, client ethclient.Client) error {
	blockNumber, err := client.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("failed to get block number: %w", err)
	}

	block, err := client.BlockByNumber(ctx, new(big.Int).SetUint64(blockNumber))
	if err != nil {
		return fmt.Errorf("failed to get block at number %d: %w", blockNumber, err)
	}

	startTime := block.Time()
	currentTime := startTime
	totalTxs := 0
	timeTxs := map[uint64]int{}
	for ctx.Err() == nil {
		newBlockNumber, err := client.BlockNumber(ctx)
		if err != nil {
			log.Printf("failed to get block number: %s", err)
			time.Sleep(2 * time.Second)
			continue
		}

		if blockNumber > newBlockNumber {
			time.Sleep(2 * time.Second)
			continue
		}

		var block *types.Block
		for i := blockNumber; i <= newBlockNumber; i++ {
			for ctx.Err() == nil {
				block, err = client.BlockByNumber(ctx, new(big.Int).SetUint64(i))
				if err != nil {
					log.Printf("failed to get block at number %d: %s", blockNumber, err)
					time.Sleep(2 * time.Second)
					continue
				}
				log.Printf("[block created] index: %d base fee: %d block gas cost: %d block txs: %d\n", i, block.BaseFee().Div(block.BaseFee(), big.NewInt(params.GWei)), block.BlockGasCost(), len(block.Body().Transactions))
				timeTxs[block.Time()] += len(block.Transactions())
				totalTxs += len(block.Transactions())
				break
			}
		}
		blockNumber = newBlockNumber + 1
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
				totalTxs, time.Duration(currentTime-startTime)*time.Second,
			)
		}
	}
	return ctx.Err()
}
