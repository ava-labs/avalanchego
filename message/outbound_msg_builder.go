// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
)

var _ OutboundMsgBuilder = &outMsgBuilder{}

type OutboundMsgBuilder interface {
	GetVersion() (OutboundMessage, error)

	Version(
		networkID,
		nodeID uint32,
		myTime uint64,
		ip utils.IPDesc,
		myVersion string,
		myVersionTime uint64,
		sig []byte,
		trackedSubnets []ids.ID,
	) (OutboundMessage, error)

	GetPeerList() (OutboundMessage, error)

	PeerList(peers []utils.IPCertDesc) (OutboundMessage, error)

	Ping() (OutboundMessage, error)

	Pong() (OutboundMessage, error)

	GetAcceptedFrontier(
		chainID ids.ID,
		requestID uint32,
		deadline uint64,
	) (OutboundMessage, error)

	AcceptedFrontier(
		chainID ids.ID,
		requestID uint32,
		containerIDs []ids.ID,
	) (OutboundMessage, error)

	GetAccepted(
		chainID ids.ID,
		requestID uint32,
		deadline uint64,
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
		deadline uint64,
		containerID ids.ID,
	) (OutboundMessage, error)

	MultiPut(
		chainID ids.ID,
		requestID uint32,
		containers [][]byte,
	) (OutboundMessage, error)

	Get(
		chainID ids.ID,
		requestID uint32,
		deadline uint64,
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
		deadline uint64,
		containerID ids.ID,
		container []byte,
	) (OutboundMessage, error)

	PullQuery(
		chainID ids.ID,
		requestID uint32,
		deadline uint64,
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
		deadline uint64,
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

	// When you create a message with the message creator, that message can be used until
	// ReturnBytes is called, after which point you may no longer use that message.
	ReturnBytes(msg []byte)
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

func (b *outMsgBuilder) GetVersion() (OutboundMessage, error) {
	return b.c.Pack(
		GetVersion,
		nil,
		GetVersion.Compressable(), // GetVersion messages can't be compressed
	)
}

func (b *outMsgBuilder) Version(
	networkID,
	nodeID uint32,
	myTime uint64,
	ip utils.IPDesc,
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
			NodeID:         nodeID,
			MyTime:         myTime,
			IP:             ip,
			VersionStr:     myVersion,
			VersionTime:    myVersionTime,
			SigBytes:       sig,
			TrackedSubnets: subnetIDBytes,
		},
		Version.Compressable(), // Version Messages can't be compressed
	)
}

func (b *outMsgBuilder) GetPeerList() (OutboundMessage, error) {
	return b.c.Pack(
		GetPeerList,
		nil,
		GetPeerList.Compressable(), // GetPeerList messages can't be compressed
	)
}

func (b *outMsgBuilder) PeerList(peers []utils.IPCertDesc) (OutboundMessage, error) {
	return b.c.Pack(
		PeerList,
		map[Field]interface{}{
			SignedPeers: peers,
		},
		b.compress && PeerList.Compressable(), // PeerList messages may be compressed
	)
}

func (b *outMsgBuilder) Ping() (OutboundMessage, error) {
	return b.c.Pack(
		Ping,
		nil,
		Ping.Compressable(), // Ping messages can't be compressed
	)
}

func (b *outMsgBuilder) Pong() (OutboundMessage, error) {
	return b.c.Pack(
		Pong,
		nil,
		Pong.Compressable(), // Pong messages can't be compressed
	)
}

func (b *outMsgBuilder) GetAcceptedFrontier(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
) (OutboundMessage, error) {
	return b.c.Pack(
		GetAcceptedFrontier,
		map[Field]interface{}{
			ChainID:   chainID[:],
			RequestID: requestID,
			Deadline:  deadline,
		},
		GetAcceptedFrontier.Compressable(), // GetAcceptedFrontier messages can't be compressed
	)
}

func (b *outMsgBuilder) AcceptedFrontier(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
) (OutboundMessage, error) {
	containerIDBytes := make([][]byte, len(containerIDs))
	for i, containerID := range containerIDs {
		copy := containerID
		containerIDBytes[i] = copy[:]
	}
	return b.c.Pack(
		AcceptedFrontier,
		map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			ContainerIDs: containerIDBytes,
		},
		AcceptedFrontier.Compressable(), // AcceptedFrontier messages can't be compressed
	)
}

func (b *outMsgBuilder) GetAccepted(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	containerIDs []ids.ID,
) (OutboundMessage, error) {
	containerIDBytes := make([][]byte, len(containerIDs))
	for i, containerID := range containerIDs {
		copy := containerID
		containerIDBytes[i] = copy[:]
	}
	return b.c.Pack(
		GetAccepted,
		map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			Deadline:     deadline,
			ContainerIDs: containerIDBytes,
		},
		GetAccepted.Compressable(), // GetAccepted messages can't be compressed
	)
}

func (b *outMsgBuilder) Accepted(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
) (OutboundMessage, error) {
	containerIDBytes := make([][]byte, len(containerIDs))
	for i, containerID := range containerIDs {
		copy := containerID
		containerIDBytes[i] = copy[:]
	}
	return b.c.Pack(
		Accepted,
		map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			ContainerIDs: containerIDBytes,
		},
		Accepted.Compressable(), // Accepted messages can't be compressed
	)
}

func (b *outMsgBuilder) GetAncestors(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	containerID ids.ID,
) (OutboundMessage, error) {
	return b.c.Pack(
		GetAncestors,
		map[Field]interface{}{
			ChainID:     chainID[:],
			RequestID:   requestID,
			Deadline:    deadline,
			ContainerID: containerID[:],
		},
		GetAncestors.Compressable(), // GetAncestors messages can't be compressed
	)
}

func (b *outMsgBuilder) MultiPut(
	chainID ids.ID,
	requestID uint32,
	containers [][]byte,
) (OutboundMessage, error) {
	return b.c.Pack(
		MultiPut,
		map[Field]interface{}{
			ChainID:             chainID[:],
			RequestID:           requestID,
			MultiContainerBytes: containers,
		},
		b.compress && MultiPut.Compressable(), // MultiPut messages may be compressed
	)
}

func (b *outMsgBuilder) Get(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	containerID ids.ID,
) (OutboundMessage, error) {
	return b.c.Pack(
		Get,
		map[Field]interface{}{
			ChainID:     chainID[:],
			RequestID:   requestID,
			Deadline:    deadline,
			ContainerID: containerID[:],
		},
		Get.Compressable(), // Get messages can't be compressed
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
		b.compress && Put.Compressable(), // Put messages may be compressed
	)
}

func (b *outMsgBuilder) PushQuery(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	containerID ids.ID,
	container []byte,
) (OutboundMessage, error) {
	return b.c.Pack(
		PushQuery,
		map[Field]interface{}{
			ChainID:        chainID[:],
			RequestID:      requestID,
			Deadline:       deadline,
			ContainerID:    containerID[:],
			ContainerBytes: container,
		},
		b.compress && PushQuery.Compressable(), // PushQuery messages may be compressed
	)
}

func (b *outMsgBuilder) PullQuery(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	containerID ids.ID,
) (OutboundMessage, error) {
	return b.c.Pack(
		PullQuery,
		map[Field]interface{}{
			ChainID:     chainID[:],
			RequestID:   requestID,
			Deadline:    deadline,
			ContainerID: containerID[:],
		},
		PullQuery.Compressable(), // PullQuery messages can't be compressed
	)
}

func (b *outMsgBuilder) Chits(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
) (OutboundMessage, error) {
	containerIDBytes := make([][]byte, len(containerIDs))
	for i, containerID := range containerIDs {
		copy := containerID
		containerIDBytes[i] = copy[:]
	}
	return b.c.Pack(
		Chits,
		map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			ContainerIDs: containerIDBytes,
		},
		Chits.Compressable(), // Chits messages can't be compressed
	)
}

// Application level request
func (b *outMsgBuilder) AppRequest(chainID ids.ID, requestID uint32, deadline uint64, msg []byte) (OutboundMessage, error) {
	return b.c.Pack(
		AppRequest,
		map[Field]interface{}{
			ChainID:         chainID[:],
			RequestID:       requestID,
			Deadline:        deadline,
			AppRequestBytes: msg,
		},
		b.compress && AppRequest.Compressable(), // App messages may be compressed
	)
}

// Application level response
func (b *outMsgBuilder) AppResponse(chainID ids.ID, requestID uint32, msg []byte) (OutboundMessage, error) {
	return b.c.Pack(
		AppResponse,
		map[Field]interface{}{
			ChainID:          chainID[:],
			RequestID:        requestID,
			AppResponseBytes: msg,
		},
		b.compress && AppResponse.Compressable(), // App messages may be compressed
	)
}

// Application level gossiped message
func (b *outMsgBuilder) AppGossip(chainID ids.ID, msg []byte) (OutboundMessage, error) {
	return b.c.Pack(
		AppGossip,
		map[Field]interface{}{
			ChainID:        chainID[:],
			AppGossipBytes: msg,
		},
		b.compress && AppGossip.Compressable(), // App messages may be compressed
	)
}

func (b *outMsgBuilder) ReturnBytes(msg []byte) {
	b.c.ReturnBytes(msg)
}
