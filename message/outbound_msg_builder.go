// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"net/netip"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/utils/compression"
	"github.com/ava-labs/avalanchego/utils/ips"
)

var _ OutboundMsgBuilder = (*outMsgBuilder)(nil)

// OutboundMsgBuilder builds outbound messages. Outbound messages are returned
// with a reference count of 1. Once the reference count hits 0, the message
// bytes should no longer be accessed.
type OutboundMsgBuilder interface {
	Handshake(
		networkID uint32,
		myTime uint64,
		ip netip.AddrPort,
		client string,
		major uint32,
		minor uint32,
		patch uint32,
		upgradeTime uint64,
		ipSigningTime uint64,
		ipNodeIDSig []byte,
		ipBLSSig []byte,
		trackedSubnets []ids.ID,
		supportedACPs []uint32,
		objectedACPs []uint32,
		knownPeersFilter []byte,
		knownPeersSalt []byte,
		requestAllSubnetIPs bool,
	) (OutboundMessage, error)

	GetPeerList(
		knownPeersFilter []byte,
		knownPeersSalt []byte,
		requestAllSubnetIPs bool,
	) (OutboundMessage, error)

	PeerList(
		peers []*ips.ClaimedIPPort,
		bypassThrottling bool,
	) (OutboundMessage, error)

	Ping(
		primaryUptime uint32,
	) (OutboundMessage, error)

	Pong() (OutboundMessage, error)

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
		containerID ids.ID,
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
		requestedHeight uint64,
	) (OutboundMessage, error)

	PullQuery(
		chainID ids.ID,
		requestID uint32,
		deadline time.Duration,
		containerID ids.ID,
		requestedHeight uint64,
	) (OutboundMessage, error)

	Chits(
		chainID ids.ID,
		requestID uint32,
		preferredID ids.ID,
		preferredIDAtHeight ids.ID,
		acceptedID ids.ID,
		acceptedHeight uint64,
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

	AppError(
		chainID ids.ID,
		requestID uint32,
		errorCode int32,
		errorMessage string,
	) (OutboundMessage, error)

	AppGossip(
		chainID ids.ID,
		msg []byte,
	) (OutboundMessage, error)

	SimplexMessage(
		msg *p2p.Simplex,
	) (OutboundMessage, error)
}

type outMsgBuilder struct {
	compressionType compression.Type

	builder *msgBuilder
}

// Use "message.NewCreator" to import this function
// since we do not expose "msgBuilder" yet
func newOutboundBuilder(compressionType compression.Type, builder *msgBuilder) OutboundMsgBuilder {
	return &outMsgBuilder{
		compressionType: compressionType,
		builder:         builder,
	}
}

func (b *outMsgBuilder) Ping(
	primaryUptime uint32,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_Ping{
				Ping: &p2p.Ping{
					Uptime: primaryUptime,
				},
			},
		},
		compression.TypeNone,
		false,
	)
}

func (b *outMsgBuilder) Pong() (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_Pong{
				Pong: &p2p.Pong{},
			},
		},
		compression.TypeNone,
		false,
	)
}

func (b *outMsgBuilder) Handshake(
	networkID uint32,
	myTime uint64,
	ip netip.AddrPort,
	client string,
	major uint32,
	minor uint32,
	patch uint32,
	upgradeTime uint64,
	ipSigningTime uint64,
	ipNodeIDSig []byte,
	ipBLSSig []byte,
	trackedSubnets []ids.ID,
	supportedACPs []uint32,
	objectedACPs []uint32,
	knownPeersFilter []byte,
	knownPeersSalt []byte,
	requestAllSubnetIPs bool,
) (OutboundMessage, error) {
	subnetIDBytes := make([][]byte, len(trackedSubnets))
	encodeIDs(trackedSubnets, subnetIDBytes)
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_Handshake{
				Handshake: &p2p.Handshake{
					NetworkId:      networkID,
					MyTime:         myTime,
					IpAddr:         ip.Addr().AsSlice(),
					IpPort:         uint32(ip.Port()),
					UpgradeTime:    upgradeTime,
					IpSigningTime:  ipSigningTime,
					IpNodeIdSig:    ipNodeIDSig,
					TrackedSubnets: subnetIDBytes,
					Client: &p2p.Client{
						Name:  client,
						Major: major,
						Minor: minor,
						Patch: patch,
					},
					SupportedAcps: supportedACPs,
					ObjectedAcps:  objectedACPs,
					KnownPeers: &p2p.BloomFilter{
						Filter: knownPeersFilter,
						Salt:   knownPeersSalt,
					},
					IpBlsSig:   ipBLSSig,
					AllSubnets: requestAllSubnetIPs,
				},
			},
		},
		compression.TypeNone,
		true,
	)
}

func (b *outMsgBuilder) GetPeerList(
	knownPeersFilter []byte,
	knownPeersSalt []byte,
	requestAllSubnetIPs bool,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_GetPeerList{
				GetPeerList: &p2p.GetPeerList{
					KnownPeers: &p2p.BloomFilter{
						Filter: knownPeersFilter,
						Salt:   knownPeersSalt,
					},
					AllSubnets: requestAllSubnetIPs,
				},
			},
		},
		b.compressionType,
		false,
	)
}

func (b *outMsgBuilder) PeerList(peers []*ips.ClaimedIPPort, bypassThrottling bool) (OutboundMessage, error) {
	claimIPPorts := make([]*p2p.ClaimedIpPort, len(peers))
	for i, p := range peers {
		claimIPPorts[i] = &p2p.ClaimedIpPort{
			X509Certificate: p.Cert.Raw,
			IpAddr:          p.AddrPort.Addr().AsSlice(),
			IpPort:          uint32(p.AddrPort.Port()),
			Timestamp:       p.Timestamp,
			Signature:       p.Signature,
			TxId:            ids.Empty[:],
		}
	}
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_PeerList_{
				PeerList_: &p2p.PeerList{
					ClaimedIpPorts: claimIPPorts,
				},
			},
		},
		b.compressionType,
		bypassThrottling,
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
		compression.TypeNone,
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
		b.compressionType,
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
		b.compressionType,
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
		b.compressionType,
		false,
	)
}

func (b *outMsgBuilder) GetAcceptedFrontier(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_GetAcceptedFrontier{
				GetAcceptedFrontier: &p2p.GetAcceptedFrontier{
					ChainId:   chainID[:],
					RequestId: requestID,
					Deadline:  uint64(deadline),
				},
			},
		},
		compression.TypeNone,
		false,
	)
}

func (b *outMsgBuilder) AcceptedFrontier(
	chainID ids.ID,
	requestID uint32,
	containerID ids.ID,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_AcceptedFrontier_{
				AcceptedFrontier_: &p2p.AcceptedFrontier{
					ChainId:     chainID[:],
					RequestId:   requestID,
					ContainerId: containerID[:],
				},
			},
		},
		compression.TypeNone,
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
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_GetAccepted{
				GetAccepted: &p2p.GetAccepted{
					ChainId:      chainID[:],
					RequestId:    requestID,
					Deadline:     uint64(deadline),
					ContainerIds: containerIDBytes,
				},
			},
		},
		compression.TypeNone,
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
		compression.TypeNone,
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
		compression.TypeNone,
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
		b.compressionType,
		false,
	)
}

func (b *outMsgBuilder) Get(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	containerID ids.ID,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_Get{
				Get: &p2p.Get{
					ChainId:     chainID[:],
					RequestId:   requestID,
					Deadline:    uint64(deadline),
					ContainerId: containerID[:],
				},
			},
		},
		compression.TypeNone,
		false,
	)
}

func (b *outMsgBuilder) Put(
	chainID ids.ID,
	requestID uint32,
	container []byte,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_Put{
				Put: &p2p.Put{
					ChainId:   chainID[:],
					RequestId: requestID,
					Container: container,
				},
			},
		},
		b.compressionType,
		false,
	)
}

func (b *outMsgBuilder) PushQuery(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	container []byte,
	requestedHeight uint64,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_PushQuery{
				PushQuery: &p2p.PushQuery{
					ChainId:         chainID[:],
					RequestId:       requestID,
					Deadline:        uint64(deadline),
					Container:       container,
					RequestedHeight: requestedHeight,
				},
			},
		},
		b.compressionType,
		false,
	)
}

func (b *outMsgBuilder) PullQuery(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	containerID ids.ID,
	requestedHeight uint64,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_PullQuery{
				PullQuery: &p2p.PullQuery{
					ChainId:         chainID[:],
					RequestId:       requestID,
					Deadline:        uint64(deadline),
					ContainerId:     containerID[:],
					RequestedHeight: requestedHeight,
				},
			},
		},
		compression.TypeNone,
		false,
	)
}

func (b *outMsgBuilder) Chits(
	chainID ids.ID,
	requestID uint32,
	preferredID ids.ID,
	preferredIDAtHeight ids.ID,
	acceptedID ids.ID,
	acceptedHeight uint64,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_Chits{
				Chits: &p2p.Chits{
					ChainId:             chainID[:],
					RequestId:           requestID,
					PreferredId:         preferredID[:],
					PreferredIdAtHeight: preferredIDAtHeight[:],
					AcceptedId:          acceptedID[:],
					AcceptedHeight:      acceptedHeight,
				},
			},
		},
		compression.TypeNone,
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
		b.compressionType,
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
		b.compressionType,
		false,
	)
}

func (b *outMsgBuilder) AppError(chainID ids.ID, requestID uint32, errorCode int32, errorMessage string) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_AppError{
				AppError: &p2p.AppError{
					ChainId:      chainID[:],
					RequestId:    requestID,
					ErrorCode:    errorCode,
					ErrorMessage: errorMessage,
				},
			},
		},
		b.compressionType,
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
		b.compressionType,
		false,
	)
}

func (b *outMsgBuilder) SimplexMessage(msg *p2p.Simplex) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_Simplex{
				Simplex: msg,
			},
		},
		b.compressionType,
		false,
	)
}
