// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package queue

import (
	"github.com/ava-labs/avalanchego/ids"
)

// Job defines the interface required to be placed on the job queue.
type Job interface {
	ID() ids.ID
	MissingDependencies() (ids.Set, error)
	// Returns true if this job has at least 1 missing dependency
	HasMissingDependencies() (bool, error)
	Execute() error
	Bytes() []byte
}
