// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/ips"

	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
)

var _ OutboundMsgBuilder = (*outMsgBuilder)(nil)

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
	return b.builder.createOutbound(
		Version,
		&p2ppb.Message{
			Message: &p2ppb.Message_Version{
				Version: &p2ppb.Version{
					NetworkId:      networkID,
					MyTime:         myTime,
					IpAddr:         []byte(ip.IP.To16()), // ref. "wrappers.TryPackIP"
					IpPort:         uint32(ip.Port),
					MyVersion:      myVersion,
					MyVersionTime:  myVersionTime,
					Sig:            sig,
					TrackedSubnets: subnetIDBytes,
				},
			},
		},
		b.compress && Version.Compressible(),
		true,
	)
}

func (b *outMsgBuilder) PeerList(peers []ips.ClaimedIPPort, bypassThrottling bool) (OutboundMessage, error) {
	claimIPPorts := make([]*p2ppb.ClaimedIpPort, len(peers))
	for i, p := range peers {
		claimIPPorts[i] = &p2ppb.ClaimedIpPort{
			// the inbound message parser will call "x509.ParseCertificate(p.X509Certificate)"
			// to decode this message
			X509Certificate: p.Cert.Raw,
			IpAddr:          []byte(p.IPPort.IP.To16()),
			IpPort:          uint32(p.IPPort.Port),
			Timestamp:       p.Timestamp,
			Signature:       p.Signature,
		}
	}
	return b.builder.createOutbound(
		PeerList,
		&p2ppb.Message{
			Message: &p2ppb.Message_PeerList{
				PeerList: &p2ppb.PeerList{
					ClaimedIpPorts: claimIPPorts,
				},
			},
		},
		b.compress && PeerList.Compressible(),
		bypassThrottling,
	)
}

func (b *outMsgBuilder) Ping() (OutboundMessage, error) {
	return b.builder.createOutbound(
		Ping,
		&p2ppb.Message{
			Message: &p2ppb.Message_Ping{
				Ping: &p2ppb.Ping{},
			},
		},
		b.compress && Ping.Compressible(),
		false,
	)
}

func (b *outMsgBuilder) Pong(uptimePercentage uint8) (OutboundMessage, error) {
	return b.builder.createOutbound(
		Pong,
		&p2ppb.Message{
			Message: &p2ppb.Message_Pong{
				Pong: &p2ppb.Pong{
					UptimePct: uint32(uptimePercentage),
				},
			},
		},
		b.compress && Pong.Compressible(),
		false,
	)
}

func (b *outMsgBuilder) GetStateSummaryFrontier(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		GetStateSummaryFrontier,
		&p2ppb.Message{
			Message: &p2ppb.Message_GetStateSummaryFrontier{
				GetStateSummaryFrontier: &p2ppb.GetStateSummaryFrontier{
					ChainId:   chainID[:],
					RequestId: requestID,
					Deadline:  uint64(deadline),
				},
			},
		},
		b.compress && GetStateSummaryFrontier.Compressible(),
		false,
	)
}

func (b *outMsgBuilder) StateSummaryFrontier(
	chainID ids.ID,
	requestID uint32,
	summary []byte,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		StateSummaryFrontier,
		&p2ppb.Message{
			Message: &p2ppb.Message_StateSummaryFrontier_{
				StateSummaryFrontier_: &p2ppb.StateSummaryFrontier{
					ChainId:   chainID[:],
					RequestId: requestID,
					Summary:   summary,
				},
			},
		},
		b.compress && StateSummaryFrontier.Compressible(),
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
		GetAcceptedStateSummary,
		&p2ppb.Message{
			Message: &p2ppb.Message_GetAcceptedStateSummary{
				GetAcceptedStateSummary: &p2ppb.GetAcceptedStateSummary{
					ChainId:   chainID[:],
					RequestId: requestID,
					Deadline:  uint64(deadline),
					Heights:   heights,
				},
			},
		},
		b.compress && GetAcceptedStateSummary.Compressible(),
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
		AcceptedStateSummary,
		&p2ppb.Message{
			Message: &p2ppb.Message_AcceptedStateSummary_{
				AcceptedStateSummary_: &p2ppb.AcceptedStateSummary{
					ChainId:    chainID[:],
					RequestId:  requestID,
					SummaryIds: summaryIDBytes,
				},
			},
		},
		b.compress && AcceptedStateSummary.Compressible(),
		false,
	)
}

func (b *outMsgBuilder) GetAcceptedFrontier(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		GetAcceptedFrontier,
		&p2ppb.Message{
			Message: &p2ppb.Message_GetAcceptedFrontier{
				GetAcceptedFrontier: &p2ppb.GetAcceptedFrontier{
					ChainId:   chainID[:],
					RequestId: requestID,
					Deadline:  uint64(deadline),
				},
			},
		},
		b.compress && GetAcceptedFrontier.Compressible(),
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
		AcceptedFrontier,
		&p2ppb.Message{
			Message: &p2ppb.Message_AcceptedFrontier_{
				AcceptedFrontier_: &p2ppb.AcceptedFrontier{
					ChainId:      chainID[:],
					RequestId:    requestID,
					ContainerIds: containerIDBytes,
				},
			},
		},
		b.compress && AcceptedFrontier.Compressible(),
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
		GetAccepted,
		&p2ppb.Message{
			Message: &p2ppb.Message_GetAccepted{
				GetAccepted: &p2ppb.GetAccepted{
					ChainId:      chainID[:],
					RequestId:    requestID,
					Deadline:     uint64(deadline),
					ContainerIds: containerIDBytes,
				},
			},
		},
		b.compress && GetAccepted.Compressible(),
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
		Accepted,
		&p2ppb.Message{
			Message: &p2ppb.Message_Accepted_{
				Accepted_: &p2ppb.Accepted{
					ChainId:      chainID[:],
					RequestId:    requestID,
					ContainerIds: containerIDBytes,
				},
			},
		},
		b.compress && Accepted.Compressible(),
		false,
	)
}

func (b *outMsgBuilder) GetAncestors(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	containerID ids.ID,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		GetAncestors,
		&p2ppb.Message{
			Message: &p2ppb.Message_GetAncestors{
				GetAncestors: &p2ppb.GetAncestors{
					ChainId:     chainID[:],
					RequestId:   requestID,
					Deadline:    uint64(deadline),
					ContainerId: containerID[:],
				},
			},
		},
		b.compress && GetAncestors.Compressible(),
		false,
	)
}

func (b *outMsgBuilder) Ancestors(
	chainID ids.ID,
	requestID uint32,
	containers [][]byte,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		Ancestors,
		&p2ppb.Message{
			Message: &p2ppb.Message_Ancestors_{
				Ancestors_: &p2ppb.Ancestors{
					ChainId:    chainID[:],
					RequestId:  requestID,
					Containers: containers,
				},
			},
		},
		b.compress && Ancestors.Compressible(),
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
		Get,
		&p2ppb.Message{
			Message: &p2ppb.Message_Get{
				Get: &p2ppb.Get{
					ChainId:     chainID[:],
					RequestId:   requestID,
					Deadline:    uint64(deadline),
					ContainerId: containerID[:],
				},
			},
		},
		b.compress && Get.Compressible(),
		false,
	)
}

func (b *outMsgBuilder) Put(
	chainID ids.ID,
	requestID uint32,
	container []byte,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		Put,
		&p2ppb.Message{
			Message: &p2ppb.Message_Put{
				Put: &p2ppb.Put{
					ChainId:   chainID[:],
					RequestId: requestID,
					Container: container,
				},
			},
		},
		b.compress && Put.Compressible(),
		false,
	)
}

func (b *outMsgBuilder) PushQuery(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	container []byte,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		PushQuery,
		&p2ppb.Message{
			Message: &p2ppb.Message_PushQuery{
				PushQuery: &p2ppb.PushQuery{
					ChainId:   chainID[:],
					RequestId: requestID,
					Deadline:  uint64(deadline),
					Container: container,
				},
			},
		},
		b.compress && PushQuery.Compressible(),
		false,
	)
}

func (b *outMsgBuilder) PullQuery(
	chainID ids.ID,
	requestID uint32,
	deadline time.Duration,
	containerID ids.ID,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		PullQuery,
		&p2ppb.Message{
			Message: &p2ppb.Message_PullQuery{
				PullQuery: &p2ppb.PullQuery{
					ChainId:     chainID[:],
					RequestId:   requestID,
					Deadline:    uint64(deadline),
					ContainerId: containerID[:],
				},
			},
		},
		b.compress && PullQuery.Compressible(),
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
	return b.builder.createOutbound(
		Chits,
		&p2ppb.Message{
			Message: &p2ppb.Message_Chits{
				Chits: &p2ppb.Chits{
					ChainId:      chainID[:],
					RequestId:    requestID,
					ContainerIds: containerIDBytes,
				},
			},
		},
		b.compress && Chits.Compressible(),
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
	return b.builder.createOutbound(
		AppRequest,
		&p2ppb.Message{
			Message: &p2ppb.Message_AppRequest{
				AppRequest: &p2ppb.AppRequest{
					ChainId:   chainID[:],
					RequestId: requestID,
					Deadline:  uint64(deadline),
					AppBytes:  msg,
				},
			},
		},
		b.compress && AppRequest.Compressible(),
		false,
	)
}

// Application level response
func (b *outMsgBuilder) AppResponse(chainID ids.ID, requestID uint32, msg []byte) (OutboundMessage, error) {
	return b.builder.createOutbound(
		AppResponse,
		&p2ppb.Message{
			Message: &p2ppb.Message_AppResponse{
				AppResponse: &p2ppb.AppResponse{
					ChainId:   chainID[:],
					RequestId: requestID,
					AppBytes:  msg,
				},
			},
		},
		b.compress && AppResponse.Compressible(),
		false,
	)
}

// Application level gossiped message
func (b *outMsgBuilder) AppGossip(chainID ids.ID, msg []byte) (OutboundMessage, error) {
	return b.builder.createOutbound(
		AppGossip,
		&p2ppb.Message{
			Message: &p2ppb.Message_AppGossip{
				AppGossip: &p2ppb.AppGossip{
					ChainId:  chainID[:],
					AppBytes: msg,
				},
			},
		},
		b.compress && AppGossip.Compressible(),
		false,
	)
}
