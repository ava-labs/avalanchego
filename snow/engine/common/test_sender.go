// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

// SenderTest is a test sender
type SenderTest struct {
	T *testing.T

	CantGetAcceptedFrontier, CantAcceptedFrontier,
	CantGetAccepted, CantAccepted,
	CantGet, CantGetAncestors, CantPut, CantMultiPut,
	CantPullQuery, CantPushQuery, CantChits,
	CantGossip bool

	GetAcceptedFrontierF func(ids.ShortSet, uint32)
	AcceptedFrontierF    func(ids.ShortID, uint32, ids.Set)
	GetAcceptedF         func(ids.ShortSet, uint32, ids.Set)
	AcceptedF            func(ids.ShortID, uint32, ids.Set)
	GetF                 func(ids.ShortID, uint32, ids.ID)
	GetAncestorsF        func(ids.ShortID, uint32, ids.ID)
	PutF                 func(ids.ShortID, uint32, ids.ID, []byte)
	MultiPutF            func(ids.ShortID, uint32, [][]byte)
	PushQueryF           func(ids.ShortSet, uint32, ids.ID, []byte)
	PullQueryF           func(ids.ShortSet, uint32, ids.ID)
	ChitsF               func(ids.ShortID, uint32, ids.Set)
	GossipF              func(ids.ID, []byte)
}

// Default set the default callable value to [cant]
func (s *SenderTest) Default(cant bool) {
	s.CantGetAcceptedFrontier = cant
	s.CantAcceptedFrontier = cant
	s.CantGetAccepted = cant
	s.CantAccepted = cant
	s.CantGet = cant
	s.CantGetAccepted = cant
	s.CantPut = cant
	s.CantMultiPut = cant
	s.CantPullQuery = cant
	s.CantPushQuery = cant
	s.CantChits = cant
	s.CantGossip = cant
}

// GetAcceptedFrontier calls GetAcceptedFrontierF if it was initialized. If it
// wasn't initialized and this function shouldn't be called and testing was
// initialized, then testing will fail.
func (s *SenderTest) GetAcceptedFrontier(validatorIDs ids.ShortSet, requestID uint32) {
	if s.GetAcceptedFrontierF != nil {
		s.GetAcceptedFrontierF(validatorIDs, requestID)
	} else if s.CantGetAcceptedFrontier && s.T != nil {
		s.T.Fatalf("Unexpectedly called GetAcceptedFrontier")
	}
}

// AcceptedFrontier calls AcceptedFrontierF if it was initialized. If it wasn't
// initialized and this function shouldn't be called and testing was
// initialized, then testing will fail.
func (s *SenderTest) AcceptedFrontier(validatorID ids.ShortID, requestID uint32, containerIDs ids.Set) {
	if s.AcceptedFrontierF != nil {
		s.AcceptedFrontierF(validatorID, requestID, containerIDs)
	} else if s.CantAcceptedFrontier && s.T != nil {
		s.T.Fatalf("Unexpectedly called AcceptedFrontier")
	}
}

// GetAccepted calls GetAcceptedF if it was initialized. If it wasn't
// initialized and this function shouldn't be called and testing was
// initialized, then testing will fail.
func (s *SenderTest) GetAccepted(validatorIDs ids.ShortSet, requestID uint32, containerIDs ids.Set) {
	if s.GetAcceptedF != nil {
		s.GetAcceptedF(validatorIDs, requestID, containerIDs)
	} else if s.CantGetAccepted && s.T != nil {
		s.T.Fatalf("Unexpectedly called GetAccepted")
	}
}

// Accepted calls AcceptedF if it was initialized. If it wasn't initialized and
// this function shouldn't be called and testing was initialized, then testing
// will fail.
func (s *SenderTest) Accepted(validatorID ids.ShortID, requestID uint32, containerIDs ids.Set) {
	if s.AcceptedF != nil {
		s.AcceptedF(validatorID, requestID, containerIDs)
	} else if s.CantAccepted && s.T != nil {
		s.T.Fatalf("Unexpectedly called Accepted")
	}
}

// Get calls GetF if it was initialized. If it wasn't initialized and this
// function shouldn't be called and testing was initialized, then testing will
// fail.
func (s *SenderTest) Get(vdr ids.ShortID, requestID uint32, vtxID ids.ID) {
	if s.GetF != nil {
		s.GetF(vdr, requestID, vtxID)
	} else if s.CantGet && s.T != nil {
		s.T.Fatalf("Unexpectedly called Get")
	}
}

// GetAncestors calls GetAncestorsF if it was initialized. If it
// wasn't initialized and this function shouldn't be called and testing was
// initialized, then testing will fail.
func (s *SenderTest) GetAncestors(validatorID ids.ShortID, requestID uint32, vtxID ids.ID) {
	if s.GetAncestorsF != nil {
		s.GetAncestorsF(validatorID, requestID, vtxID)
	} else if s.CantGetAncestors && s.T != nil {
		s.T.Fatalf("Unexpectedly called CantGetAncestors")
	}
}

// Put calls PutF if it was initialized. If it wasn't initialized and this
// function shouldn't be called and testing was initialized, then testing will
// fail.
func (s *SenderTest) Put(vdr ids.ShortID, requestID uint32, vtxID ids.ID, vtx []byte) {
	if s.PutF != nil {
		s.PutF(vdr, requestID, vtxID, vtx)
	} else if s.CantPut && s.T != nil {
		s.T.Fatalf("Unexpectedly called Put")
	}
}

// MultiPut calls MultiPutF if it was initialized. If it wasn't initialized and this
// function shouldn't be called and testing was initialized, then testing will
// fail.
func (s *SenderTest) MultiPut(vdr ids.ShortID, requestID uint32, vtxs [][]byte) {
	if s.MultiPutF != nil {
		s.MultiPutF(vdr, requestID, vtxs)
	} else if s.CantMultiPut && s.T != nil {
		s.T.Fatalf("Unexpectedly called MultiPut")
	}
}

// PushQuery calls PushQueryF if it was initialized. If it wasn't initialized
// and this function shouldn't be called and testing was initialized, then
// testing will fail.
func (s *SenderTest) PushQuery(vdrs ids.ShortSet, requestID uint32, vtxID ids.ID, vtx []byte) {
	if s.PushQueryF != nil {
		s.PushQueryF(vdrs, requestID, vtxID, vtx)
	} else if s.CantPushQuery && s.T != nil {
		s.T.Fatalf("Unexpectedly called PushQuery")
	}
}

// PullQuery calls PullQueryF if it was initialized. If it wasn't initialized
// and this function shouldn't be called and testing was initialized, then
// testing will fail.
func (s *SenderTest) PullQuery(vdrs ids.ShortSet, requestID uint32, vtxID ids.ID) {
	if s.PullQueryF != nil {
		s.PullQueryF(vdrs, requestID, vtxID)
	} else if s.CantPullQuery && s.T != nil {
		s.T.Fatalf("Unexpectedly called PullQuery")
	}
}

// Chits calls ChitsF if it was initialized. If it wasn't initialized and this
// function shouldn't be called and testing was initialized, then testing will
// fail.
func (s *SenderTest) Chits(vdr ids.ShortID, requestID uint32, votes ids.Set) {
	if s.ChitsF != nil {
		s.ChitsF(vdr, requestID, votes)
	} else if s.CantChits && s.T != nil {
		s.T.Fatalf("Unexpectedly called Chits")
	}
}

// Gossip calls GossipF if it was initialized. If it wasn't initialized and this
// function shouldn't be called and testing was initialized, then testing will
// fail.
func (s *SenderTest) Gossip(containerID ids.ID, container []byte) {
	if s.GossipF != nil {
		s.GossipF(containerID, container)
	} else if s.CantGossip && s.T != nil {
		s.T.Fatalf("Unexpectedly called Gossip")
	}
}
