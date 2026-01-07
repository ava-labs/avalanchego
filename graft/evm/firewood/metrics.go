// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/log"
)

func init() {
	if err := ffi.StartMetrics(); err != nil {
		log.Crit("starting firewood metrics", "error", err)
	}
}
