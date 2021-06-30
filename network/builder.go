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
func (b Builder) GetVersion() (Msg, error) {
	buf := b.getByteSlice()
	return b.Pack(
		buf,
		GetVersion,
		nil,
		false, // GetVersion messages can't be compressed
		false, // GetVersion messages can't be compressed
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
		false, // Version Messages can't be compressed
		false, // Version Messages can't be compressed
	)
}

// GetPeerList message
func (b Builder) GetPeerList() (Msg, error) {
	buf := b.getByteSlice()
	// GetPeerList messages can't be compressed
	return b.Pack(buf, GetPeerList, nil, false, false)
}

func (b Builder) PeerList(peers []utils.IPCertDesc, includeIsCompressedFlag, compress bool) (Msg, error) {
	buf := b.getByteSlice()
	return b.Pack(
		buf,
		PeerList,
		map[Field]interface{}{
			SignedPeers: peers,
		},
		includeIsCompressedFlag, // PeerList messages may be compressed
		compress,
	)
}

// Ping message
func (b Builder) Ping() (Msg, error) {
	buf := b.getByteSlice()
	return b.Pack(
		buf,
		Ping,
		nil,
		false, // Ping messages can't be compressed
		false,
	)
}

// Pong message
func (b Builder) Pong() (Msg, error) {
	buf := b.getByteSlice()
	return b.Pack(
		buf,
		Pong,
		nil,
		false, // Ping messages can't be compressed
		false,
	)
}

// GetAcceptedFrontier message
func (b Builder) GetAcceptedFrontier(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
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
		false, // GetAcceptedFrontier messages can't be compressed
		false,
	)
}

// AcceptedFrontier message
func (b Builder) AcceptedFrontier(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
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
		false, // AcceptedFrontier messages can't be compressed
		false,
	)
}

// GetAccepted message
func (b Builder) GetAccepted(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	containerIDs []ids.ID,
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
		false, // GetAccepted messages can't be compressed
		false,
	)
}

// Accepted message
func (b Builder) Accepted(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
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
		false, // Accepted messages can't be compressed
		false,
	)
}

// GetAncestors message
func (b Builder) GetAncestors(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	containerID ids.ID,
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
		false, // GetAncestors messages can't be compressed
		false,
	)
}

// MultiPut message
func (b Builder) MultiPut(
	chainID ids.ID,
	requestID uint32,
	containers [][]byte,
	includeIsCompressedFlag bool,
	compressed bool,
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
		compressed,
	)
}

// Get message
func (b Builder) Get(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	containerID ids.ID,
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
		false, // Get messages can't be compressed
		false,
	)
}

// Put message
func (b Builder) Put(
	chainID ids.ID,
	requestID uint32,
	containerID ids.ID,
	container []byte,
	includeIsCompressedFlag bool,
	compress bool,
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
		compress,
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
	compress bool,
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
		compress,
	)
}

// PullQuery message
func (b Builder) PullQuery(
	chainID ids.ID,
	requestID uint32,
	deadline uint64,
	containerID ids.ID,
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
		false, // PullQuery messages can't be compressed
		false,
	)
}

// Chits message
func (b Builder) Chits(
	chainID ids.ID,
	requestID uint32,
	containerIDs []ids.ID,
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
		false, // Chits messages can't be compressed
		false,
	)
}
