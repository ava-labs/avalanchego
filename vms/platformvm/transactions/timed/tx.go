// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timed

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

type Tx interface {
	ID() ids.ID
	StartTime() time.Time
	EndTime() time.Time
	Weight() uint64
	Bytes() []byte
}
