// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/message"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

// ExternalSenderTest is a test sender
type ExternalSenderTest struct {
	T *testing.T
	B *testing.B

	c message.Codec
	b message.Builder

	CantSendGetAcceptedFrontier, CantSendAcceptedFrontier,
	CantSendGetAccepted, CantSendAccepted,
	CantSendGetAncestors, CantSendMultiPut,
	CantSendGet, CantSendPut,
	CantSendPullQuery, CantSendPushQuery, CantSendChits,
	CantSendGossip,
	CantSendAppRequest, CantSendAppResponse, CantSendAppGossip bool

	SendGetAcceptedFrontierF func(nodeIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Duration) ids.ShortSet
	SendAcceptedFrontierF    func(nodeID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs []ids.ID)

	SendGetAcceptedF func(nodeIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Duration, containerIDs []ids.ID) ids.ShortSet
	SendAcceptedF    func(nodeID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs []ids.ID)

	SendGetAncestorsF func(nodeID ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Duration, containerID ids.ID) bool
	SendMultiPutF     func(nodeID ids.ShortID, chainID ids.ID, requestID uint32, containers [][]byte)

	SendGetF func(nodeID ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Duration, containerID ids.ID) bool
	SendPutF func(nodeID ids.ShortID, chainID ids.ID, requestID uint32, containerID ids.ID, container []byte)

	SendPushQueryF func(nodeIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Duration, containerID ids.ID, container []byte) ids.ShortSet
	SendPullQueryF func(nodeIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Duration, containerID ids.ID) ids.ShortSet
	SendChitsF     func(nodeID ids.ShortID, chainID ids.ID, requestID uint32, votes []ids.ID)

	SendGossipF func(subnetID, chainID, containerID ids.ID, container []byte) bool

	SendAppRequestF  func(nodeIDs ids.ShortSet, chainID ids.ID, requestID uint32, deadline time.Duration, appRequestBytes []byte) ids.ShortSet
	SendAppResponseF func(nodeIDs ids.ShortID, chainID ids.ID, requestID uint32, appResponseBytyes []byte)
	SendAppGossipF   func(subnetID, chainID ids.ID, appGossipBytyes []byte) bool
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

	assert := assert.New(s.T)
	codec, err := message.NewCodecWithAllocator(
		"test_codec",
		prometheus.NewRegistry(),
		func() []byte { return nil },
		int64(constants.DefaultMaxMessageSize),
	)
	assert.NoError(err)
	s.c = codec
	s.b = message.NewBuilder(codec)
}

// TODO ABENEGIA: fix or remove these mocks
func (s *ExternalSenderTest) Parse(bytes []byte) (message.InboundMessage, error) {
	return s.c.Parse(bytes)
}
func (s *ExternalSenderTest) GetMsgBuilder() message.Builder { return s.b }
func (s *ExternalSenderTest) IsCompressionEnabled() bool     { return false }

// TODO ABENEGIA: fix return type
// TODO ABENEGIA: refactor with template pattern
func (s *ExternalSenderTest) Send(msgType constants.MsgType, outMsg message.OutboundMessage, nodeIDs ids.ShortSet) ids.ShortSet {
	assert := assert.New(s.T)

	// turn  message.OutboundMessage into  message.InboundMessage so be able to retrieve fields
	inMsg, err := s.c.Parse(outMsg.Bytes())
	assert.NoError(err)

	res := ids.NewShortSet(nodeIDs.Len())
	switch msgType {
	case constants.GetAcceptedFrontierMsg:
		// SendGetAcceptedFrontier calls SendGetAcceptedFrontierF if it was initialized. If it
		// wasn't initialized and this function shouldn't be called and testing was
		// initialized, then testing will fail.
		switch {
		case s.SendGetAcceptedFrontierF != nil:
			chainID, err := ids.ToID(inMsg.Get(message.ChainID).([]byte))
			assert.NoError(err)
			reqID, ok := inMsg.Get(message.RequestID).(uint32)
			assert.True(ok)
			deadline := time.Duration(inMsg.Get(message.Deadline).(uint64))
			return s.SendGetAcceptedFrontierF(nodeIDs, chainID, reqID, deadline)
		case s.CantSendGetAcceptedFrontier && s.T != nil:
			s.T.Fatalf("Unexpectedly called GetAcceptedFrontier")
		case s.CantSendGetAcceptedFrontier && s.B != nil:
			s.B.Fatalf("Unexpectedly called GetAcceptedFrontier")
		}

	case constants.AcceptedFrontierMsg:
		// SendAcceptedFrontier calls SendAcceptedFrontierF if it was initialized. If it wasn't
		// initialized and this function shouldn't be called and testing was
		// initialized, then testing will fail.
		switch {
		case s.SendAcceptedFrontierF != nil:
			chainID, err := ids.ToID(inMsg.Get(message.ChainID).([]byte))
			assert.NoError(err)
			reqID, ok := inMsg.Get(message.RequestID).(uint32)
			assert.True(ok)
			containerIDsBytes := inMsg.Get(message.ContainerIDs).([][]byte)
			containerIDs := make([]ids.ID, len(containerIDsBytes))
			for _, containerIDBytes := range containerIDsBytes {
				containerID, err := ids.ToID(containerIDBytes)
				assert.NoError(err)
				containerIDs = append(containerIDs, containerID)
			}

			s.SendAcceptedFrontierF(nodeIDs.List()[0], chainID, reqID, containerIDs)
		case s.CantSendAcceptedFrontier && s.T != nil:
			s.T.Fatalf("Unexpectedly called SendAcceptedFrontier")
		case s.CantSendAcceptedFrontier && s.B != nil:
			s.B.Fatalf("Unexpectedly called SendAcceptedFrontier")
		}

	case constants.GetAcceptedMsg:
		// SendGetAccepted calls SendGetAcceptedF if it was initialized. If it wasn't
		// initialized and this function shouldn't be called and testing was
		// initialized, then testing will fail.
		switch {
		case s.SendGetAcceptedF != nil:
			chainID, err := ids.ToID(inMsg.Get(message.ChainID).([]byte))
			assert.NoError(err)
			reqID, ok := inMsg.Get(message.RequestID).(uint32)
			assert.True(ok)
			deadline := time.Duration(inMsg.Get(message.Deadline).(uint64))

			containerIDsBytes := inMsg.Get(message.ContainerIDs).([][]byte)
			containerIDs := make([]ids.ID, len(containerIDsBytes))
			for _, containerIDBytes := range containerIDsBytes {
				containerID, err := ids.ToID(containerIDBytes)
				assert.NoError(err)
				containerIDs = append(containerIDs, containerID)
			}

			return s.SendGetAcceptedF(nodeIDs, chainID, reqID, deadline, containerIDs)
		case s.CantSendGetAccepted && s.T != nil:
			s.T.Fatalf("Unexpectedly called SendGetAccepted")
		case s.CantSendGetAccepted && s.B != nil:
			s.B.Fatalf("Unexpectedly called SendGetAccepted")
		}

	case constants.AcceptedMsg:
		switch {
		case s.SendAcceptedF != nil:
			chainID, err := ids.ToID(inMsg.Get(message.ChainID).([]byte))
			assert.NoError(err)
			reqID, ok := inMsg.Get(message.RequestID).(uint32)
			assert.True(ok)

			containerIDsBytes := inMsg.Get(message.ContainerIDs).([][]byte)
			containerIDs := make([]ids.ID, len(containerIDsBytes))
			for _, containerIDBytes := range containerIDsBytes {
				containerID, err := ids.ToID(containerIDBytes)
				assert.NoError(err)
				containerIDs = append(containerIDs, containerID)
			}

			s.SendAcceptedF(nodeIDs.List()[0], chainID, reqID, containerIDs)
		case s.CantSendAccepted && s.T != nil:
			s.T.Fatalf("Unexpectedly called Accepted")
		case s.CantSendAccepted && s.B != nil:
			s.B.Fatalf("Unexpectedly called Accepted")
		}

	case constants.GetAncestorsMsg:
		// SendGetAncestors calls SendGetAncestorsF if it was initialized. If it wasn't initialized and this
		// function shouldn't be called and testing was initialized, then testing will
		// fail.
		switch {
		case s.SendGetAncestorsF != nil:
			chainID, err := ids.ToID(inMsg.Get(message.ChainID).([]byte))
			assert.NoError(err)
			reqID, ok := inMsg.Get(message.RequestID).(uint32)
			assert.True(ok)
			deadline := time.Duration(inMsg.Get(message.Deadline).(uint64))
			containerID, err := ids.ToID(inMsg.Get(message.ContainerID).([]byte))
			assert.NoError(err)
			s.SendGetAncestorsF(nodeIDs.List()[0], chainID, reqID, deadline, containerID)
		case s.CantSendGetAncestors && s.T != nil:
			s.T.Fatalf("Unexpectedly called SendGetAncestors")
		case s.CantSendGetAncestors && s.B != nil:
			s.B.Fatalf("Unexpectedly called SendGetAncestors")
		}

	case constants.MultiPutMsg:
		// SendMultiPut calls SendMultiPutF if it was initialized. If it wasn't initialized and this
		// function shouldn't be called and testing was initialized, then testing will
		// fail.
		switch {
		case s.SendMultiPutF != nil:
			chainID, err := ids.ToID(inMsg.Get(message.ChainID).([]byte))
			assert.NoError(err)
			reqID, ok := inMsg.Get(message.RequestID).(uint32)
			assert.True(ok)
			containers := inMsg.Get(message.MultiContainerBytes).([][]byte)
			s.SendMultiPutF(nodeIDs.List()[0], chainID, reqID, containers)
		case s.CantSendMultiPut && s.T != nil:
			s.T.Fatalf("Unexpectedly called SendMultiPut")
		case s.CantSendMultiPut && s.B != nil:
			s.B.Fatalf("Unexpectedly called SendMultiPut")
		}

	case constants.GetMsg:
		// SendGet calls SendGetF if it was initialized. If it wasn't initialized and this
		// function shouldn't be called and testing was initialized, then testing will
		// fail.
		switch {
		case s.SendGetF != nil:
			chainID, err := ids.ToID(inMsg.Get(message.ChainID).([]byte))
			assert.NoError(err)
			reqID, ok := inMsg.Get(message.RequestID).(uint32)
			assert.True(ok)
			deadline := time.Duration(inMsg.Get(message.Deadline).(uint64))
			containerID, err := ids.ToID(inMsg.Get(message.ContainerID).([]byte))
			assert.NoError(err)
			s.SendGetF(nodeIDs.List()[0], chainID, reqID, deadline, containerID)
		case s.CantSendGet && s.T != nil:
			s.T.Fatalf("Unexpectedly called SendGet")
		case s.CantSendGet && s.B != nil:
			s.B.Fatalf("Unexpectedly called SendGet")
		}

	case constants.PutMsg:
		// SendPut calls SendPutF if it was initialized. If it wasn't initialized and this
		// function shouldn't be called and testing was initialized, then testing will
		// fail.
		switch {
		case s.SendPutF != nil:
			chainID, err := ids.ToID(inMsg.Get(message.ChainID).([]byte))
			assert.NoError(err)
			reqID, ok := inMsg.Get(message.RequestID).(uint32)
			assert.True(ok)
			containerID, _ := ids.ToID(inMsg.Get(message.ContainerID).([]byte))
			container := inMsg.Get(message.ContainerBytes).([]byte)
			s.SendPutF(nodeIDs.List()[0], chainID, reqID, containerID, container)
		case s.CantSendPut && s.T != nil:
			s.T.Fatalf("Unexpectedly called SendPut")
		case s.CantSendPut && s.B != nil:
			s.B.Fatalf("Unexpectedly called SendPut")
		}

	case constants.PushQueryMsg:
		switch {
		case s.SendPushQueryF != nil:
			chainID, err := ids.ToID(inMsg.Get(message.ChainID).([]byte))
			assert.NoError(err)
			reqID, ok := inMsg.Get(message.RequestID).(uint32)
			assert.True(ok)
			deadline := time.Duration(inMsg.Get(message.Deadline).(uint64))
			containerID, err := ids.ToID(inMsg.Get(message.ContainerID).([]byte))
			assert.NoError(err)
			container := inMsg.Get(message.ContainerBytes).([]byte)
			return s.SendPushQueryF(nodeIDs, chainID, reqID, deadline, containerID, container)
		case s.CantSendPushQuery && s.T != nil:
			s.T.Fatalf("Unexpectedly called SendPushQuery")
		case s.CantSendPushQuery && s.B != nil:
			s.B.Fatalf("Unexpectedly called SendPushQuery")
		}

	case constants.PullQueryMsg:
		// SendPullQuery calls SendPullQueryF if it was initialized. If it wasn't initialized
		// and this function shouldn't be called and testing was initialized, then
		// testing will fail.
		switch {
		case s.SendPullQueryF != nil:
			chainID, err := ids.ToID(inMsg.Get(message.ChainID).([]byte))
			assert.NoError(err)
			reqID, ok := inMsg.Get(message.RequestID).(uint32)
			assert.True(ok)
			deadline := time.Duration(inMsg.Get(message.Deadline).(uint64))
			containerID, err := ids.ToID(inMsg.Get(message.ContainerID).([]byte))
			assert.NoError(err)
			return s.SendPullQueryF(nodeIDs, chainID, reqID, deadline, containerID)
		case s.CantSendPullQuery && s.T != nil:
			s.T.Fatalf("Unexpectedly called SendPullQuery")
		case s.CantSendPullQuery && s.B != nil:
			s.B.Fatalf("Unexpectedly called SendPullQuery")
		}

	case constants.ChitsMsg:
		// SendChits calls SendChitsF if it was initialized. If it wasn't initialized and this
		// function shouldn't be called and testing was initialized, then testing will
		// fail.
		switch {
		case s.SendChitsF != nil:
			chainID, err := ids.ToID(inMsg.Get(message.ChainID).([]byte))
			assert.NoError(err)
			reqID, ok := inMsg.Get(message.RequestID).(uint32)
			assert.True(ok)

			votesBytes := inMsg.Get(message.ContainerIDs).([][]byte)
			votes := make([]ids.ID, len(votesBytes))
			for _, voteBytes := range votesBytes {
				vote, err := ids.ToID(voteBytes)
				assert.NoError(err)
				votes = append(votes, vote)
			}

			s.SendChitsF(nodeIDs.List()[0], chainID, reqID, votes)
		case s.CantSendChits && s.T != nil:
			s.T.Fatalf("Unexpectedly called SendChits")
		case s.CantSendChits && s.B != nil:
			s.B.Fatalf("Unexpectedly called SendChits")
		}
	case constants.AppRequestMsg:
		// SendAppRequest calls SendAppRequestF if it was initialized. If it wasn't initialized and this
		// function shouldn't be called and testing was initialized, then testing will
		// fail.
		switch {
		case s.SendAppRequestF != nil:
			chainID, err := ids.ToID(inMsg.Get(message.ChainID).([]byte))
			assert.NoError(err)
			reqID, ok := inMsg.Get(message.RequestID).(uint32)
			assert.True(ok)
			deadline := time.Duration(inMsg.Get(message.Deadline).(uint64))
			appBytes := inMsg.Get(message.AppRequestBytes).([]byte)
			return s.SendAppRequestF(nodeIDs, chainID, reqID, deadline, appBytes)
		case s.CantSendAppRequest && s.T != nil:
			s.T.Fatalf("Unexpectedly called SendAppRequest")
		case s.CantSendAppRequest && s.B != nil:
			s.B.Fatalf("Unexpectedly called SendAppRequest")
		}
	case constants.AppResponseMsg:
		// SendAppResponse calls SendAppResponseF if it was initialized. If it wasn't initialized and this
		// function shouldn't be called and testing was initialized, then testing will
		// fail.
		switch {
		case s.SendAppResponseF != nil:
			chainID, err := ids.ToID(inMsg.Get(message.ChainID).([]byte))
			assert.NoError(err)
			reqID, ok := inMsg.Get(message.RequestID).(uint32)
			assert.True(ok)
			appBytes := inMsg.Get(message.AppResponseBytes).([]byte)
			s.SendAppResponseF(nodeIDs.List()[0], chainID, reqID, appBytes)
		case s.CantSendAppResponse && s.T != nil:
			s.T.Fatalf("Unexpectedly called SendAppResponse")
		case s.CantSendAppResponse && s.B != nil:
			s.B.Fatalf("Unexpectedly called SendAppResponse")
		}

	default:
		return res
	}
	return res
}

func (s *ExternalSenderTest) Gossip(msgType constants.MsgType, outMsg message.OutboundMessage, subnetID ids.ID) bool {
	assert := assert.New(s.T)

	// turn  message.OutboundMessage into  message.InboundMessage so be able to retrieve fields
	inMsg, err := s.c.Parse(outMsg.Bytes())
	assert.NoError(err)

	switch msgType {
	case constants.AppGossipMsg:
		// SendAppGossip calls SendAppGossipF if it was initialized. If it wasn't initialized and this
		// function shouldn't be called and testing was initialized, then testing will
		// fail.
		switch {
		case s.SendAppGossipF != nil:
			chainID, err := ids.ToID(inMsg.Get(message.ChainID).([]byte))
			assert.NoError(err)
			appBytes, ok := inMsg.Get(message.AppGossipBytes).([]byte)
			assert.True(ok)
			return s.SendAppGossipF(subnetID, chainID, appBytes)
		case s.CantSendAppGossip && s.T != nil:
			s.T.Fatalf("Unexpectedly called SendAppGossip")
		case s.CantSendAppGossip && s.B != nil:
			s.B.Fatalf("Unexpectedly called SendAppGossip")
		}
	case constants.GossipMsg:
		// SendGossip calls SendGossipF if it was initialized. If it wasn't initialized and this
		// function shouldn't be called and testing was initialized, then testing will
		// fail.
		switch {
		case s.SendGossipF != nil:
			chainID, err := ids.ToID(inMsg.Get(message.ChainID).([]byte))
			assert.NoError(err)
			containerID, err := ids.ToID(inMsg.Get(message.ContainerID).([]byte))
			assert.NoError(err)
			container := inMsg.Get(message.ContainerBytes).([]byte)
			return s.SendGossipF(subnetID, chainID, containerID, container)
		case s.CantSendGossip && s.T != nil:
			s.T.Fatalf("Unexpectedly called SendGossip")
		case s.CantSendGossip && s.B != nil:
			s.B.Fatalf("Unexpectedly called SendGossip")
		}
	default:
		return false
	}
	return false
}
