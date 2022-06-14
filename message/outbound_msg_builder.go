// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/ips"
)

var _ OutboundMsgBuilder = &outMsgBuilder{}

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
		containerID ids.ID,
		container []byte,
	) (OutboundMessage, error)

	PushQuery(
		chainID ids.ID,
		requestID uint32,
		deadline time.Duration,
		containerID ids.ID,
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

	ChitsV2(
		chainID ids.ID,
		requestID uint32,
		containerIDs []ids.ID,
		containerID ids.ID,
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

type outMsgBuilder struct {
	c        Codec
	compress bool
}

func NewOutboundBuilder(c Codec, enableCompression bool) OutboundMsgBuilder {
	return &outMsgBuilder{
		c:        c,
		compress: enableCompression,
	}
}

func (b *outMsgBuilder) Version(
	networkID uint32,
	myTime uint64,
	ip ips.IPPort,
	myVersion string,
	myVersionTime uint64,
	sig []byte,
	trackedSubnets []ids.ID,
) (OutboundMessage, error) {
	subnetIDBytes := make([][]byte, len(trackedSubnets))
	for i, containerID := range trackedSubnets {
		copy := containerID
		subnetIDBytes[i] = copy[:]
	}
	return b.c.Pack(
		Version,
		map[Field]interface{}{
			NetworkID:      networkID,
			NodeID:         uint32(0),
			MyTime:         myTime,
			IP:             ip,
			VersionStr:     myVersion,
			VersionTime:    myVersionTime,
			SigBytes:       sig,
			TrackedSubnets: subnetIDBytes,
		},
		Version.Compressible(), // Version Messages can't be compressed
		true,
	)
}

func (b *outMsgBuilder) PeerList(peers []ips.ClaimedIPPort, bypassThrottling bool) (OutboundMessage, error) {
	return b.c.Pack(
		PeerList,
		map[Field]interface{}{
			Peers: peers,
		},
		b.compress && PeerList.Compressible(), // PeerList messages may be compressed
		bypassThrottling,
	)
}

func (b *outMsgBuilder) Ping() (OutboundMessage, error) {
	return b.c.Pack(
		Ping,
		nil,
		Ping.Compressible(), // Ping messages can't be compressed
		false,
	)
}

func (b *outMsgBuilder) Pong(uptimePercentage uint8) (OutboundMessage, error) {
	return b.c.Pack(
		Pong,
		map[Field]interface{}{
			Uptime: uptimePercentage,
		},
		Pong.Compressible(), // Pong messages can't be compressed
		false,
	)
}

func (b *outMsgBuilder) GetStateSummaryFrontier(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
) (OutboundMessage, error) {
	return b.c.Pack(
		GetStateSummaryFrontier,
		map[Field]interface{}{
			ChainID:   chainID[:],
			RequestID: requestID,
			Deadline:  uint64(deadline),
		},
		GetStateSummaryFrontier.Compressible(), // GetStateSummaryFrontier messages can't be compressed
		false,
	)
}

func (b *outMsgBuilder) StateSummaryFrontier(
	chainID ids.ID,
	requestID uint32,
	summary []byte,
) (OutboundMessage, error) {
	return b.c.Pack(
		StateSummaryFrontier,
		map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			SummaryBytes: summary,
		},
		b.compress && StateSummaryFrontier.Compressible(), // StateSummaryFrontier messages may be compressed
		false,
	)
}

func (b *outMsgBuilder) GetAcceptedStateSummary(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	heights []uint64,
) (OutboundMessage, error) {
	return b.c.Pack(
		GetAcceptedStateSummary,
		map[Field]interface{}{
			ChainID:        chainID[:],
			RequestID:      requestID,
			Deadline:       uint64(deadline),
			SummaryHeights: heights,
		},
		b.compress && GetAcceptedStateSummary.Compressible(), // GetAcceptedStateSummary messages may be compressed
		false,
	)
}

func (b *outMsgBuilder) AcceptedStateSummary(
	chainID ids.ID,
	requestID uint32,
	summaryIDs []ids.ID,
) (OutboundMessage, error) {
	summaryIDBytes := make([][]byte, len(summaryIDs))
	encodeIDs(summaryIDs, summaryIDBytes)
	return b.c.Pack(
		AcceptedStateSummary,
		map[Field]interface{}{
			ChainID:    chainID[:],
			RequestID:  requestID,
			SummaryIDs: summaryIDBytes,
		},
		b.compress && AcceptedStateSummary.Compressible(), // AcceptedStateSummary messages may be compressed
		false,
	)
}

func (b *outMsgBuilder) GetAcceptedFrontier(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
) (OutboundMessage, error) {
	return b.c.Pack(
		GetAcceptedFrontier,
		map[Field]interface{}{
			ChainID:   chainID[:],
			RequestID: requestID,
			Deadline:  uint64(deadline),
		},
		GetAcceptedFrontier.Compressible(), // GetAcceptedFrontier messages can't be compressed
		false,
	)
}

func (b *outMsgBuilder) AcceptedFrontier(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
) (OutboundMessage, error) {
	containerIDBytes := make([][]byte, len(containerIDs))
	encodeIDs(containerIDs, containerIDBytes)
	return b.c.Pack(
		AcceptedFrontier,
		map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			ContainerIDs: containerIDBytes,
		},
		AcceptedFrontier.Compressible(), // AcceptedFrontier messages can't be compressed
		false,
	)
}

func (b *outMsgBuilder) GetAccepted(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	containerIDs []ids.ID,
) (OutboundMessage, error) {
	containerIDBytes := make([][]byte, len(containerIDs))
	encodeIDs(containerIDs, containerIDBytes)
	return b.c.Pack(
		GetAccepted,
		map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			Deadline:     uint64(deadline),
			ContainerIDs: containerIDBytes,
		},
		GetAccepted.Compressible(), // GetAccepted messages can't be compressed
		false,
	)
}

func (b *outMsgBuilder) Accepted(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
) (OutboundMessage, error) {
	containerIDBytes := make([][]byte, len(containerIDs))
	encodeIDs(containerIDs, containerIDBytes)
	return b.c.Pack(
		Accepted,
		map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			ContainerIDs: containerIDBytes,
		},
		Accepted.Compressible(), // Accepted messages can't be compressed
		false,
	)
}

func (b *outMsgBuilder) GetAncestors(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	containerID ids.ID,
) (OutboundMessage, error) {
	return b.c.Pack(
		GetAncestors,
		map[Field]interface{}{
			ChainID:     chainID[:],
			RequestID:   requestID,
			Deadline:    uint64(deadline),
			ContainerID: containerID[:],
		},
		GetAncestors.Compressible(), // GetAncestors messages can't be compressed
		false,
	)
}

func (b *outMsgBuilder) Ancestors(
	chainID ids.ID,
	requestID uint32,
	containers [][]byte,
) (OutboundMessage, error) {
	return b.c.Pack(
		Ancestors,
		map[Field]interface{}{
			ChainID:             chainID[:],
			RequestID:           requestID,
			MultiContainerBytes: containers,
		},
		b.compress && Ancestors.Compressible(), // Ancestors messages may be compressed
		false,
	)
}

func (b *outMsgBuilder) Get(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	containerID ids.ID,
) (OutboundMessage, error) {
	return b.c.Pack(
		Get,
		map[Field]interface{}{
			ChainID:     chainID[:],
			RequestID:   requestID,
			Deadline:    uint64(deadline),
			ContainerID: containerID[:],
		},
		Get.Compressible(), // Get messages can't be compressed
		false,
	)
}

func (b *outMsgBuilder) Put(
	chainID ids.ID,
	requestID uint32,
	containerID ids.ID,
	container []byte,
) (OutboundMessage, error) {
	return b.c.Pack(
		Put,
		map[Field]interface{}{
			ChainID:        chainID[:],
			RequestID:      requestID,
			ContainerID:    containerID[:],
			ContainerBytes: container,
		},
		b.compress && Put.Compressible(), // Put messages may be compressed
		false,
	)
}

func (b *outMsgBuilder) PushQuery(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	containerID ids.ID,
	container []byte,
) (OutboundMessage, error) {
	return b.c.Pack(
		PushQuery,
		map[Field]interface{}{
			ChainID:        chainID[:],
			RequestID:      requestID,
			Deadline:       uint64(deadline),
			ContainerID:    containerID[:],
			ContainerBytes: container,
		},
		b.compress && PushQuery.Compressible(), // PushQuery messages may be compressed
		false,
	)
}

func (b *outMsgBuilder) PullQuery(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	containerID ids.ID,
) (OutboundMessage, error) {
	return b.c.Pack(
		PullQuery,
		map[Field]interface{}{
			ChainID:     chainID[:],
			RequestID:   requestID,
			Deadline:    uint64(deadline),
			ContainerID: containerID[:],
		},
		PullQuery.Compressible(), // PullQuery messages can't be compressed
		false,
	)
}

func (b *outMsgBuilder) Chits(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
) (OutboundMessage, error) {
	containerIDBytes := make([][]byte, len(containerIDs))
	encodeIDs(containerIDs, containerIDBytes)
	return b.c.Pack(
		Chits,
		map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			ContainerIDs: containerIDBytes,
		},
		Chits.Compressible(), // Chits messages can't be compressed
		false,
	)
}

func (b *outMsgBuilder) ChitsV2(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
	containerID ids.ID,
) (OutboundMessage, error) {
	containerIDBytes := make([][]byte, len(containerIDs))
	encodeIDs(containerIDs, containerIDBytes)

	return b.c.Pack(
		ChitsV2,
		map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			ContainerIDs: containerIDBytes,
			ContainerID:  containerID[:],
		},
		ChitsV2.Compressible(), // ChitsV2 messages can't be compressed
		false,
	)
}

// Application level request
func (b *outMsgBuilder) AppRequest(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	msg []byte,
) (OutboundMessage, error) {
	return b.c.Pack(
		AppRequest,
		map[Field]interface{}{
			ChainID:   chainID[:],
			RequestID: requestID,
			Deadline:  uint64(deadline),
			AppBytes:  msg,
		},
		b.compress && AppRequest.Compressible(), // App messages may be compressed
		false,
	)
}

// Application level response
func (b *outMsgBuilder) AppResponse(chainID ids.ID, requestID uint32, msg []byte) (OutboundMessage, error) {
	return b.c.Pack(
		AppResponse,
		map[Field]interface{}{
			ChainID:   chainID[:],
			RequestID: requestID,
			AppBytes:  msg,
		},
		b.compress && AppResponse.Compressible(), // App messages may be compressed
		false,
	)
}

// Application level gossiped message
func (b *outMsgBuilder) AppGossip(chainID ids.ID, msg []byte) (OutboundMessage, error) {
	return b.c.Pack(
		AppGossip,
		map[Field]interface{}{
			ChainID:  chainID[:],
			AppBytes: msg,
		},
		b.compress && AppGossip.Compressible(), // App messages may be compressed
		false,
	)
}
