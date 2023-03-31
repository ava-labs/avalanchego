// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	errSend   = errors.New("unexpectedly called Send")
	errGossip = errors.New("unexpectedly called Gossip")
)

// ExternalSenderTest is a test sender
type ExternalSenderTest struct {
	TB testing.TB

	CantSend, CantGossip bool

	SendF   func(msg message.OutboundMessage, nodeIDs set.Set[ids.NodeID], subnetID ids.ID, allower subnets.Allower) set.Set[ids.NodeID]
	GossipF func(msg message.OutboundMessage, subnetID ids.ID, numValidatorsToSend, numNonValidatorsToSend, numPeersToSend int, allower subnets.Allower) set.Set[ids.NodeID]
}

// Default set the default callable value to [cant]
func (s *ExternalSenderTest) Default(cant bool) {
	s.CantSend = cant
	s.CantGossip = cant
}

func (s *ExternalSenderTest) Send(
	msg message.OutboundMessage,
	nodeIDs set.Set[ids.NodeID],
	subnetID ids.ID,
	allower subnets.Allower,
) set.Set[ids.NodeID] {
	if s.SendF != nil {
		return s.SendF(msg, nodeIDs, subnetID, allower)
	}
	if s.CantSend {
		if s.TB != nil {
			s.TB.Helper()
			s.TB.Fatal(errSend)
		}
	}
	return nil
}

// Given a msg type, the corresponding mock function is called if it was initialized.
// If it wasn't initialized and this function shouldn't be called and testing was
// initialized, then testing will fail.
func (s *ExternalSenderTest) Gossip(
	msg message.OutboundMessage,
	subnetID ids.ID,
	numValidatorsToSend int,
	numNonValidatorsToSend int,
	numPeersToSend int,
	allower subnets.Allower,
) set.Set[ids.NodeID] {
	if s.GossipF != nil {
		return s.GossipF(msg, subnetID, numValidatorsToSend, numNonValidatorsToSend, numPeersToSend, allower)
	}
	if s.CantGossip {
		if s.TB != nil {
			s.TB.Helper()
			s.TB.Fatal(errGossip)
		}
	}
	return nil
}
