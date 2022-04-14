// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

import (
	"errors"
	"testing"

	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/message"
)

var (
	errSend   = errors.New("unexpectedly called Send")
	errGossip = errors.New("unexpectedly called Gossip")
)

// ExternalSenderTest is a test sender
type ExternalSenderTest struct {
	TB testing.TB

	CantSend, CantGossip bool

	SendF   func(msg message.OutboundMessage, nodeIDs ids.ShortSet, subnetID ids.ID, validatorOnly bool) ids.ShortSet
	GossipF func(msg message.OutboundMessage, subnetID ids.ID, validatorOnly bool, numValidatorsToSend, numNonValidatorsToSend, numPeersToSend int) ids.ShortSet
}

// Default set the default callable value to [cant]
func (s *ExternalSenderTest) Default(cant bool) {
	s.CantSend = cant
	s.CantGossip = cant
}

func (s *ExternalSenderTest) Send(
	msg message.OutboundMessage,
	nodeIDs ids.ShortSet,
	subnetID ids.ID,
	validatorOnly bool,
) ids.ShortSet {
	if s.SendF != nil {
		return s.SendF(msg, nodeIDs, subnetID, validatorOnly)
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
	validatorOnly bool,
	numValidatorsToSend int,
	numNonValidatorsToSend int,
	numPeersToSend int,
) ids.ShortSet {
	if s.GossipF != nil {
		return s.GossipF(msg, subnetID, validatorOnly, numValidatorsToSend, numNonValidatorsToSend, numPeersToSend)
	}
	if s.CantGossip {
		if s.TB != nil {
			s.TB.Helper()
			s.TB.Fatal(errGossip)
		}
	}
	return nil
}
