// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"math"
)

const (
	// IDTypeID is the type ID for ids.ID
	IDTypeID uint64 = math.MaxUint64 - iota
	// StatusTypeID is the type ID for choices.Status
	StatusTypeID
	// TimeTypeID is the type ID for time
	TimeTypeID
	// BlockTypeID is the type ID of blocks in state
	BlockTypeID
)
