// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/utils/ips"
)

var _ OutboundMsgBuilder = (*outMsgBuilder)(nil)

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

	PeerListAck(
		peerAcks []*p2p.PeerAck,
	) (OutboundMessage, error)

	Ping() (OutboundMessage, error)

	Pong(
		primaryUptime uint32,
		subnetUptimes []*p2p.SubnetUptime,
	) (OutboundMessage, error)

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
		engineType p2p.EngineType,
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
		engineType p2p.EngineType,
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
		engineType p2p.EngineType,
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
		engineType p2p.EngineType,
	) (OutboundMessage, error)

	Put(
		chainID ids.ID,
		requestID uint32,
		container []byte,
		engineType p2p.EngineType,
	) (OutboundMessage, error)

	PushQuery(
		chainID ids.ID,
		requestID uint32,
		deadline time.Duration,
		container []byte,
		engineType p2p.EngineType,
	) (OutboundMessage, error)

	PullQuery(
		chainID ids.ID,
		requestID uint32,
		deadline time.Duration,
		containerID ids.ID,
		engineType p2p.EngineType,
	) (OutboundMessage, error)

	Chits(
		chainID ids.ID,
		requestID uint32,
		preferredContainerIDs []ids.ID,
		acceptedContainerIDs []ids.ID,
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
	compress bool // set to "true" if compression is enabled

	builder *msgBuilder
}

// Use "message.NewCreator" to import this function
// since we do not expose "msgBuilder" yet
func newOutboundBuilder(enableCompression bool, builder *msgBuilder) OutboundMsgBuilder {
	return &outMsgBuilder{
		compress: enableCompression,
		builder:  builder,
	}
}

func (b *outMsgBuilder) Ping() (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_Ping{
				Ping: &p2p.Ping{},
			},
		},
		false,
		false,
	)
}

func (b *outMsgBuilder) Pong(
	primaryUptime uint32,
	subnetUptimes []*p2p.SubnetUptime,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_Pong{
				Pong: &p2p.Pong{
					Uptime:        primaryUptime,
					SubnetUptimes: subnetUptimes,
				},
			},
		},
		false,
		false,
	)
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
	encodeIDs(trackedSubnets, subnetIDBytes)
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_Version{
				Version: &p2p.Version{
					NetworkId:      networkID,
					MyTime:         myTime,
					IpAddr:         ip.IP.To16(),
					IpPort:         uint32(ip.Port),
					MyVersion:      myVersion,
					MyVersionTime:  myVersionTime,
					Sig:            sig,
					TrackedSubnets: subnetIDBytes,
				},
			},
		},
		false,
		true,
	)
}

func (b *outMsgBuilder) PeerList(peers []ips.ClaimedIPPort, bypassThrottling bool) (OutboundMessage, error) {
	claimIPPorts := make([]*p2p.ClaimedIpPort, len(peers))
	for i, p := range peers {
		claimIPPorts[i] = &p2p.ClaimedIpPort{
			X509Certificate: p.Cert.Raw,
			IpAddr:          p.IPPort.IP.To16(),
			IpPort:          uint32(p.IPPort.Port),
			Timestamp:       p.Timestamp,
			Signature:       p.Signature,
			TxId:            p.TxID[:],
		}
	}
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_PeerList{
				PeerList: &p2p.PeerList{
					ClaimedIpPorts: claimIPPorts,
				},
			},
		},
		b.compress,
		bypassThrottling,
	)
}

func (b *outMsgBuilder) PeerListAck(peerAcks []*p2p.PeerAck) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_PeerListAck{
				PeerListAck: &p2p.PeerListAck{
					PeerAcks: peerAcks,
				},
			},
		},
		false,
		false,
	)
}

func (b *outMsgBuilder) GetStateSummaryFrontier(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_GetStateSummaryFrontier{
				GetStateSummaryFrontier: &p2p.GetStateSummaryFrontier{
					ChainId:   chainID[:],
					RequestId: requestID,
					Deadline:  uint64(deadline),
				},
			},
		},
		false,
		false,
	)
}

func (b *outMsgBuilder) StateSummaryFrontier(
	chainID ids.ID,
	requestID uint32,
	summary []byte,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_StateSummaryFrontier_{
				StateSummaryFrontier_: &p2p.StateSummaryFrontier{
					ChainId:   chainID[:],
					RequestId: requestID,
					Summary:   summary,
				},
			},
		},
		b.compress,
		false,
	)
}

func (b *outMsgBuilder) GetAcceptedStateSummary(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	heights []uint64,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_GetAcceptedStateSummary{
				GetAcceptedStateSummary: &p2p.GetAcceptedStateSummary{
					ChainId:   chainID[:],
					RequestId: requestID,
					Deadline:  uint64(deadline),
					Heights:   heights,
				},
			},
		},
		b.compress,
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
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_AcceptedStateSummary_{
				AcceptedStateSummary_: &p2p.AcceptedStateSummary{
					ChainId:    chainID[:],
					RequestId:  requestID,
					SummaryIds: summaryIDBytes,
				},
			},
		},
		b.compress,
		false,
	)
}

func (b *outMsgBuilder) GetAcceptedFrontier(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	engineType p2p.EngineType,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_GetAcceptedFrontier{
				GetAcceptedFrontier: &p2p.GetAcceptedFrontier{
					ChainId:    chainID[:],
					RequestId:  requestID,
					Deadline:   uint64(deadline),
					EngineType: engineType,
				},
			},
		},
		false,
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
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_AcceptedFrontier_{
				AcceptedFrontier_: &p2p.AcceptedFrontier{
					ChainId:      chainID[:],
					RequestId:    requestID,
					ContainerIds: containerIDBytes,
				},
			},
		},
		false,
		false,
	)
}

func (b *outMsgBuilder) GetAccepted(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	containerIDs []ids.ID,
	engineType p2p.EngineType,
) (OutboundMessage, error) {
	containerIDBytes := make([][]byte, len(containerIDs))
	encodeIDs(containerIDs, containerIDBytes)
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_GetAccepted{
				GetAccepted: &p2p.GetAccepted{
					ChainId:      chainID[:],
					RequestId:    requestID,
					Deadline:     uint64(deadline),
					ContainerIds: containerIDBytes,
					EngineType:   engineType,
				},
			},
		},
		false,
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
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_Accepted_{
				Accepted_: &p2p.Accepted{
					ChainId:      chainID[:],
					RequestId:    requestID,
					ContainerIds: containerIDBytes,
				},
			},
		},
		false,
		false,
	)
}

func (b *outMsgBuilder) GetAncestors(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	containerID ids.ID,
	engineType p2p.EngineType,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_GetAncestors{
				GetAncestors: &p2p.GetAncestors{
					ChainId:     chainID[:],
					RequestId:   requestID,
					Deadline:    uint64(deadline),
					ContainerId: containerID[:],
					EngineType:  engineType,
				},
			},
		},
		false,
		false,
	)
}

func (b *outMsgBuilder) Ancestors(
	chainID ids.ID,
	requestID uint32,
	containers [][]byte,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_Ancestors_{
				Ancestors_: &p2p.Ancestors{
					ChainId:    chainID[:],
					RequestId:  requestID,
					Containers: containers,
				},
			},
		},
		b.compress,
		false,
	)
}

func (b *outMsgBuilder) Get(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	containerID ids.ID,
	engineType p2p.EngineType,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_Get{
				Get: &p2p.Get{
					ChainId:     chainID[:],
					RequestId:   requestID,
					Deadline:    uint64(deadline),
					ContainerId: containerID[:],
					EngineType:  engineType,
				},
			},
		},
		false,
		false,
	)
}

func (b *outMsgBuilder) Put(
	chainID ids.ID,
	requestID uint32,
	container []byte,
	engineType p2p.EngineType,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_Put{
				Put: &p2p.Put{
					ChainId:    chainID[:],
					RequestId:  requestID,
					Container:  container,
					EngineType: engineType,
				},
			},
		},
		b.compress,
		false,
	)
}

func (b *outMsgBuilder) PushQuery(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	container []byte,
	engineType p2p.EngineType,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_PushQuery{
				PushQuery: &p2p.PushQuery{
					ChainId:    chainID[:],
					RequestId:  requestID,
					Deadline:   uint64(deadline),
					Container:  container,
					EngineType: engineType,
				},
			},
		},
		b.compress,
		false,
	)
}

func (b *outMsgBuilder) PullQuery(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	containerID ids.ID,
	engineType p2p.EngineType,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_PullQuery{
				PullQuery: &p2p.PullQuery{
					ChainId:     chainID[:],
					RequestId:   requestID,
					Deadline:    uint64(deadline),
					ContainerId: containerID[:],
					EngineType:  engineType,
				},
			},
		},
		false,
		false,
	)
}

func (b *outMsgBuilder) Chits(
	chainID ids.ID,
	requestID uint32,
	preferredContainerIDs []ids.ID,
	acceptedContainerIDs []ids.ID,
) (OutboundMessage, error) {
	preferredContainerIDBytes := make([][]byte, len(preferredContainerIDs))
	encodeIDs(preferredContainerIDs, preferredContainerIDBytes)
	acceptedContainerIDBytes := make([][]byte, len(acceptedContainerIDs))
	encodeIDs(acceptedContainerIDs, acceptedContainerIDBytes)
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_Chits{
				Chits: &p2p.Chits{
					ChainId:               chainID[:],
					RequestId:             requestID,
					PreferredContainerIds: preferredContainerIDBytes,
					AcceptedContainerIds:  acceptedContainerIDBytes,
				},
			},
		},
		false,
		false,
	)
}

func (b *outMsgBuilder) AppRequest(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	msg []byte,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_AppRequest{
				AppRequest: &p2p.AppRequest{
					ChainId:   chainID[:],
					RequestId: requestID,
					Deadline:  uint64(deadline),
					AppBytes:  msg,
				},
			},
		},
		b.compress,
		false,
	)
}

func (b *outMsgBuilder) AppResponse(chainID ids.ID, requestID uint32, msg []byte) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_AppResponse{
				AppResponse: &p2p.AppResponse{
					ChainId:   chainID[:],
					RequestId: requestID,
					AppBytes:  msg,
				},
			},
		},
		b.compress,
		false,
	)
}

func (b *outMsgBuilder) AppGossip(chainID ids.ID, msg []byte) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_AppGossip{
				AppGossip: &p2p.AppGossip{
					ChainId:  chainID[:],
					AppBytes: msg,
				},
			},
		},
		b.compress,
		false,
	)
}
