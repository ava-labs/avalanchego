// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

// ExternalSenderTest is a test sender
type ExternalSenderTest struct {
	T *testing.T
	B *testing.B

	mc message.Creator

	disabledSend map[message.Op]struct{}
	sendFMap     map[message.Op]func(T *testing.T, inMsg message.InboundMessage, nodeIDs ids.ShortSet, subnetID ids.ID, validatorOnly bool) ids.ShortSet

	disabledGossip map[message.Op]struct{}
	gossipFMap     map[message.Op]func(T *testing.T, inMsg message.InboundMessage, subnetID ids.ID, validatorOnly bool) bool
}

// Default set the default callable value to [cant]
func (s *ExternalSenderTest) Default(cant bool) {
	assert := assert.New(s.T)
	metrics := prometheus.NewRegistry()
	mc, err := message.NewCreator(metrics, true /*compress*/, "dummyNamespace" /*parentNamespace*/)
	assert.NoError(err)
	s.mc = mc

	s.disabledSend = make(map[message.Op]struct{})
	s.disabledGossip = make(map[message.Op]struct{})

	s.sendFMap = make(map[message.Op]func(T *testing.T, inMsg message.InboundMessage, nodeIDs ids.ShortSet, subnetID ids.ID, validatorOnly bool) ids.ShortSet)
	s.gossipFMap = make(map[message.Op]func(T *testing.T, inMsg message.InboundMessage, subnetID ids.ID, validatorOnly bool) bool)

	if cant {
		s.disabledSend[message.GetAcceptedFrontier] = struct{}{}
		s.disabledSend[message.AcceptedFrontier] = struct{}{}
		s.disabledSend[message.GetAccepted] = struct{}{}
		s.disabledSend[message.Accepted] = struct{}{}
		s.disabledSend[message.GetAncestors] = struct{}{}
		s.disabledSend[message.MultiPut] = struct{}{}
		s.disabledSend[message.Get] = struct{}{}
		s.disabledSend[message.Put] = struct{}{}
		s.disabledSend[message.PullQuery] = struct{}{}
		s.disabledSend[message.PushQuery] = struct{}{}
		s.disabledSend[message.Chits] = struct{}{}
		s.disabledSend[message.AppRequest] = struct{}{}
		s.disabledSend[message.AppResponse] = struct{}{}
		s.disabledSend[message.AppGossip] = struct{}{} // AppSpecificGossip

		s.disabledSend[message.Put] = struct{}{} // gossip of ordinary containers happens via Put msg
		s.disabledGossip[message.AppGossip] = struct{}{}
	}
}

func (s *ExternalSenderTest) EnableSend(msgType message.Op) {
	delete(s.disabledSend, msgType)
}

func (s *ExternalSenderTest) DisableSend(msgType message.Op) {
	s.disabledSend[msgType] = struct{}{}
}

func (s *ExternalSenderTest) MockSend(msgType message.Op,
	f func(T *testing.T, inMsg message.InboundMessage, nodeIDs ids.ShortSet, subnetID ids.ID, validatorOnly bool) ids.ShortSet) {
	s.sendFMap[msgType] = f
}

func (s *ExternalSenderTest) ClearMockSend(msgType message.Op) {
	delete(s.sendFMap, msgType)
}

// Given a msg type, the corresponding mock function is called if it was initialized.
// If it wasn't initialized and this function shouldn't be called and testing was
// initialized, then testing will fail.
func (s *ExternalSenderTest) Send(outMsg message.OutboundMessage, nodeIDs ids.ShortSet, subnetID ids.ID, validatorOnly bool) ids.ShortSet {
	assert := assert.New(s.T)

	// turn  message.OutboundMessage into  message.InboundMessage so be able to retrieve fields
	dummyNodeID := ids.ShortEmpty
	dummyOnFinishedHandling := func() {}
	inMsg, err := s.mc.Parse(outMsg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
	assert.NoError(err)

	_, isDisabled := s.disabledSend[outMsg.Op()]

	res := ids.NewShortSet(nodeIDs.Len())
	switch outMsg.Op() {
	case
		message.GetAcceptedFrontier,
		message.AcceptedFrontier,
		message.GetAccepted,
		message.Accepted,
		message.GetAncestors,
		message.MultiPut,
		message.Get,
		message.Put,
		message.PushQuery,
		message.PullQuery,
		message.Chits,
		message.AppRequest,
		message.AppResponse:

		if mock, ok := s.sendFMap[outMsg.Op()]; ok {
			return mock(s.T, inMsg, nodeIDs, subnetID, validatorOnly)
		}

		switch {
		case isDisabled && s.T != nil:
			s.T.Fatalf("Unexpectedly called send for %s msg type", outMsg.Op().String())
		case isDisabled && s.B != nil:
			s.T.Fatalf("Unexpectedly called send for %s msg type", outMsg.Op().String())
		}

	default:
		s.T.Fatalf("Attempt to send unhandled message type")
	}

	return res
}

// Given a msg type, the corresponding mock function is called if it was initialized.
// If it wasn't initialized and this function shouldn't be called and testing was
// initialized, then testing will fail.
func (s *ExternalSenderTest) Gossip(
	outMsg message.OutboundMessage,
	subnetID ids.ID,
	validatorOnly bool) bool {
	assert := assert.New(s.T)

	// turn  message.OutboundMessage into  message.InboundMessage so be able to retrieve fields
	dummyNodeID := ids.ShortEmpty
	dummyOnFinishedHandling := func() {}
	inMsg, err := s.mc.Parse(outMsg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
	assert.NoError(err)

	_, isDisabled := s.disabledGossip[outMsg.Op()]

	switch outMsg.Op() {
	case
		message.AppGossip,
		message.Put:
		if mock, ok := s.gossipFMap[outMsg.Op()]; ok {
			return mock(s.T, inMsg, subnetID, validatorOnly)
		}

		switch {
		case isDisabled && s.T != nil:
			s.T.Fatalf("Unexpectedly called gossip for %s msg type", outMsg.Op().String())
			return false
		case isDisabled && s.B != nil:
			s.T.Fatalf("Unexpectedly called gossip for %s msg type", outMsg.Op().String())
			return false
		}

	default:
		s.T.Fatalf("Attempt to gossip unhandled message type")
		return false
	}

	return false
}
