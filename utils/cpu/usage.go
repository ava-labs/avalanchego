// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cpu

import (
	"os"
	"sync"
	"time"

	"github.com/struCoder/pidusage"
)

const (
	sampleFrequency = 100 * time.Millisecond
	newPointWeight  = .05
	oldPointWeight  = 1 - newPointWeight
)

var (
	runningAverageLock sync.RWMutex
	runningAverage     float64
)

func init() {
	go func() {
		for {
			info, err := pidusage.GetStat(os.Getpid())

			var scaledCPU float64
			if err == nil {
				scaledCPU = newPointWeight * info.CPU / 100
			}

			runningAverageLock.Lock()
			runningAverage = oldPointWeight*runningAverage + scaledCPU
			runningAverageLock.Unlock()

			time.Sleep(sampleFrequency)
		}
	}()
}

func Usage() float64 {
	runningAverageLock.RLock()
	cpu := runningAverage
	runningAverageLock.RUnlock()
	return cpu
}
