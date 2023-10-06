// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
)

func Less(i, j avalanche.Vertex) bool {
	statusI := i.Status()
	statusJ := j.Status()

	// Put unknown vertices at the front of the heap to ensure once we have made
	// it below a certain height in DAG traversal we do not need to reset
	if !statusI.Fetched() {
		return true
	}
	if !statusJ.Fetched() {
		return false
	}

	// Treat errors on retrieving the height as if the vertex is not fetched
	heightI, errI := i.Height()
	if errI != nil {
		return true
	}
	heightJ, errJ := j.Height()
	if errJ != nil {
		return false
	}
	return heightI > heightJ
}
