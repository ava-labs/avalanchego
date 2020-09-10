// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"github.com/ava-labs/avalanche-go/ids"
)

const (
	minRequestsSize = 32
)

type req struct {
	vdr ids.ShortID
	id  uint32
}

// Requests tracks pending container messages from a peer.
type Requests struct {
	reqsToID map[[20]byte]map[uint32]ids.ID
	idToReq  map[[32]byte]req
}

// Add a request. Assumes that requestIDs are unique. Assumes that containerIDs
// are only in one request at a time.
func (r *Requests) Add(vdr ids.ShortID, requestID uint32, containerID ids.ID) {
	if r.reqsToID == nil {
		r.reqsToID = make(map[[20]byte]map[uint32]ids.ID, minRequestsSize)
	}
	vdrKey := vdr.Key()
	vdrReqs, ok := r.reqsToID[vdrKey]
	if !ok {
		vdrReqs = make(map[uint32]ids.ID)
		r.reqsToID[vdrKey] = vdrReqs
	}
	vdrReqs[requestID] = containerID

	if r.idToReq == nil {
		r.idToReq = make(map[[32]byte]req, minRequestsSize)
	}
	r.idToReq[containerID.Key()] = req{
		vdr: vdr,
		id:  requestID,
	}
}

// Remove attempts to abandon a requestID sent to a validator. If the request is
// currently outstanding, the requested ID will be returned along with true. If
// the request isn't currently outstanding, false will be returned.
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

// RemoveAny outstanding requests for the container ID. True is returned if the
// container ID had an outstanding request.
func (r *Requests) RemoveAny(containerID ids.ID) bool {
	req, ok := r.idToReq[containerID.Key()]
	if !ok {
		return false
	}

	r.Remove(req.vdr, req.id)
	return true
}

// Len returns the total number of outstanding requests.
func (r *Requests) Len() int { return len(r.idToReq) }

// Contains returns true if there is an outstanding request for the container
// ID.
func (r *Requests) Contains(containerID ids.ID) bool {
	_, ok := r.idToReq[containerID.Key()]
	return ok
}
