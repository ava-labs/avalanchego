// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timed

import (
	"time"
)

type Tx interface {
	StartTime() time.Time
	EndTime() time.Time
	Weight() uint64
}
