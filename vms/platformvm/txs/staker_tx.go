// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"time"
)

type StakerTx interface {
	UnsignedTx

	StartTime() time.Time
	EndTime() time.Time
	Weight() uint64
}
