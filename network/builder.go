// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
)

// Builder extends a Codec to build messages safely
type Builder struct{ Codec }

// GetVersion message
func (m Builder) GetVersion() (Msg, error) { return m.Pack(GetVersion, nil) }

// Version message
func (m Builder) Version(networkID, nodeID uint32, myTime uint64, ip utils.IPDesc, myVersion string) (Msg, error) {
	return m.Pack(Version, map[Field]interface{}{
		NetworkID:  networkID,
		NodeID:     nodeID,
		MyTime:     myTime,
		IP:         ip,
		VersionStr: myVersion,
	})
}

// GetPeerList message
func (m Builder) GetPeerList() (Msg, error) { return m.Pack(GetPeerList, nil) }

// PeerList message
func (m Builder) PeerList(ipDescs []utils.IPDesc) (Msg, error) {
	return m.Pack(PeerList, map[Field]interface{}{Peers: ipDescs})
}

// Ping message
func (m Builder) Ping() (Msg, error) { return m.Pack(Ping, nil) }

// Pong message
func (m Builder) Pong() (Msg, error) { return m.Pack(Pong, nil) }

// GetAcceptedFrontier message
func (m Builder) GetAcceptedFrontier(chainID ids.ID, requestID uint32, deadline uint64) (Msg, error) {
	return m.Pack(GetAcceptedFrontier, map[Field]interface{}{
		ChainID:   chainID[:],
		RequestID: requestID,
		Deadline:  deadline,
	})
}

// AcceptedFrontier message
func (m Builder) AcceptedFrontier(chainID ids.ID, requestID uint32, containerIDs []ids.ID) (Msg, error) {
	containerIDBytes := make([][]byte, len(containerIDs))
	for i, containerID := range containerIDs {
		copy := containerID
		containerIDBytes[i] = copy[:]
	}
	return m.Pack(AcceptedFrontier, map[Field]interface{}{
		ChainID:      chainID[:],
		RequestID:    requestID,
		ContainerIDs: containerIDBytes,
	})
}

// GetAccepted message
func (m Builder) GetAccepted(chainID ids.ID, requestID uint32, deadline uint64, containerIDs []ids.ID) (Msg, error) {
	containerIDBytes := make([][]byte, len(containerIDs))
	for i, containerID := range containerIDs {
		copy := containerID
		containerIDBytes[i] = copy[:]
	}
	return m.Pack(GetAccepted, map[Field]interface{}{
		ChainID:      chainID[:],
		RequestID:    requestID,
		Deadline:     deadline,
		ContainerIDs: containerIDBytes,
	})
}

// Accepted message
func (m Builder) Accepted(chainID ids.ID, requestID uint32, containerIDs []ids.ID) (Msg, error) {
	containerIDBytes := make([][]byte, len(containerIDs))
	for i, containerID := range containerIDs {
		copy := containerID
		containerIDBytes[i] = copy[:]
	}
	return m.Pack(Accepted, map[Field]interface{}{
		ChainID:      chainID[:],
		RequestID:    requestID,
		ContainerIDs: containerIDBytes,
	})
}

// GetAncestors message
func (m Builder) GetAncestors(chainID ids.ID, requestID uint32, deadline uint64, containerID ids.ID) (Msg, error) {
	return m.Pack(GetAncestors, map[Field]interface{}{
		ChainID:     chainID[:],
		RequestID:   requestID,
		Deadline:    deadline,
		ContainerID: containerID[:],
	})
}

// MultiPut message
func (m Builder) MultiPut(chainID ids.ID, requestID uint32, containers [][]byte) (Msg, error) {
	return m.Pack(MultiPut, map[Field]interface{}{
		ChainID:             chainID[:],
		RequestID:           requestID,
		MultiContainerBytes: containers,
	})
}

// Get message
func (m Builder) Get(chainID ids.ID, requestID uint32, deadline uint64, containerID ids.ID) (Msg, error) {
	return m.Pack(Get, map[Field]interface{}{
		ChainID:     chainID[:],
		RequestID:   requestID,
		Deadline:    deadline,
		ContainerID: containerID[:],
	})
}

// Put message
func (m Builder) Put(chainID ids.ID, requestID uint32, containerID ids.ID, container []byte) (Msg, error) {
	return m.Pack(Put, map[Field]interface{}{
		ChainID:        chainID[:],
		RequestID:      requestID,
		ContainerID:    containerID[:],
		ContainerBytes: container,
	})
}

// PushQuery message
func (m Builder) PushQuery(chainID ids.ID, requestID uint32, deadline uint64, containerID ids.ID, container []byte) (Msg, error) {
	return m.Pack(PushQuery, map[Field]interface{}{
		ChainID:        chainID[:],
		RequestID:      requestID,
		Deadline:       deadline,
		ContainerID:    containerID[:],
		ContainerBytes: container,
	})
}

// PullQuery message
func (m Builder) PullQuery(chainID ids.ID, requestID uint32, deadline uint64, containerID ids.ID) (Msg, error) {
	return m.Pack(PullQuery, map[Field]interface{}{
		ChainID:     chainID[:],
		RequestID:   requestID,
		Deadline:    deadline,
		ContainerID: containerID[:],
	})
}

// Chits message
func (m Builder) Chits(chainID ids.ID, requestID uint32, containerIDs []ids.ID) (Msg, error) {
	containerIDBytes := make([][]byte, len(containerIDs))
	for i, containerID := range containerIDs {
		copy := containerID
		containerIDBytes[i] = copy[:]
	}
	return m.Pack(Chits, map[Field]interface{}{
		ChainID:      chainID[:],
		RequestID:    requestID,
		ContainerIDs: containerIDBytes,
	})
}

// Application level messgae
func (m Builder) AppMsg(chainID ids.ID, msg []byte) (Msg, error) {
	return m.Pack(AppMsg, map[Field]interface{}{
		ChainID:     chainID[:],
		AppMsgBytes: msg,
	})
}
