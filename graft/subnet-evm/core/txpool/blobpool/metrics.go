// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
// Copyright 2023 The go-ethereum Authors
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

package blobpool

import (
	"github.com/ava-labs/libevm/metrics"

	// Force libevm metrics of the same name to be registered first.
	_ "github.com/ava-labs/libevm/core/txpool/blobpool"
)

// ====== If resolving merge conflicts ======
//
// All calls to metrics.NewRegistered*() for metrics also defined in libevm/core/txpool/blobpool
// have been replaced with metrics.GetOrRegister*() to get metrics already registered in
// libevm/core/txpool/blobpool or register them here otherwise. These replacements ensure the
// same metrics are shared between the two packages.
var (
	// datacapGauge tracks the user's configured capacity for the blob pool. It
	// is mostly a way to expose/debug issues.
	datacapGauge = metrics.GetOrRegisterGauge("blobpool/datacap", nil)

	// The below metrics track the per-datastore metrics for the primary blob
	// store and the temporary limbo store.
	datausedGauge = metrics.GetOrRegisterGauge("blobpool/dataused", nil)
	datarealGauge = metrics.GetOrRegisterGauge("blobpool/datareal", nil)
	slotusedGauge = metrics.GetOrRegisterGauge("blobpool/slotused", nil)

	limboDatausedGauge = metrics.GetOrRegisterGauge("blobpool/limbo/dataused", nil)
	limboDatarealGauge = metrics.GetOrRegisterGauge("blobpool/limbo/datareal", nil)
	limboSlotusedGauge = metrics.GetOrRegisterGauge("blobpool/limbo/slotused", nil)

	// The below metrics track the per-shelf metrics for the primary blob store
	// and the temporary limbo store.
	shelfDatausedGaugeName = "blobpool/shelf_%d/dataused"
	shelfDatagapsGaugeName = "blobpool/shelf_%d/datagaps"
	shelfSlotusedGaugeName = "blobpool/shelf_%d/slotused"
	shelfSlotgapsGaugeName = "blobpool/shelf_%d/slotgaps"

	limboShelfDatausedGaugeName = "blobpool/limbo/shelf_%d/dataused"
	limboShelfDatagapsGaugeName = "blobpool/limbo/shelf_%d/datagaps"
	limboShelfSlotusedGaugeName = "blobpool/limbo/shelf_%d/slotused"
	limboShelfSlotgapsGaugeName = "blobpool/limbo/shelf_%d/slotgaps"

	// The oversized metrics aggregate the shelf stats above the max blob count
	// limits to track transactions that are just huge, but don't contain blobs.
	//
	// There are no oversized data in the limbo, it only contains blobs and some
	// constant metadata.
	oversizedDatausedGauge = metrics.GetOrRegisterGauge("blobpool/oversized/dataused", nil)
	oversizedDatagapsGauge = metrics.GetOrRegisterGauge("blobpool/oversized/datagaps", nil)
	oversizedSlotusedGauge = metrics.GetOrRegisterGauge("blobpool/oversized/slotused", nil)
	oversizedSlotgapsGauge = metrics.GetOrRegisterGauge("blobpool/oversized/slotgaps", nil)

	// basefeeGauge and blobfeeGauge track the current network 1559 base fee and
	// 4844 blob fee respectively.
	basefeeGauge = metrics.GetOrRegisterGauge("blobpool/basefee", nil)
	blobfeeGauge = metrics.GetOrRegisterGauge("blobpool/blobfee", nil)

	// pooltipGauge is the configurable miner tip to permit a transaction into
	// the pool.
	pooltipGauge = metrics.GetOrRegisterGauge("blobpool/pooltip", nil)

	// addwait/time, resetwait/time and getwait/time track the rough health of
	// the pool and whether it's capable of keeping up with the load from the
	// network.
	addwaitHist   = metrics.GetOrRegisterHistogram("blobpool/addwait", nil, metrics.NewExpDecaySample(1028, 0.015))
	addtimeHist   = metrics.GetOrRegisterHistogram("blobpool/addtime", nil, metrics.NewExpDecaySample(1028, 0.015))
	getwaitHist   = metrics.GetOrRegisterHistogram("blobpool/getwait", nil, metrics.NewExpDecaySample(1028, 0.015))
	gettimeHist   = metrics.GetOrRegisterHistogram("blobpool/gettime", nil, metrics.NewExpDecaySample(1028, 0.015))
	pendwaitHist  = metrics.GetOrRegisterHistogram("blobpool/pendwait", nil, metrics.NewExpDecaySample(1028, 0.015))
	pendtimeHist  = metrics.GetOrRegisterHistogram("blobpool/pendtime", nil, metrics.NewExpDecaySample(1028, 0.015))
	resetwaitHist = metrics.GetOrRegisterHistogram("blobpool/resetwait", nil, metrics.NewExpDecaySample(1028, 0.015))
	resettimeHist = metrics.GetOrRegisterHistogram("blobpool/resettime", nil, metrics.NewExpDecaySample(1028, 0.015))

	// The below metrics track various cases where transactions are dropped out
	// of the pool. Most are exceptional, some are chain progression and some
	// threshold cappings.
	dropInvalidMeter     = metrics.GetOrRegisterMeter("blobpool/drop/invalid", nil)     // Invalid transaction, consensus change or bugfix, neutral-ish
	dropDanglingMeter    = metrics.GetOrRegisterMeter("blobpool/drop/dangling", nil)    // First nonce gapped, bad
	dropFilledMeter      = metrics.GetOrRegisterMeter("blobpool/drop/filled", nil)      // State full-overlap, chain progress, ok
	dropOverlappedMeter  = metrics.GetOrRegisterMeter("blobpool/drop/overlapped", nil)  // State partial-overlap, chain progress, ok
	dropRepeatedMeter    = metrics.GetOrRegisterMeter("blobpool/drop/repeated", nil)    // Repeated nonce, bad
	dropGappedMeter      = metrics.GetOrRegisterMeter("blobpool/drop/gapped", nil)      // Non-first nonce gapped, bad
	dropOverdraftedMeter = metrics.GetOrRegisterMeter("blobpool/drop/overdrafted", nil) // Balance exceeded, bad
	dropOvercappedMeter  = metrics.GetOrRegisterMeter("blobpool/drop/overcapped", nil)  // Per-account cap exceeded, bad
	dropOverflownMeter   = metrics.GetOrRegisterMeter("blobpool/drop/overflown", nil)   // Global disk cap exceeded, neutral-ish
	dropUnderpricedMeter = metrics.GetOrRegisterMeter("blobpool/drop/underpriced", nil) // Gas tip changed, neutral
	dropReplacedMeter    = metrics.GetOrRegisterMeter("blobpool/drop/replaced", nil)    // Transaction replaced, neutral

	// The below metrics track various outcomes of transactions being added to
	// the pool.
	addInvalidMeter      = metrics.GetOrRegisterMeter("blobpool/add/invalid", nil)      // Invalid transaction, reject, neutral
	addUnderpricedMeter  = metrics.GetOrRegisterMeter("blobpool/add/underpriced", nil)  // Gas tip too low, neutral
	addStaleMeter        = metrics.GetOrRegisterMeter("blobpool/add/stale", nil)        // Nonce already filled, reject, bad-ish
	addGappedMeter       = metrics.GetOrRegisterMeter("blobpool/add/gapped", nil)       // Nonce gapped, reject, bad-ish
	addOverdraftedMeter  = metrics.GetOrRegisterMeter("blobpool/add/overdrafted", nil)  // Balance exceeded, reject, neutral
	addOvercappedMeter   = metrics.GetOrRegisterMeter("blobpool/add/overcapped", nil)   // Per-account cap exceeded, reject, neutral
	addNoreplaceMeter    = metrics.GetOrRegisterMeter("blobpool/add/noreplace", nil)    // Replacement fees or tips too low, neutral
	addNonExclusiveMeter = metrics.GetOrRegisterMeter("blobpool/add/nonexclusive", nil) // Plain transaction from same account exists, reject, neutral
	addValidMeter        = metrics.GetOrRegisterMeter("blobpool/add/valid", nil)        // Valid transaction, add, neutral
)
