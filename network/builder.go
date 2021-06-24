// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
)

// Builder extends a Codec to build messages safely
type Builder struct {
	codec
	// [getByteSlice] must not be nil.
	// [getByteSlice] may return nil.
	// [getByteSlice] must be safe for concurrent access by multiple goroutines.
	getByteSlice func() []byte
}

// GetVersion message
func (b Builder) GetVersion(includeIsCompressedFlag bool) (Msg, error) {
	buf := b.getByteSlice()
	return b.Pack(
		buf,
		GetVersion,
		nil,
		includeIsCompressedFlag,
		includeIsCompressedFlag && canBeCompressed(GetVersion),
	)
}

// Version message
func (b Builder) Version(
	networkID,
	nodeID uint32,
	myTime uint64,
	ip utils.IPDesc,
	myVersion string,
	myVersionTime uint64,
	sig []byte,
	includeIsCompressedFlag bool,
) (Msg, error) {
	buf := b.getByteSlice()
	return b.Pack(
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
		includeIsCompressedFlag && canBeCompressed(Version),
	)
}

// GetPeerList message
func (b Builder) GetPeerList(includeIsCompressedFlag bool) (Msg, error) {
	buf := b.getByteSlice()
	return b.Pack(buf, GetPeerList, nil, includeIsCompressedFlag, false)
}

func (b Builder) PeerList(peers []utils.IPCertDesc, includeIsCompressedFlag bool) (Msg, error) {
	buf := b.getByteSlice()
	return b.Pack(
		buf,
		PeerList,
		map[Field]interface{}{
			SignedPeers: peers,
		},
		includeIsCompressedFlag,
		includeIsCompressedFlag && canBeCompressed(PeerList),
	)
}

// Ping message
func (b Builder) Ping(includeIsCompressedFlag bool) (Msg, error) {
	buf := b.getByteSlice()
	return b.Pack(
		buf,
		Ping,
		nil,
		includeIsCompressedFlag,
		includeIsCompressedFlag && canBeCompressed(Ping),
	)
}

// Pong message
func (b Builder) Pong(includeIsCompressedFlag bool) (Msg, error) {
	buf := b.getByteSlice()
	return b.Pack(
		buf,
		Pong,
		nil,
		includeIsCompressedFlag,
		includeIsCompressedFlag && canBeCompressed(Pong),
	)
}

// GetAcceptedFrontier message
func (b Builder) GetAcceptedFrontier(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	includeIsCompressedFlag bool,
) (Msg, error) {
	buf := b.getByteSlice()
	return b.Pack(
		buf,
		GetAcceptedFrontier,
		map[Field]interface{}{
			ChainID:   chainID[:],
			RequestID: requestID,
			Deadline:  deadline,
		},
		includeIsCompressedFlag,
		includeIsCompressedFlag && canBeCompressed(GetAcceptedFrontier),
	)
}

// AcceptedFrontier message
func (b Builder) AcceptedFrontier(
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
	buf := b.getByteSlice()
	return b.Pack(
		buf,
		AcceptedFrontier,
		map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			ContainerIDs: containerIDBytes,
		},
		includeIsCompressedFlag,
		includeIsCompressedFlag && canBeCompressed(AcceptedFrontier),
	)
}

// GetAccepted message
func (b Builder) GetAccepted(
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
	buf := b.getByteSlice()
	return b.Pack(
		buf,
		GetAccepted,
		map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			Deadline:     deadline,
			ContainerIDs: containerIDBytes,
		},
		includeIsCompressedFlag,
		includeIsCompressedFlag && canBeCompressed(GetAccepted),
	)
}

// Accepted message
func (b Builder) Accepted(
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
	buf := b.getByteSlice()
	return b.Pack(
		buf,
		Accepted,
		map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			ContainerIDs: containerIDBytes,
		},
		includeIsCompressedFlag,
		includeIsCompressedFlag && canBeCompressed(Accepted),
	)
}

// GetAncestors message
func (b Builder) GetAncestors(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	containerID ids.ID,
	includeIsCompressedFlag bool,
) (Msg, error) {
	buf := b.getByteSlice()
	return b.Pack(
		buf,
		GetAncestors,
		map[Field]interface{}{
			ChainID:     chainID[:],
			RequestID:   requestID,
			Deadline:    deadline,
			ContainerID: containerID[:],
		},
		includeIsCompressedFlag,
		includeIsCompressedFlag && canBeCompressed(GetAncestors),
	)
}

// MultiPut message
func (b Builder) MultiPut(
	chainID ids.ID,
	requestID uint32,
	containers [][]byte,
	includeIsCompressedFlag bool,
) (Msg, error) {
	buf := b.getByteSlice()
	return b.Pack(
		buf,
		MultiPut,
		map[Field]interface{}{
			ChainID:             chainID[:],
			RequestID:           requestID,
			MultiContainerBytes: containers,
		},
		includeIsCompressedFlag,
		includeIsCompressedFlag && canBeCompressed(MultiPut),
	)
}

// Get message
func (b Builder) Get(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	containerID ids.ID,
	includeIsCompressedFlag bool,
) (Msg, error) {
	buf := b.getByteSlice()
	return b.Pack(
		buf,
		Get,
		map[Field]interface{}{
			ChainID:     chainID[:],
			RequestID:   requestID,
			Deadline:    deadline,
			ContainerID: containerID[:],
		},
		includeIsCompressedFlag,
		includeIsCompressedFlag && canBeCompressed(Get),
	)
}

// Put message
func (b Builder) Put(
	chainID ids.ID,
	requestID uint32,
	containerID ids.ID,
	container []byte,
	includeIsCompressedFlag bool,
) (Msg, error) {
	buf := b.getByteSlice()
	return b.Pack(
		buf,
		Put,
		map[Field]interface{}{
			ChainID:        chainID[:],
			RequestID:      requestID,
			ContainerID:    containerID[:],
			ContainerBytes: container,
		},
		includeIsCompressedFlag,
		includeIsCompressedFlag && canBeCompressed(Put),
	)
}

// PushQuery message
func (b Builder) PushQuery(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	containerID ids.ID,
	container []byte,
	includeIsCompressedFlag bool,
) (Msg, error) {
	buf := b.getByteSlice()
	return b.Pack(
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
		includeIsCompressedFlag && canBeCompressed(PushQuery),
	)
}

// PullQuery message
func (b Builder) PullQuery(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	containerID ids.ID,
	includeIsCompressedFlag bool,
) (Msg, error) {
	buf := b.getByteSlice()
	return b.Pack(
		buf,
		PullQuery,
		map[Field]interface{}{
			ChainID:     chainID[:],
			RequestID:   requestID,
			Deadline:    deadline,
			ContainerID: containerID[:],
		},
		includeIsCompressedFlag,
		includeIsCompressedFlag && canBeCompressed(PullQuery),
	)
}

// Chits message
func (b Builder) Chits(
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
	buf := b.getByteSlice()
	return b.Pack(
		buf,
		Chits,
		map[Field]interface{}{
			ChainID:      chainID[:],
			RequestID:    requestID,
			ContainerIDs: containerIDBytes,
		},
		includeIsCompressedFlag,
		includeIsCompressedFlag && canBeCompressed(Chits),
	)
}

// Returns whether we should compress a message of the given type.
// (Assuming the peer can handle compressed messages)
func canBeCompressed(op Op) bool {
	switch op {
	case PushQuery, Put, MultiPut, PeerList:
		return true
	default:
		return false
	}
}
