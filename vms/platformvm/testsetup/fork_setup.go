// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testsetup

import (
	"time"

	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

type ActiveFork uint8

const (
	ApricotPhase3Fork ActiveFork = 0
	ApricotPhase5Fork ActiveFork = 1
	BanffFork         ActiveFork = 2
	CortinaFork       ActiveFork = 3
	DFork             ActiveFork = 4
	LatestFork        ActiveFork = DFork
)

// default times of each P-chain fork
var forkTimes = map[ActiveFork]time.Time{
	ApricotPhase3Fork: ValidateEndTime.Add(-2 * time.Second),   // GenesisTime.Add(1 * time.Second),
	ApricotPhase5Fork: ValidateEndTime.Add(-2 * time.Second),   // GenesisTime.Add(2 * time.Second),
	BanffFork:         ValidateEndTime.Add(-2 * time.Second),   // GenesisTime.Add(3 * time.Second),
	CortinaFork:       ValidateStartTime.Add(-2 * time.Second), // GenesisTime.Add(4 * time.Second),
	DFork:             mockable.MaxTime,                        // GenesisTime.Add(5 * time.Second),
}
