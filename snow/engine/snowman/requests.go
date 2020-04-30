// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"github.com/ava-labs/gecko/ids"
)

// Requests ...
type Requests struct {
	reqs      map[[20]byte]map[uint32]ids.ID
	requested ids.Set
}

// Add ...
func (r *Requests) Add(vdr ids.ShortID, requestID uint32, containerID ids.ID) {
	if r.reqs == nil {
		r.reqs = make(map[[20]byte]map[uint32]ids.ID)
	}
	vdrKey := vdr.Key()
	vdrReqs, ok := r.reqs[vdrKey]
	if !ok {
		vdrReqs = make(map[uint32]ids.ID)
		r.reqs[vdrKey] = vdrReqs
	}
	vdrReqs[requestID] = containerID
	r.requested.Add(containerID)
}

// Remove ...
func (r *Requests) Remove(vdr ids.ShortID, requestID uint32) (ids.ID, bool) {
	if r.reqs == nil {
		return ids.ID{}, false
	}
	vdrKey := vdr.Key()
	vdrReqs, ok := r.reqs[vdrKey]
	if !ok {
		return ids.ID{}, false
	}
	containerID, ok := vdrReqs[requestID]
	if !ok {
		return ids.ID{}, false
	}

	if len(vdrReqs) == 1 {
		delete(r.reqs, vdrKey)
	} else {
		delete(vdrReqs, requestID)
	}
	r.requested.Remove(containerID)

	return containerID, true
}

// Len ...
func (r *Requests) Len() int { return r.requested.Len() }

// Contains ...
func (r *Requests) Contains(containerID ids.ID) bool { return r.requested.Contains(containerID) }
