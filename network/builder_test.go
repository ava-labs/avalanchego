// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"net"
	"testing"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils"
	"github.com/stretchr/testify/assert"
)

var (
	TestBuilder Builder
)

func TestBuildGetVersion(t *testing.T) {
	msg, err := TestBuilder.GetVersion()
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, GetVersion, msg.Op())

	parsedMsg, err := TestBuilder.Parse(msg.Bytes())
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, GetVersion, parsedMsg.Op())
}

func TestBuildVersion(t *testing.T) {
	networkID := uint32(1)
	nodeID := uint32(3)
	myTime := uint64(2)
	ip := utils.IPDesc{
		IP:   net.IPv6loopback,
		Port: 12345,
	}
	myVersion := "xD"

	msg, err := TestBuilder.Version(
		networkID,
		nodeID,
		myTime,
		ip,
		myVersion,
	)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, Version, msg.Op())
	assert.Equal(t, networkID, msg.Get(NetworkID))
	assert.Equal(t, nodeID, msg.Get(NodeID))
	assert.Equal(t, myTime, msg.Get(MyTime))
	assert.Equal(t, ip, msg.Get(IP))
	assert.Equal(t, myVersion, msg.Get(VersionStr))

	parsedMsg, err := TestBuilder.Parse(msg.Bytes())
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, Version, parsedMsg.Op())
	assert.Equal(t, networkID, parsedMsg.Get(NetworkID))
	assert.Equal(t, nodeID, parsedMsg.Get(NodeID))
	assert.Equal(t, myTime, parsedMsg.Get(MyTime))
	assert.Equal(t, ip, parsedMsg.Get(IP))
	assert.Equal(t, myVersion, parsedMsg.Get(VersionStr))
}

func TestBuildGetPeerList(t *testing.T) {
	msg, err := TestBuilder.GetPeerList()
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, GetPeerList, msg.Op())

	parsedMsg, err := TestBuilder.Parse(msg.Bytes())
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, GetPeerList, parsedMsg.Op())
}

func TestBuildPeerList(t *testing.T) {
	ips := []utils.IPDesc{
		utils.IPDesc{
			IP:   net.IPv6loopback,
			Port: 12345,
		},
		utils.IPDesc{
			IP:   net.IPv6loopback,
			Port: 54321,
		},
	}

	msg, err := TestBuilder.PeerList(ips)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, PeerList, msg.Op())
	assert.Equal(t, ips, msg.Get(Peers))

	parsedMsg, err := TestBuilder.Parse(msg.Bytes())
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, PeerList, parsedMsg.Op())
	assert.Equal(t, ips, parsedMsg.Get(Peers))
}

func TestBuildGetAcceptedFrontier(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)

	msg, err := TestBuilder.GetAcceptedFrontier(chainID, requestID)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, GetAcceptedFrontier, msg.Op())
	assert.Equal(t, chainID.Bytes(), msg.Get(ChainID))
	assert.Equal(t, requestID, msg.Get(RequestID))

	parsedMsg, err := TestBuilder.Parse(msg.Bytes())
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, GetAcceptedFrontier, parsedMsg.Op())
	assert.Equal(t, chainID.Bytes(), parsedMsg.Get(ChainID))
	assert.Equal(t, requestID, parsedMsg.Get(RequestID))
}

func TestBuildAcceptedFrontier(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	containerID := ids.Empty.Prefix(1)
	containerIDSet := ids.Set{}
	containerIDSet.Add(containerID)
	containerIDs := [][]byte{containerID.Bytes()}

	msg, err := TestBuilder.AcceptedFrontier(chainID, requestID, containerIDSet)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, AcceptedFrontier, msg.Op())
	assert.Equal(t, chainID.Bytes(), msg.Get(ChainID))
	assert.Equal(t, requestID, msg.Get(RequestID))
	assert.Equal(t, containerIDs, msg.Get(ContainerIDs))

	parsedMsg, err := TestBuilder.Parse(msg.Bytes())
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, AcceptedFrontier, parsedMsg.Op())
	assert.Equal(t, chainID.Bytes(), parsedMsg.Get(ChainID))
	assert.Equal(t, requestID, parsedMsg.Get(RequestID))
	assert.Equal(t, containerIDs, parsedMsg.Get(ContainerIDs))
}

func TestBuildGetAccepted(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	containerID := ids.Empty.Prefix(1)
	containerIDSet := ids.Set{}
	containerIDSet.Add(containerID)
	containerIDs := [][]byte{containerID.Bytes()}

	msg, err := TestBuilder.GetAccepted(chainID, requestID, containerIDSet)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, GetAccepted, msg.Op())
	assert.Equal(t, chainID.Bytes(), msg.Get(ChainID))
	assert.Equal(t, requestID, msg.Get(RequestID))
	assert.Equal(t, containerIDs, msg.Get(ContainerIDs))

	parsedMsg, err := TestBuilder.Parse(msg.Bytes())
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, GetAccepted, parsedMsg.Op())
	assert.Equal(t, chainID.Bytes(), parsedMsg.Get(ChainID))
	assert.Equal(t, requestID, parsedMsg.Get(RequestID))
	assert.Equal(t, containerIDs, parsedMsg.Get(ContainerIDs))
}

func TestBuildAccepted(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	containerID := ids.Empty.Prefix(1)
	containerIDSet := ids.Set{}
	containerIDSet.Add(containerID)
	containerIDs := [][]byte{containerID.Bytes()}

	msg, err := TestBuilder.Accepted(chainID, requestID, containerIDSet)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, Accepted, msg.Op())
	assert.Equal(t, chainID.Bytes(), msg.Get(ChainID))
	assert.Equal(t, requestID, msg.Get(RequestID))
	assert.Equal(t, containerIDs, msg.Get(ContainerIDs))

	parsedMsg, err := TestBuilder.Parse(msg.Bytes())
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, Accepted, parsedMsg.Op())
	assert.Equal(t, chainID.Bytes(), parsedMsg.Get(ChainID))
	assert.Equal(t, requestID, parsedMsg.Get(RequestID))
	assert.Equal(t, containerIDs, parsedMsg.Get(ContainerIDs))
}

func TestBuildGet(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	containerID := ids.Empty.Prefix(1)

	msg, err := TestBuilder.Get(chainID, requestID, containerID)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, Get, msg.Op())
	assert.Equal(t, chainID.Bytes(), msg.Get(ChainID))
	assert.Equal(t, requestID, msg.Get(RequestID))
	assert.Equal(t, containerID.Bytes(), msg.Get(ContainerID))

	parsedMsg, err := TestBuilder.Parse(msg.Bytes())
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, Get, parsedMsg.Op())
	assert.Equal(t, chainID.Bytes(), parsedMsg.Get(ChainID))
	assert.Equal(t, requestID, parsedMsg.Get(RequestID))
	assert.Equal(t, containerID.Bytes(), parsedMsg.Get(ContainerID))
}

func TestBuildPut(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	containerID := ids.Empty.Prefix(1)
	container := []byte{2}

	msg, err := TestBuilder.Put(chainID, requestID, containerID, container)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, Put, msg.Op())
	assert.Equal(t, chainID.Bytes(), msg.Get(ChainID))
	assert.Equal(t, requestID, msg.Get(RequestID))
	assert.Equal(t, containerID.Bytes(), msg.Get(ContainerID))
	assert.Equal(t, container, msg.Get(ContainerBytes))

	parsedMsg, err := TestBuilder.Parse(msg.Bytes())
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, Put, parsedMsg.Op())
	assert.Equal(t, chainID.Bytes(), parsedMsg.Get(ChainID))
	assert.Equal(t, requestID, parsedMsg.Get(RequestID))
	assert.Equal(t, containerID.Bytes(), parsedMsg.Get(ContainerID))
	assert.Equal(t, container, parsedMsg.Get(ContainerBytes))
}

func TestBuildPushQuery(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	containerID := ids.Empty.Prefix(1)
	container := []byte{2}

	msg, err := TestBuilder.PushQuery(chainID, requestID, containerID, container)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, PushQuery, msg.Op())
	assert.Equal(t, chainID.Bytes(), msg.Get(ChainID))
	assert.Equal(t, requestID, msg.Get(RequestID))
	assert.Equal(t, containerID.Bytes(), msg.Get(ContainerID))
	assert.Equal(t, container, msg.Get(ContainerBytes))

	parsedMsg, err := TestBuilder.Parse(msg.Bytes())
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, PushQuery, parsedMsg.Op())
	assert.Equal(t, chainID.Bytes(), parsedMsg.Get(ChainID))
	assert.Equal(t, requestID, parsedMsg.Get(RequestID))
	assert.Equal(t, containerID.Bytes(), parsedMsg.Get(ContainerID))
	assert.Equal(t, container, parsedMsg.Get(ContainerBytes))
}

func TestBuildPullQuery(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	containerID := ids.Empty.Prefix(1)

	msg, err := TestBuilder.PullQuery(chainID, requestID, containerID)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, PullQuery, msg.Op())
	assert.Equal(t, chainID.Bytes(), msg.Get(ChainID))
	assert.Equal(t, requestID, msg.Get(RequestID))
	assert.Equal(t, containerID.Bytes(), msg.Get(ContainerID))

	parsedMsg, err := TestBuilder.Parse(msg.Bytes())
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, PullQuery, parsedMsg.Op())
	assert.Equal(t, chainID.Bytes(), parsedMsg.Get(ChainID))
	assert.Equal(t, requestID, parsedMsg.Get(RequestID))
	assert.Equal(t, containerID.Bytes(), parsedMsg.Get(ContainerID))
}

func TestBuildChits(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	containerID := ids.Empty.Prefix(1)
	containerIDSet := ids.Set{}
	containerIDSet.Add(containerID)
	containerIDs := [][]byte{containerID.Bytes()}

	msg, err := TestBuilder.Chits(chainID, requestID, containerIDSet)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, Chits, msg.Op())
	assert.Equal(t, chainID.Bytes(), msg.Get(ChainID))
	assert.Equal(t, requestID, msg.Get(RequestID))
	assert.Equal(t, containerIDs, msg.Get(ContainerIDs))

	parsedMsg, err := TestBuilder.Parse(msg.Bytes())
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, Chits, parsedMsg.Op())
	assert.Equal(t, chainID.Bytes(), parsedMsg.Get(ChainID))
	assert.Equal(t, requestID, parsedMsg.Get(RequestID))
	assert.Equal(t, containerIDs, parsedMsg.Get(ContainerIDs))
}
