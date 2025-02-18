// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/ava-labs/firewood/ffi/v2"
)

func runSingleBenchmark(databaseEntries uint64) error {
	fwdb := new(firewood.Firewood)

	blockHeight := (databaseEntries + databaseCreationBatchSize - 1) / databaseCreationBatchSize

	batchIdx := uint64(0)
	entriesKeys := make([][]byte, 10000)
	for i := range entriesKeys {
		entriesKeys[i] = calculateIndexEncoding(uint64(i))
	}
	var updateDuration, batchDuration time.Duration
	entriesToUpdate := uint64(1)
	lastChangeEntriesCount := time.Now()
	for {
		startBatchTime := time.Now()

		// update a single entry, at random.
		startUpdateTime := time.Now()
		rows := make([]firewood.KeyValue, entriesToUpdate)
		for i := uint64(0); i < entriesToUpdate; i++ {
			rows[i] = firewood.KeyValue{Key: entriesKeys[i], Value: binary.BigEndian.AppendUint64(nil, batchIdx*10000+i)}
		}

		updateDuration = time.Since(startUpdateTime)
		stats.updateRate.Set(float64(databaseRunningUpdateSize) * float64(time.Second) / float64(updateDuration))
		stats.updates.Add(databaseRunningUpdateSize)

		batchWriteStartTime := time.Now()
		fwdb.Batch(rows)
		blockHeight++
		batchDuration = time.Since(startBatchTime)
		batchWriteDuration := time.Since(batchWriteStartTime)
		stats.batchWriteRate.Set(float64(time.Second) / float64(batchWriteDuration))

		if *verbose {
			fmt.Printf("update rate [%d]\tbatch rate [%d]\tbatch size [%d]\n",
				time.Second/updateDuration,
				time.Second/batchDuration,
				entriesToUpdate)
		}

		stats.batches.Inc()
		batchIdx++

		if time.Since(lastChangeEntriesCount) > 5*time.Minute {
			lastChangeEntriesCount = time.Now()
			entriesToUpdate *= 10
			if entriesToUpdate > 10000 {
				entriesToUpdate = 1
			}
		}
	}
}
