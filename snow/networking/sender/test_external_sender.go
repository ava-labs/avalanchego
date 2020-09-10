// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanche-go/ids"
)

// ExternalSenderTest is a test sender
type ExternalSenderTest struct {
	T *testing.T
	B *testing.B

	CantGetAcceptedFrontier, CantAcceptedFrontier,
	CantGetAccepted, CantAccepted,
	CantGetAncestors, CantMultiPut,
	CantGet, CantPut,
	CantPullQuery, CantPushQuery, CantChits,
	CantGossip bool

	GetAcceptedFrontierF func(validatorIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Time)
	AcceptedFrontierF    func(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs ids.Set)

	GetAcceptedF func(validatorIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Time, containerIDs ids.Set)
	AcceptedF    func(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs ids.Set)

	GetAncestorsF func(validatorID ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Time, containerID ids.ID)
	MultiPutF     func(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containers [][]byte)

	GetF func(validatorID ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Time, containerID ids.ID)
	PutF func(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerID ids.ID, container []byte)

	PushQueryF func(validatorIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Time, containerID ids.ID, container []byte)
	PullQueryF func(validatorIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Time, containerID ids.ID)
	ChitsF     func(validatorID ids.ShortID, chainID ids.ID, requestID uint32, votes ids.Set)

	GossipF func(chainID ids.ID, containerID ids.ID, container []byte)
}

// Default set the default callable value to [cant]
func (s *ExternalSenderTest) Default(cant bool) {
	s.CantGetAcceptedFrontier = cant
	s.CantAcceptedFrontier = cant

	s.CantGetAccepted = cant
	s.CantAccepted = cant

	s.CantGetAncestors = cant
	s.CantMultiPut = cant

	s.CantGet = cant
	s.CantPut = cant

	s.CantPullQuery = cant
	s.CantPushQuery = cant
	s.CantChits = cant

	s.CantGossip = cant
}

// GetAcceptedFrontier calls GetAcceptedFrontierF if it was initialized. If it
// wasn't initialized and this function shouldn't be called and testing was
// initialized, then testing will fail.
func (s *ExternalSenderTest) GetAcceptedFrontier(validatorIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Time) {
	if s.GetAcceptedFrontierF != nil {
		s.GetAcceptedFrontierF(validatorIDs, chainID, requestID, deadline)
	} else if s.CantGetAcceptedFrontier && s.T != nil {
		s.T.Fatalf("Unexpectedly called GetAcceptedFrontier")
	} else if s.CantGetAcceptedFrontier && s.B != nil {
		s.B.Fatalf("Unexpectedly called GetAcceptedFrontier")
	}
}

// AcceptedFrontier calls AcceptedFrontierF if it was initialized. If it wasn't
// initialized and this function shouldn't be called and testing was
// initialized, then testing will fail.
func (s *ExternalSenderTest) AcceptedFrontier(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs ids.Set) {
	if s.AcceptedFrontierF != nil {
		s.AcceptedFrontierF(validatorID, chainID, requestID, containerIDs)
	} else if s.CantAcceptedFrontier && s.T != nil {
		s.T.Fatalf("Unexpectedly called AcceptedFrontier")
	} else if s.CantAcceptedFrontier && s.B != nil {
		s.B.Fatalf("Unexpectedly called AcceptedFrontier")
	}
}

// GetAccepted calls GetAcceptedF if it was initialized. If it wasn't
// initialized and this function shouldn't be called and testing was
// initialized, then testing will fail.
func (s *ExternalSenderTest) GetAccepted(validatorIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Time, containerIDs ids.Set) {
	if s.GetAcceptedF != nil {
		s.GetAcceptedF(validatorIDs, chainID, requestID, deadline, containerIDs)
	} else if s.CantGetAccepted && s.T != nil {
		s.T.Fatalf("Unexpectedly called GetAccepted")
	} else if s.CantGetAccepted && s.B != nil {
		s.B.Fatalf("Unexpectedly called GetAccepted")
	}
}

// Accepted calls AcceptedF if it was initialized. If it wasn't initialized and
// this function shouldn't be called and testing was initialized, then testing
// will fail.
func (s *ExternalSenderTest) Accepted(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs ids.Set) {
	if s.AcceptedF != nil {
		s.AcceptedF(validatorID, chainID, requestID, containerIDs)
	} else if s.CantAccepted && s.T != nil {
		s.T.Fatalf("Unexpectedly called Accepted")
	} else if s.CantAccepted && s.B != nil {
		s.B.Fatalf("Unexpectedly called Accepted")
	}
}

// GetAncestors calls GetAncestorsF if it was initialized. If it wasn't initialized and this
// function shouldn't be called and testing was initialized, then testing will
// fail.
func (s *ExternalSenderTest) GetAncestors(vdr ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Time, vtxID ids.ID) {
	if s.GetAncestorsF != nil {
		s.GetAncestorsF(vdr, chainID, requestID, deadline, vtxID)
	} else if s.CantGetAncestors && s.T != nil {
		s.T.Fatalf("Unexpectedly called GetAncestors")
	} else if s.CantGetAncestors && s.B != nil {
		s.B.Fatalf("Unexpectedly called GetAncestors")
	}
}

// MultiPut calls MultiPutF if it was initialized. If it wasn't initialized and this
// function shouldn't be called and testing was initialized, then testing will
// fail.
func (s *ExternalSenderTest) MultiPut(vdr ids.ShortID, chainID ids.ID, requestID uint32, vtxs [][]byte) {
	if s.MultiPutF != nil {
		s.MultiPutF(vdr, chainID, requestID, vtxs)
	} else if s.CantMultiPut && s.T != nil {
		s.T.Fatalf("Unexpectedly called MultiPut")
	} else if s.CantMultiPut && s.B != nil {
		s.B.Fatalf("Unexpectedly called MultiPut")
	}
}

// Get calls GetF if it was initialized. If it wasn't initialized and this
// function shouldn't be called and testing was initialized, then testing will
// fail.
func (s *ExternalSenderTest) Get(vdr ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Time, vtxID ids.ID) {
	if s.GetF != nil {
		s.GetF(vdr, chainID, requestID, deadline, vtxID)
	} else if s.CantGet && s.T != nil {
		s.T.Fatalf("Unexpectedly called Get")
	} else if s.CantGet && s.B != nil {
		s.B.Fatalf("Unexpectedly called Get")
	}
}

// Put calls PutF if it was initialized. If it wasn't initialized and this
// function shouldn't be called and testing was initialized, then testing will
// fail.
func (s *ExternalSenderTest) Put(vdr ids.ShortID, chainID ids.ID, requestID uint32, vtxID ids.ID, vtx []byte) {
	if s.PutF != nil {
		s.PutF(vdr, chainID, requestID, vtxID, vtx)
	} else if s.CantPut && s.T != nil {
		s.T.Fatalf("Unexpectedly called Put")
	} else if s.CantPut && s.B != nil {
		s.B.Fatalf("Unexpectedly called Put")
	}
}

// PushQuery calls PushQueryF if it was initialized. If it wasn't initialized
// and this function shouldn't be called and testing was initialized, then
// testing will fail.
func (s *ExternalSenderTest) PushQuery(vdrs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Time, vtxID ids.ID, vtx []byte) {
	if s.PushQueryF != nil {
		s.PushQueryF(vdrs, chainID, requestID, deadline, vtxID, vtx)
	} else if s.CantPushQuery && s.T != nil {
		s.T.Fatalf("Unexpectedly called PushQuery")
	} else if s.CantPushQuery && s.B != nil {
		s.B.Fatalf("Unexpectedly called PushQuery")
	}
}

// PullQuery calls PullQueryF if it was initialized. If it wasn't initialized
// and this function shouldn't be called and testing was initialized, then
// testing will fail.
func (s *ExternalSenderTest) PullQuery(vdrs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Time, vtxID ids.ID) {
	if s.PullQueryF != nil {
		s.PullQueryF(vdrs, chainID, requestID, deadline, vtxID)
	} else if s.CantPullQuery && s.T != nil {
		s.T.Fatalf("Unexpectedly called PullQuery")
	} else if s.CantPullQuery && s.B != nil {
		s.B.Fatalf("Unexpectedly called PullQuery")
	}
}

// Chits calls ChitsF if it was initialized. If it wasn't initialized and this
// function shouldn't be called and testing was initialized, then testing will
// fail.
func (s *ExternalSenderTest) Chits(vdr ids.ShortID, chainID ids.ID, requestID uint32, votes ids.Set) {
	if s.ChitsF != nil {
		s.ChitsF(vdr, chainID, requestID, votes)
	} else if s.CantChits && s.T != nil {
		s.T.Fatalf("Unexpectedly called Chits")
	} else if s.CantChits && s.B != nil {
		s.B.Fatalf("Unexpectedly called Chits")
	}
}

// Gossip calls GossipF if it was initialized. If it wasn't initialized and this
// function shouldn't be called and testing was initialized, then testing will
// fail.
func (s *ExternalSenderTest) Gossip(chainID ids.ID, containerID ids.ID, container []byte) {
	if s.GossipF != nil {
		s.GossipF(chainID, containerID, container)
	} else if s.CantGossip && s.T != nil {
		s.T.Fatalf("Unexpectedly called Gossip")
	} else if s.CantGossip && s.B != nil {
		s.B.Fatalf("Unexpectedly called Gossip")
	}
}
