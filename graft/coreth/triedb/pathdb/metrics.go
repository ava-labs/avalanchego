// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>

package pathdb

import (
	"github.com/ava-labs/libevm/metrics"

	// Force libevm metrics of the same name to be registered first.
	_ "github.com/ava-labs/libevm/triedb/pathdb"
)

// ====== If resolving merge conflicts ======
//
// All calls to metrics.NewRegistered*() for metrics also defined in libevm/triedb/pathdb
// have been replaced with metrics.GetOrRegister*() to get metrics already registered in
// libevm/triedb/pathdb or register them here otherwise. These replacements ensure the same
// metrics are shared between the two packages.
//
//nolint:unused
var (
	cleanHitMeter   = metrics.GetOrRegisterMeter("pathdb/clean/hit", nil)
	cleanMissMeter  = metrics.GetOrRegisterMeter("pathdb/clean/miss", nil)
	cleanReadMeter  = metrics.GetOrRegisterMeter("pathdb/clean/read", nil)
	cleanWriteMeter = metrics.GetOrRegisterMeter("pathdb/clean/write", nil)

	dirtyHitMeter         = metrics.GetOrRegisterMeter("pathdb/dirty/hit", nil)
	dirtyMissMeter        = metrics.GetOrRegisterMeter("pathdb/dirty/miss", nil)
	dirtyReadMeter        = metrics.GetOrRegisterMeter("pathdb/dirty/read", nil)
	dirtyWriteMeter       = metrics.GetOrRegisterMeter("pathdb/dirty/write", nil)
	dirtyNodeHitDepthHist = metrics.GetOrRegisterHistogram("pathdb/dirty/depth", nil, metrics.NewExpDecaySample(1028, 0.015))

	cleanFalseMeter = metrics.GetOrRegisterMeter("pathdb/clean/false", nil)
	dirtyFalseMeter = metrics.GetOrRegisterMeter("pathdb/dirty/false", nil)
	diskFalseMeter  = metrics.GetOrRegisterMeter("pathdb/disk/false", nil)

	commitTimeTimer  = metrics.GetOrRegisterTimer("pathdb/commit/time", nil)
	commitNodesMeter = metrics.GetOrRegisterMeter("pathdb/commit/nodes", nil)
	commitBytesMeter = metrics.GetOrRegisterMeter("pathdb/commit/bytes", nil)

	gcNodesMeter = metrics.GetOrRegisterMeter("pathdb/gc/nodes", nil)
	gcBytesMeter = metrics.GetOrRegisterMeter("pathdb/gc/bytes", nil)

	diffLayerBytesMeter = metrics.GetOrRegisterMeter("pathdb/diff/bytes", nil)
	diffLayerNodesMeter = metrics.GetOrRegisterMeter("pathdb/diff/nodes", nil)

	historyBuildTimeMeter  = metrics.GetOrRegisterTimer("pathdb/history/time", nil)
	historyDataBytesMeter  = metrics.GetOrRegisterMeter("pathdb/history/bytes/data", nil)
	historyIndexBytesMeter = metrics.GetOrRegisterMeter("pathdb/history/bytes/index", nil)
)
