// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
)

// Builder extends a Codec to build messages safely
type Builder struct {
	Codec
	// [getByteSlice] must not be nil.
	// [getByteSlice] may return nil.
	// [getByteSlice] must be safe for concurrent access by multiple goroutines.
	getByteSlice func() []byte
}

// GetVersion message
func (m Builder) GetVersion(includeIsCompressedFlag bool) (Msg, error) {
	buf := m.getByteSlice()
	return m.Pack(buf, GetVersion, nil, includeIsCompressedFlag, false)
}

// Version message
func (m Builder) Version(
	networkID,
	nodeID uint32,
	myTime uint64,
	ip utils.IPDesc,
	myVersion string,
	myVersionTime uint64,
	sig []byte,
	includeIsCompressedFlag bool,
) (Msg, error) {
	buf := m.getByteSlice()
	return m.Pack(
		buf,
		Version,
		map[Field]interface{}{
			NetworkID:   networkID,
			NodeID:      nodeID,
			MyTime:      myTime,
			IP:          ip,
			VersionStr:  myVersion,
			VersionTime: myVersionTime,
			SigBytes:    sig,
		},
		includeIsCompressedFlag,
		false,
	)
}

// GetPeerList message
func (m Builder) GetPeerList(includeIsCompressedFlag bool) (Msg, error) {
	buf := m.getByteSlice()
	return m.Pack(buf, GetPeerList, nil, includeIsCompressedFlag, false)
}

func (m Builder) PeerList(peers []utils.IPCertDesc, includeIsCompressedFlag, compress bool) (Msg, error) {
	buf := m.getByteSlice()
	return m.Pack(
		buf,
		PeerList,
		map[Field]interface{}{
			SignedPeers: peers,
		},
		includeIsCompressedFlag,
		compress,
	)
}

// Ping message
func (m Builder) Ping(includeIsCompressedFlag bool) (Msg, error) {
	buf := m.getByteSlice()
	return m.Pack(buf, Ping, nil, includeIsCompressedFlag, false)
}

// Pong message
func (m Builder) Pong(includeIsCompressedFlag bool) (Msg, error) {
	buf := m.getByteSlice()
	return m.Pack(buf, Pong, nil, includeIsCompressedFlag, false)
}

// GetAcceptedFrontier message
func (m Builder) GetAcceptedFrontier(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	includeIsCompressedFlag bool,
) (Msg, error) {
	buf := m.getByteSlice()
	return m.Pack(
		buf,
		GetAcceptedFrontier,
		map[Field]interface{}{
			ChainID:   chainID[:],
			RequestID: requestID,
			Deadline:  deadline,
		},
		includeIsCompressedFlag,
		false,
	)
}

// AcceptedFrontier message
func (m Builder) AcceptedFrontier(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
	includeIsCompressedFlag bool,
	compress bool,
) (Msg, error) {
	containerIDBytes := make([][]byte, len(containerIDs))
	for i, containerID := range containerIDs {
		copy := containerID
		containerIDBytes[i] = copy[:]
	}
	buf := m.getByteSlice()
	return m.Pack(
		buf,
		AcceptedFrontier,
		map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			ContainerIDs: containerIDBytes,
		},
		includeIsCompressedFlag,
		compress,
	)
}

// GetAccepted message
func (m Builder) GetAccepted(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	containerIDs []ids.ID,
	includeIsCompressedFlag bool,
) (Msg, error) {
	containerIDBytes := make([][]byte, len(containerIDs))
	for i, containerID := range containerIDs {
		copy := containerID
		containerIDBytes[i] = copy[:]
	}
	buf := m.getByteSlice()
	return m.Pack(
		buf,
		GetAccepted,
		map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			Deadline:     deadline,
			ContainerIDs: containerIDBytes,
		},
		includeIsCompressedFlag,
		false,
	)
}

// Accepted message
func (m Builder) Accepted(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
	includeIsCompressedFlag bool,
) (Msg, error) {
	containerIDBytes := make([][]byte, len(containerIDs))
	for i, containerID := range containerIDs {
		copy := containerID
		containerIDBytes[i] = copy[:]
	}
	buf := m.getByteSlice()
	return m.Pack(
		buf,
		Accepted,
		map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			ContainerIDs: containerIDBytes,
		},
		includeIsCompressedFlag,
		false,
	)
}

// GetAncestors message
func (m Builder) GetAncestors(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	containerID ids.ID,
	includeIsCompressedFlag bool,
) (Msg, error) {
	buf := m.getByteSlice()
	return m.Pack(
		buf,
		GetAncestors,
		map[Field]interface{}{
			ChainID:     chainID[:],
			RequestID:   requestID,
			Deadline:    deadline,
			ContainerID: containerID[:],
		},
		includeIsCompressedFlag,
		false,
	)
}

// MultiPut message
func (m Builder) MultiPut(
	chainID ids.ID,
	requestID uint32,
	containers [][]byte,
	includeIsCompressedFlag bool,
	compress bool,
) (Msg, error) {
	buf := m.getByteSlice()
	return m.Pack(
		buf,
		MultiPut,
		map[Field]interface{}{
			ChainID:             chainID[:],
			RequestID:           requestID,
			MultiContainerBytes: containers,
		},
		includeIsCompressedFlag,
		compress,
	)
}

// Get message
func (m Builder) Get(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	containerID ids.ID,
	includeIsCompressedFlag bool,
) (Msg, error) {
	buf := m.getByteSlice()
	return m.Pack(
		buf,
		Get,
		map[Field]interface{}{
			ChainID:     chainID[:],
			RequestID:   requestID,
			Deadline:    deadline,
			ContainerID: containerID[:],
		},
		includeIsCompressedFlag,
		false,
	)
}

// Put message
func (m Builder) Put(
	chainID ids.ID,
	requestID uint32,
	containerID ids.ID,
	container []byte,
	includeIsCompressedFlag bool,
	compress bool,
) (Msg, error) {
	buf := m.getByteSlice()
	return m.Pack(
		buf,
		Put,
		map[Field]interface{}{
			ChainID:        chainID[:],
			RequestID:      requestID,
			ContainerID:    containerID[:],
			ContainerBytes: container,
		},
		includeIsCompressedFlag,
		compress,
	)
}

// PushQuery message
func (m Builder) PushQuery(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	containerID ids.ID,
	container []byte,
	includeIsCompressedFlag bool,
	compress bool,
) (Msg, error) {
	buf := m.getByteSlice()
	return m.Pack(
		buf,
		PushQuery,
		map[Field]interface{}{
			ChainID:        chainID[:],
			RequestID:      requestID,
			Deadline:       deadline,
			ContainerID:    containerID[:],
			ContainerBytes: container,
		},
		includeIsCompressedFlag,
		compress,
	)
}

// PullQuery message
func (m Builder) PullQuery(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	containerID ids.ID,
	includeIsCompressedFlag bool,
) (Msg, error) {
	buf := m.getByteSlice()
	return m.Pack(
		buf,
		PullQuery,
		map[Field]interface{}{
			ChainID:     chainID[:],
			RequestID:   requestID,
			Deadline:    deadline,
			ContainerID: containerID[:],
		},
		includeIsCompressedFlag,
		false,
	)
}

// Chits message
func (m Builder) Chits(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
	includeIsCompressedFlag bool,
	compress bool,
) (Msg, error) {
	containerIDBytes := make([][]byte, len(containerIDs))
	for i, containerID := range containerIDs {
		copy := containerID
		containerIDBytes[i] = copy[:]
	}
	buf := m.getByteSlice()
	return m.Pack(
		buf,
		Chits,
		map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			ContainerIDs: containerIDBytes,
		},
		includeIsCompressedFlag,
		compress,
	)
}
