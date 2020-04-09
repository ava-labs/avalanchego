// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package networking

import (
	stdmath "math"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/math"
)

// AwaitingConnections ...
type AwaitingConnections struct {
	Requested      validators.Set
	WeightRequired uint64
	Finish         func()

	weight uint64
}

// Add ...
func (aw *AwaitingConnections) Add(conn ids.ShortID) {
	vdr, ok := aw.Requested.Get(conn)
	if !ok {
		return
	}
	weight, err := math.Add64(vdr.Weight(), aw.weight)
	if err != nil {
		weight = stdmath.MaxUint64
	}
	aw.weight = weight
}

// Remove ...
func (aw *AwaitingConnections) Remove(conn ids.ShortID) {
	vdr, ok := aw.Requested.Get(conn)
	if !ok {
		return
	}
	aw.weight -= vdr.Weight()
}

// Ready ...
func (aw *AwaitingConnections) Ready() bool { return aw.weight >= aw.WeightRequired }
