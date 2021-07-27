// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

// ExternalSenderTest is a test sender
type ExternalSenderTest struct {
	T *testing.T
	B *testing.B

	CantSendGetAcceptedFrontier, CantSendAcceptedFrontier,
	CantSendGetAccepted, CantSendAccepted,
	CantSendGetAncestors, CantSendMultiPut,
	CantSendGet, CantSendPut,
	CantSendPullQuery, CantSendPushQuery, CantSendChits,
	CantSendGossip,
	CantSendAppRequest, CantSendAppResponse, CantSendAppGossip bool

	SendGetAcceptedFrontierF func(nodeIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Duration) []ids.ShortID
	SendAcceptedFrontierF    func(nodeID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs []ids.ID)

	SendGetAcceptedF func(nodeIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Duration, containerIDs []ids.ID) []ids.ShortID
	SendAcceptedF    func(nodeID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs []ids.ID)

	SendGetAncestorsF func(nodeID ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Duration, containerID ids.ID) bool
	SendMultiPutF     func(nodeID ids.ShortID, chainID ids.ID, requestID uint32, containers [][]byte)

	SendGetF func(nodeID ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Duration, containerID ids.ID) bool
	SendPutF func(nodeID ids.ShortID, chainID ids.ID, requestID uint32, containerID ids.ID, container []byte)

	SendPushQueryF func(nodeIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Duration, containerID ids.ID, container []byte) []ids.ShortID
	SendPullQueryF func(nodeIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Duration, containerID ids.ID) []ids.ShortID
	SendChitsF     func(nodeID ids.ShortID, chainID ids.ID, requestID uint32, votes []ids.ID)

	SendAppRequestF  func(nodeIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Duration, appRequestBytes []byte) []ids.ShortID
	SendAppResponseF func(nodeIDs ids.ShortID, chainID ids.ID, requestID uint32, appResponseBytyes []byte)
	SendAppGossipF   func(chainID ids.ID, appGossipBytyes []byte)

	SendGossipF func(chainID ids.ID, containerID ids.ID, container []byte)
}

// Default set the default callable value to [cant]
func (s *ExternalSenderTest) Default(cant bool) {
	s.CantSendGetAcceptedFrontier = cant
	s.CantSendAcceptedFrontier = cant

	s.CantSendGetAccepted = cant
	s.CantSendAccepted = cant

	s.CantSendGetAncestors = cant
	s.CantSendMultiPut = cant

	s.CantSendGet = cant
	s.CantSendPut = cant

	s.CantSendPullQuery = cant
	s.CantSendPushQuery = cant
	s.CantSendChits = cant
	s.CantSendAppRequest = cant
	s.CantSendAppResponse = cant
	s.CantSendAppGossip = cant
	s.CantSendGossip = cant
}

// SendGetAcceptedFrontier calls SendGetAcceptedFrontierF if it was initialized. If it
// wasn't initialized and this function shouldn't be called and testing was
// initialized, then testing will fail.
func (s *ExternalSenderTest) SendGetAcceptedFrontier(nodeIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Duration) []ids.ShortID {
	switch {
	case s.SendGetAcceptedFrontierF != nil:
		return s.SendGetAcceptedFrontierF(nodeIDs, chainID, requestID, deadline)
	case s.CantSendGetAcceptedFrontier && s.T != nil:
		s.T.Fatalf("Unexpectedly called GetAcceptedFrontier")
	case s.CantSendGetAcceptedFrontier && s.B != nil:
		s.B.Fatalf("Unexpectedly called GetAcceptedFrontier")
	}
	return nil
}

// SendAcceptedFrontier calls SendAcceptedFrontierF if it was initialized. If it wasn't
// initialized and this function shouldn't be called and testing was
// initialized, then testing will fail.
func (s *ExternalSenderTest) SendAcceptedFrontier(nodeID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs []ids.ID) {
	switch {
	case s.SendAcceptedFrontierF != nil:
		s.SendAcceptedFrontierF(nodeID, chainID, requestID, containerIDs)
	case s.CantSendAcceptedFrontier && s.T != nil:
		s.T.Fatalf("Unexpectedly called SendAcceptedFrontier")
	case s.CantSendAcceptedFrontier && s.B != nil:
		s.B.Fatalf("Unexpectedly called SendAcceptedFrontier")
	}
}

// SendGetAccepted calls SendGetAcceptedF if it was initialized. If it wasn't
// initialized and this function shouldn't be called and testing was
// initialized, then testing will fail.
func (s *ExternalSenderTest) SendGetAccepted(nodeIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Duration, containerIDs []ids.ID) []ids.ShortID {
	switch {
	case s.SendGetAcceptedF != nil:
		return s.SendGetAcceptedF(nodeIDs, chainID, requestID, deadline, containerIDs)
	case s.CantSendGetAccepted && s.T != nil:
		s.T.Fatalf("Unexpectedly called SendGetAccepted")
	case s.CantSendGetAccepted && s.B != nil:
		s.B.Fatalf("Unexpectedly called SendGetAccepted")
	}
	return nil
}

// SendAccepted calls SendAcceptedF if it was initialized. If it wasn't initialized and
// this function shouldn't be called and testing was initialized, then testing
// will fail.
func (s *ExternalSenderTest) SendAccepted(nodeID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs []ids.ID) {
	switch {
	case s.SendAcceptedF != nil:
		s.SendAcceptedF(nodeID, chainID, requestID, containerIDs)
	case s.CantSendAccepted && s.T != nil:
		s.T.Fatalf("Unexpectedly called Accepted")
	case s.CantSendAccepted && s.B != nil:
		s.B.Fatalf("Unexpectedly called Accepted")
	}
}

// SendGetAncestors calls SendGetAncestorsF if it was initialized. If it wasn't initialized and this
// function shouldn't be called and testing was initialized, then testing will
// fail.
func (s *ExternalSenderTest) SendGetAncestors(vdr ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Duration, vtxID ids.ID) bool {
	switch {
	case s.SendGetAncestorsF != nil:
		return s.SendGetAncestorsF(vdr, chainID, requestID, deadline, vtxID)
	case s.CantSendGetAncestors && s.T != nil:
		s.T.Fatalf("Unexpectedly called SendGetAncestors")
	case s.CantSendGetAncestors && s.B != nil:
		s.B.Fatalf("Unexpectedly called SendGetAncestors")
	}
	return false
}

// SendMultiPut calls SendMultiPutF if it was initialized. If it wasn't initialized and this
// function shouldn't be called and testing was initialized, then testing will
// fail.
func (s *ExternalSenderTest) SendMultiPut(vdr ids.ShortID, chainID ids.ID, requestID uint32, vtxs [][]byte) {
	switch {
	case s.SendMultiPutF != nil:
		s.SendMultiPutF(vdr, chainID, requestID, vtxs)
	case s.CantSendMultiPut && s.T != nil:
		s.T.Fatalf("Unexpectedly called SendMultiPut")
	case s.CantSendMultiPut && s.B != nil:
		s.B.Fatalf("Unexpectedly called SendMultiPut")
	}
}

// SendGet calls SendGetF if it was initialized. If it wasn't initialized and this
// function shouldn't be called and testing was initialized, then testing will
// fail.
func (s *ExternalSenderTest) SendGet(vdr ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Duration, vtxID ids.ID) bool {
	switch {
	case s.SendGetF != nil:
		return s.SendGetF(vdr, chainID, requestID, deadline, vtxID)
	case s.CantSendGet && s.T != nil:
		s.T.Fatalf("Unexpectedly called SendGet")
	case s.CantSendGet && s.B != nil:
		s.B.Fatalf("Unexpectedly called SendGet")
	}
	return false
}

// SendPut calls SendPutF if it was initialized. If it wasn't initialized and this
// function shouldn't be called and testing was initialized, then testing will
// fail.
func (s *ExternalSenderTest) SendPut(vdr ids.ShortID, chainID ids.ID, requestID uint32, vtxID ids.ID, vtx []byte) {
	switch {
	case s.SendPutF != nil:
		s.SendPutF(vdr, chainID, requestID, vtxID, vtx)
	case s.CantSendPut && s.T != nil:
		s.T.Fatalf("Unexpectedly called SendPut")
	case s.CantSendPut && s.B != nil:
		s.B.Fatalf("Unexpectedly called SendPut")
	}
}

// SendPushQuery calls SendPushQueryF if it was initialized. If it wasn't initialized
// and this function shouldn't be called and testing was initialized, then
// testing will fail.
func (s *ExternalSenderTest) SendPushQuery(vdrs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Duration, vtxID ids.ID, vtx []byte) []ids.ShortID {
	switch {
	case s.SendPushQueryF != nil:
		return s.SendPushQueryF(vdrs, chainID, requestID, deadline, vtxID, vtx)
	case s.CantSendPushQuery && s.T != nil:
		s.T.Fatalf("Unexpectedly called SendPushQuery")
	case s.CantSendPushQuery && s.B != nil:
		s.B.Fatalf("Unexpectedly called SendPushQuery")
	}
	return nil
}

// SendPullQuery calls SendPullQueryF if it was initialized. If it wasn't initialized
// and this function shouldn't be called and testing was initialized, then
// testing will fail.
func (s *ExternalSenderTest) SendPullQuery(vdrs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Duration, vtxID ids.ID) []ids.ShortID {
	switch {
	case s.SendPullQueryF != nil:
		return s.SendPullQueryF(vdrs, chainID, requestID, deadline, vtxID)
	case s.CantSendPullQuery && s.T != nil:
		s.T.Fatalf("Unexpectedly called SendPullQuery")
	case s.CantSendPullQuery && s.B != nil:
		s.B.Fatalf("Unexpectedly called SendPullQuery")
	}
	return nil
}

// SendChits calls SendChitsF if it was initialized. If it wasn't initialized and this
// function shouldn't be called and testing was initialized, then testing will
// fail.
func (s *ExternalSenderTest) SendChits(vdr ids.ShortID, chainID ids.ID, requestID uint32, votes []ids.ID) {
	switch {
	case s.SendChitsF != nil:
		s.SendChitsF(vdr, chainID, requestID, votes)
	case s.CantSendChits && s.T != nil:
		s.T.Fatalf("Unexpectedly called SendChits")
	case s.CantSendChits && s.B != nil:
		s.B.Fatalf("Unexpectedly called SendChits")
	}
}

// SendAppRequest calls SendAppRequestF if it was initialized. If it wasn't initialized and this
// function shouldn't be called and testing was initialized, then testing will
// fail.
func (s *ExternalSenderTest) SendAppRequest(nodeIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Duration, appRequestBytes []byte) []ids.ShortID {
	switch {
	case s.SendAppRequestF != nil:
		return s.SendAppRequestF(nodeIDs, chainID, requestID, deadline, appRequestBytes)
	case s.CantSendAppRequest && s.T != nil:
		s.T.Fatalf("Unexpectedly called SendAppRequest")
	case s.CantSendAppRequest && s.B != nil:
		s.B.Fatalf("Unexpectedly called SendAppRequest")
	}
	return nil
}

// SendAppResponse calls SendAppResponseF if it was initialized. If it wasn't initialized and this
// function shouldn't be called and testing was initialized, then testing will
// fail.
func (s *ExternalSenderTest) SendAppResponse(nodeID ids.ShortID, chainID ids.ID, requestID uint32, appResponseBytes []byte) {
	switch {
	case s.SendAppResponseF != nil:
		s.SendAppResponseF(nodeID, chainID, requestID, appResponseBytes)
	case s.CantSendAppResponse && s.T != nil:
		s.T.Fatalf("Unexpectedly called SendAppResponse")
	case s.CantSendAppResponse && s.B != nil:
		s.B.Fatalf("Unexpectedly called SendAppResponse")
	}
}

// SendAppGossip calls SendAppGossipF if it was initialized. If it wasn't initialized and this
// function shouldn't be called and testing was initialized, then testing will
// fail.
func (s *ExternalSenderTest) SendAppGossip(chainID ids.ID, appGossipBytes []byte) {
	switch {
	case s.SendAppGossipF != nil:
		s.SendAppGossipF(chainID, appGossipBytes)
	case s.CantSendAppGossip && s.T != nil:
		s.T.Fatalf("Unexpectedly called SendAppGossip")
	case s.CantSendAppGossip && s.B != nil:
		s.B.Fatalf("Unexpectedly called SendAppGossip")
	}
}

// SendGossip calls SendGossipF if it was initialized. If it wasn't initialized and this
// function shouldn't be called and testing was initialized, then testing will
// fail.
func (s *ExternalSenderTest) SendGossip(chainID ids.ID, containerID ids.ID, container []byte) {
	switch {
	case s.SendGossipF != nil:
		s.SendGossipF(chainID, containerID, container)
	case s.CantSendGossip && s.T != nil:
		s.T.Fatalf("Unexpectedly called SendGossip")
	case s.CantSendGossip && s.B != nil:
		s.B.Fatalf("Unexpectedly called SendGossip")
	}
}
