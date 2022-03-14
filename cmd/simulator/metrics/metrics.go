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

// Monitor periodically prints metrics related to transaction activity on
// a given network.
func Monitor(ctx context.Context, client ethclient.Client) error {
	lastBlockNumber, err := client.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("failed to get block number: %w", err)
	}
	startTime := uint64(time.Now().Unix())
	currentTime := startTime
	totalTxs := 0
	timeTxs := make(map[uint64]int)
	totalGas := uint64(0)
	timeGas := make(map[uint64]uint64)
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
				txs := len(block.Transactions())
				gas := block.GasUsed()
				t := block.Time()

				log.Printf("[block created] t: %v index: %d base fee: %d block gas cost: %d block txs: %d gas used: %d\n", time.Unix(int64(t), 0), i, block.BaseFee().Div(block.BaseFee(), big.NewInt(params.GWei)), block.BlockGasCost(), txs, gas)

				// Update Tx Count
				timeTxs[t] += txs
				totalTxs += txs

				// Update Gas Usage
				timeGas[t] += gas
				totalGas += gas
				break
			}
		}
		lastBlockNumber = newBlockNumber
		currentTime = block.Time()

		// log 10s
		tenStxs := 0
		tenSgas := uint64(0)
		for i := uint64(currentTime - 10); i < currentTime; i++ { // ensure only looking at closed times
			tenStxs += timeTxs[i]
			tenSgas += timeGas[i]
		}
		diff := currentTime - startTime
		if diff > 0 {
			log.Printf(
				"[stats] historical TPS: %.2f last 10s TPS: %.2f total txs: %d historical GPS: %.1f, last 10s GPS: %.1f elapsed: %v\n",
				float64(totalTxs)/float64(diff), float64(tenStxs)/float64(10), totalTxs,
				float64(totalGas)/float64(diff), float64(tenSgas)/float64(10),
				time.Duration(diff)*time.Second,
			)
		}
	}
	return ctx.Err()
}
