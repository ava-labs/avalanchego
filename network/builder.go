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
		false,
		false,
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
		includeIsCompressedFlag && Version.canBeCompressed(),
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
		includeIsCompressedFlag && PeerList.canBeCompressed(),
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
		includeIsCompressedFlag && Ping.canBeCompressed(),
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
		includeIsCompressedFlag && Pong.canBeCompressed(),
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
		includeIsCompressedFlag && GetAcceptedFrontier.canBeCompressed(),
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
		includeIsCompressedFlag && AcceptedFrontier.canBeCompressed(),
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
		includeIsCompressedFlag && GetAccepted.canBeCompressed(),
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
		includeIsCompressedFlag && Accepted.canBeCompressed(),
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
		includeIsCompressedFlag && GetAncestors.canBeCompressed(),
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
		includeIsCompressedFlag && MultiPut.canBeCompressed(),
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
		includeIsCompressedFlag && Get.canBeCompressed(),
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
		includeIsCompressedFlag && Put.canBeCompressed(),
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
		includeIsCompressedFlag && PushQuery.canBeCompressed(),
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
		includeIsCompressedFlag && PullQuery.canBeCompressed(),
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
		includeIsCompressedFlag && Chits.canBeCompressed(),
	)
}
