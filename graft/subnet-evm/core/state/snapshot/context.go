// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2022 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package snapshot

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"

	"golang.org/x/exp/slog"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/log"
)

// generatorStats is a collection of statistics gathered by the snapshot generator
// for logging purposes.
type generatorStats struct {
	wiping   chan struct{}      // Notification channel if wiping is in progress
	origin   uint64             // Origin prefix where generation started
	start    time.Time          // Timestamp when generation started
	accounts uint64             // Number of accounts indexed(generated or recovered)
	slots    uint64             // Number of storage slots indexed(generated or recovered)
	storage  common.StorageSize // Total account and storage slot size(generation or recovery)
}

// Info creates an contextual info-level log with the given message and the context pulled
// from the internally maintained statistics.
func (gs *generatorStats) Info(msg string, root common.Hash, marker []byte) {
	gs.log(log.LvlInfo, msg, root, marker)
}

// Debug creates an contextual debug-level log with the given message and the context pulled
// from the internally maintained statistics.
func (gs *generatorStats) Debug(msg string, root common.Hash, marker []byte) {
	gs.log(log.LvlDebug, msg, root, marker)
}

// log creates an contextual log with the given message and the context pulled
// from the internally maintained statistics.
func (gs *generatorStats) log(level slog.Level, msg string, root common.Hash, marker []byte) {
	var ctx []interface{}
	if root != (common.Hash{}) {
		ctx = append(ctx, []interface{}{"root", root}...)
	}
	// Figure out whether we're after or within an account
	switch len(marker) {
	case common.HashLength:
		ctx = append(ctx, []interface{}{"at", common.BytesToHash(marker)}...)
	case 2 * common.HashLength:
		ctx = append(ctx, []interface{}{
			"in", common.BytesToHash(marker[:common.HashLength]),
			"at", common.BytesToHash(marker[common.HashLength:]),
		}...)
	}
	// Add the usual measurements
	ctx = append(ctx, []interface{}{
		"accounts", gs.accounts,
		"slots", gs.slots,
		"storage", gs.storage,
		"elapsed", common.PrettyDuration(time.Since(gs.start)),
	}...)
	// Calculate the estimated indexing time based on current stats
	if len(marker) > 0 {
		if done := binary.BigEndian.Uint64(marker[:8]) - gs.origin; done > 0 {
			left := math.MaxUint64 - binary.BigEndian.Uint64(marker[:8])

			speed := done/uint64(time.Since(gs.start)/time.Millisecond+1) + 1 // +1s to avoid division by zero
			ctx = append(ctx, []interface{}{
				"eta", common.PrettyDuration(time.Duration(left/speed) * time.Millisecond),
			}...)
		}
	}

	switch level {
	case log.LvlTrace:
		log.Trace(msg, ctx...)
	case log.LvlDebug:
		log.Debug(msg, ctx...)
	case log.LvlInfo:
		log.Info(msg, ctx...)
	case log.LevelWarn:
		log.Warn(msg, ctx...)
	case log.LevelError:
		log.Error(msg, ctx...)
	case log.LevelCrit:
		log.Crit(msg, ctx...)
	default:
		log.Error(fmt.Sprintf("log with invalid log level %s: %s", level, msg), ctx...)
	}
}
