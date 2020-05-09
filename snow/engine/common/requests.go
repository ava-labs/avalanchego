// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"github.com/ava-labs/gecko/ids"
)

type req struct {
	vdr   ids.ShortID
	reqID uint32
}

// Requests ...
type Requests struct {
	reqsToID map[[20]byte]map[uint32]ids.ID
	idToReq  map[[32]byte]req
}

// Add ...
func (r *Requests) Add(vdr ids.ShortID, requestID uint32, containerID ids.ID) {
	if r.reqsToID == nil {
		r.reqsToID = make(map[[20]byte]map[uint32]ids.ID)
	}
	vdrKey := vdr.Key()
	vdrReqs, ok := r.reqsToID[vdrKey]
	if !ok {
		vdrReqs = make(map[uint32]ids.ID)
		r.reqsToID[vdrKey] = vdrReqs
	}
	vdrReqs[requestID] = containerID

	if r.idToReq == nil {
		r.idToReq = make(map[[32]byte]req)
	}
	r.idToReq[containerID.Key()] = req{
		vdr:   vdr,
		reqID: requestID,
	}
}

// Remove ...
func (r *Requests) Remove(vdr ids.ShortID, requestID uint32) (ids.ID, bool) {
	vdrKey := vdr.Key()
	vdrReqs, ok := r.reqsToID[vdrKey]
	if !ok {
		return ids.ID{}, false
	}
	containerID, ok := vdrReqs[requestID]
	if !ok {
		return ids.ID{}, false
	}

	if len(vdrReqs) == 1 {
		delete(r.reqsToID, vdrKey)
	} else {
		delete(vdrReqs, requestID)
	}

	delete(r.idToReq, containerID.Key())
	return containerID, true
}

// RemoveAny ...
func (r *Requests) RemoveAny(containerID ids.ID) bool {
	req, ok := r.idToReq[containerID.Key()]
	if !ok {
		return false
	}

	r.Remove(req.vdr, req.reqID)
	return true
}

// Len ...
func (r *Requests) Len() int { return len(r.idToReq) }

// Contains ...
func (r *Requests) Contains(containerID ids.ID) bool {
	_, ok := r.idToReq[containerID.Key()]
	return ok
}
