// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/ips"
)

var _ OutboundMsgBuilder = &outMsgBuilderWithPacker{}

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

type outMsgBuilderWithPacker struct {
	c        Codec
	compress bool
}

func NewOutboundBuilderWithPacker(c Codec, enableCompression bool) OutboundMsgBuilder {
	return &outMsgBuilderWithPacker{
		c:        c,
		compress: enableCompression,
	}
}

func (b *outMsgBuilderWithPacker) Version(
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
		b.compress && Version.Compressible(),
		true,
	)
}

func (b *outMsgBuilderWithPacker) PeerList(peers []ips.ClaimedIPPort, bypassThrottling bool) (OutboundMessage, error) {
	return b.c.Pack(
		PeerList,
		map[Field]interface{}{
			Peers: peers,
		},
		b.compress && PeerList.Compressible(),
		bypassThrottling,
	)
}

func (b *outMsgBuilderWithPacker) Ping() (OutboundMessage, error) {
	return b.c.Pack(
		Ping,
		nil,
		b.compress && Ping.Compressible(),
		false,
	)
}

func (b *outMsgBuilderWithPacker) Pong(uptimePercentage uint8) (OutboundMessage, error) {
	return b.c.Pack(
		Pong,
		map[Field]interface{}{
			Uptime: uptimePercentage,
		},
		b.compress && Pong.Compressible(),
		false,
	)
}

func (b *outMsgBuilderWithPacker) GetStateSummaryFrontier(
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
		b.compress && GetStateSummaryFrontier.Compressible(),
		false,
	)
}

func (b *outMsgBuilderWithPacker) StateSummaryFrontier(
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
		b.compress && StateSummaryFrontier.Compressible(),
		false,
	)
}

func (b *outMsgBuilderWithPacker) GetAcceptedStateSummary(
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
		b.compress && GetAcceptedStateSummary.Compressible(),
		false,
	)
}

func (b *outMsgBuilderWithPacker) AcceptedStateSummary(
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
		b.compress && AcceptedStateSummary.Compressible(),
		false,
	)
}

func (b *outMsgBuilderWithPacker) GetAcceptedFrontier(
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
		b.compress && GetAcceptedFrontier.Compressible(),
		false,
	)
}

func (b *outMsgBuilderWithPacker) AcceptedFrontier(
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
		b.compress && AcceptedFrontier.Compressible(),
		false,
	)
}

func (b *outMsgBuilderWithPacker) GetAccepted(
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
		b.compress && GetAccepted.Compressible(),
		false,
	)
}

func (b *outMsgBuilderWithPacker) Accepted(
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
		b.compress && Accepted.Compressible(),
		false,
	)
}

func (b *outMsgBuilderWithPacker) GetAncestors(
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
		b.compress && GetAncestors.Compressible(),
		false,
	)
}

func (b *outMsgBuilderWithPacker) Ancestors(
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
		b.compress && Ancestors.Compressible(),
		false,
	)
}

func (b *outMsgBuilderWithPacker) Get(
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
		b.compress && Get.Compressible(),
		false,
	)
}

func (b *outMsgBuilderWithPacker) Put(
	chainID ids.ID,
	requestID uint32,
	container []byte,
) (OutboundMessage, error) {
	return b.c.Pack(
		Put,
		map[Field]interface{}{
			ChainID:        chainID[:],
			RequestID:      requestID,
			ContainerID:    ids.Empty[:], // Populated for backwards compatibility
			ContainerBytes: container,
		},
		b.compress && Put.Compressible(),
		false,
	)
}

func (b *outMsgBuilderWithPacker) PushQuery(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	container []byte,
) (OutboundMessage, error) {
	return b.c.Pack(
		PushQuery,
		map[Field]interface{}{
			ChainID:        chainID[:],
			RequestID:      requestID,
			Deadline:       uint64(deadline),
			ContainerID:    ids.Empty[:], // Populated for backwards compatibility
			ContainerBytes: container,
		},
		b.compress && PushQuery.Compressible(),
		false,
	)
}

func (b *outMsgBuilderWithPacker) PullQuery(
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
		b.compress && PullQuery.Compressible(),
		false,
	)
}

func (b *outMsgBuilderWithPacker) Chits(
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
		b.compress && Chits.Compressible(),
		false,
	)
}

// Application level request
func (b *outMsgBuilderWithPacker) AppRequest(
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
		b.compress && AppRequest.Compressible(),
		false,
	)
}

// Application level response
func (b *outMsgBuilderWithPacker) AppResponse(chainID ids.ID, requestID uint32, msg []byte) (OutboundMessage, error) {
	return b.c.Pack(
		AppResponse,
		map[Field]interface{}{
			ChainID:   chainID[:],
			RequestID: requestID,
			AppBytes:  msg,
		},
		b.compress && AppResponse.Compressible(),
		false,
	)
}

// Application level gossiped message
func (b *outMsgBuilderWithPacker) AppGossip(chainID ids.ID, msg []byte) (OutboundMessage, error) {
	return b.c.Pack(
		AppGossip,
		map[Field]interface{}{
			ChainID:  chainID[:],
			AppBytes: msg,
		},
		b.compress && AppGossip.Compressible(),
		false,
	)
}
