// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metadata

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

// Mutable interface collects all methods updating
// metadata state upon blocks execution
type Mutable interface {
	GetTimestamp() time.Time
	SetTimestamp(tm time.Time)
	GetCurrentSupply() uint64
	SetCurrentSupply(cs uint64)
}

// Content interface collects all methods to query and mutate
// all of the tracked metadata. Note this Content is a superset
// of Mutable
type Content interface {
	Mutable

	ShouldInit() (bool, error)
	DoneInit() error
	GetLastAccepted() ids.ID
	SetLastAccepted(ids.ID)
	SetHeight(height uint64)
	GetHeight() uint64
}

// Management interface collects all methods used to initialize
// metadata db upon vm initialization, along with methods to
// persist updated state.
type Management interface {
	SyncGenesis(
		genesisBlkID ids.ID,
		genesisTimestamp uint64,
		genesisInitialSupply uint64,
	) error
	LoadMetadata() error

	WriteMetadata() error
	CloseMetadata() error
}

type DataState interface {
	Content
	Management
}
