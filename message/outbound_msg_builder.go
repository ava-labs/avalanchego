// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/ips"
)

// OutboundMsgBuilder builds outbound messages. Outbound messages are returned
// with a reference count of 1. Once the reference count hits 0, the message
// bytes should no longer be accessed.
type OutboundMsgBuilder interface {
	Version(
		networkID uint32,
		myTime uint64,
		ip ips.IPPort,
		myVersion string,
		myVersionTime uint64,
		sig []byte,
		trackedSubnets []ids.ID,
	) (OutboundMessage, error)

	PeerList(
		peers []ips.ClaimedIPPort,
		bypassThrottling bool,
	) (OutboundMessage, error)

	Ping() (OutboundMessage, error)

	Pong(uptimePercentage uint8) (OutboundMessage, error)

	GetStateSummaryFrontier(
		chainID ids.ID,
		requestID uint32,
		deadline time.Duration,
	) (OutboundMessage, error)

	StateSummaryFrontier(
		chainID ids.ID,
		requestID uint32,
		summary []byte,
	) (OutboundMessage, error)

	GetAcceptedStateSummary(
		chainID ids.ID,
		requestID uint32,
		deadline time.Duration,
		heights []uint64,
	) (OutboundMessage, error)

	AcceptedStateSummary(
		chainID ids.ID,
		requestID uint32,
		summaryIDs []ids.ID,
	) (OutboundMessage, error)

	GetAcceptedFrontier(
		chainID ids.ID,
		requestID uint32,
		deadline time.Duration,
	) (OutboundMessage, error)

	AcceptedFrontier(
		chainID ids.ID,
		requestID uint32,
		containerIDs []ids.ID,
	) (OutboundMessage, error)

	GetAccepted(
		chainID ids.ID,
		requestID uint32,
		deadline time.Duration,
		containerIDs []ids.ID,
	) (OutboundMessage, error)

	Accepted(
		chainID ids.ID,
		requestID uint32,
		containerIDs []ids.ID,
	) (OutboundMessage, error)

	GetAncestors(
		chainID ids.ID,
		requestID uint32,
		deadline time.Duration,
		containerID ids.ID,
	) (OutboundMessage, error)

	Ancestors(
		chainID ids.ID,
		requestID uint32,
		containers [][]byte,
	) (OutboundMessage, error)

	Get(
		chainID ids.ID,
		requestID uint32,
		deadline time.Duration,
		containerID ids.ID,
	) (OutboundMessage, error)

	Put(
		chainID ids.ID,
		requestID uint32,
		container []byte,
	) (OutboundMessage, error)

	PushQuery(
		chainID ids.ID,
		requestID uint32,
		deadline time.Duration,
		container []byte,
	) (OutboundMessage, error)

	PullQuery(
		chainID ids.ID,
		requestID uint32,
		deadline time.Duration,
		containerID ids.ID,
	) (OutboundMessage, error)

	Chits(
		chainID ids.ID,
		requestID uint32,
		containerIDs []ids.ID,
	) (OutboundMessage, error)

	AppRequest(
		chainID ids.ID,
		requestID uint32,
		deadline time.Duration,
		msg []byte,
	) (OutboundMessage, error)

	AppResponse(
		chainID ids.ID,
		requestID uint32,
		msg []byte,
	) (OutboundMessage, error)

	AppGossip(
		chainID ids.ID,
		msg []byte,
	) (OutboundMessage, error)
}
