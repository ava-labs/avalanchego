// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
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
		{IP: net.IPv6loopback, Port: 12345},
		{IP: net.IPv6loopback, Port: 54321},
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
	deadline := uint64(15)

	msg, err := TestBuilder.GetAcceptedFrontier(chainID, requestID, deadline)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, GetAcceptedFrontier, msg.Op())
	assert.Equal(t, chainID[:], msg.Get(ChainID))
	assert.Equal(t, requestID, msg.Get(RequestID))
	assert.Equal(t, deadline, msg.Get(Deadline))

	parsedMsg, err := TestBuilder.Parse(msg.Bytes())
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, GetAcceptedFrontier, parsedMsg.Op())
	assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
	assert.Equal(t, requestID, parsedMsg.Get(RequestID))
	assert.Equal(t, deadline, parsedMsg.Get(Deadline))
}

func TestBuildAcceptedFrontier(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	containerID := ids.Empty.Prefix(1)
	containerIDs := [][]byte{containerID[:]}

	msg, err := TestBuilder.AcceptedFrontier(chainID, requestID, []ids.ID{containerID})
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, AcceptedFrontier, msg.Op())
	assert.Equal(t, chainID[:], msg.Get(ChainID))
	assert.Equal(t, requestID, msg.Get(RequestID))
	assert.Equal(t, containerIDs, msg.Get(ContainerIDs))

	parsedMsg, err := TestBuilder.Parse(msg.Bytes())
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, AcceptedFrontier, parsedMsg.Op())
	assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
	assert.Equal(t, requestID, parsedMsg.Get(RequestID))
	assert.Equal(t, containerIDs, parsedMsg.Get(ContainerIDs))
}

func TestBuildGetAccepted(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	deadline := uint64(15)
	containerID := ids.Empty.Prefix(1)
	containerIDs := [][]byte{containerID[:]}

	msg, err := TestBuilder.GetAccepted(chainID, requestID, deadline, []ids.ID{containerID})
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, GetAccepted, msg.Op())
	assert.Equal(t, chainID[:], msg.Get(ChainID))
	assert.Equal(t, requestID, msg.Get(RequestID))
	assert.Equal(t, deadline, msg.Get(Deadline))
	assert.Equal(t, containerIDs, msg.Get(ContainerIDs))

	parsedMsg, err := TestBuilder.Parse(msg.Bytes())
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, GetAccepted, parsedMsg.Op())
	assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
	assert.Equal(t, requestID, parsedMsg.Get(RequestID))
	assert.Equal(t, deadline, parsedMsg.Get(Deadline))
	assert.Equal(t, containerIDs, parsedMsg.Get(ContainerIDs))
}

func TestBuildAccepted(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	containerID := ids.Empty.Prefix(1)
	containerIDs := [][]byte{containerID[:]}

	msg, err := TestBuilder.Accepted(chainID, requestID, []ids.ID{containerID})
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, Accepted, msg.Op())
	assert.Equal(t, chainID[:], msg.Get(ChainID))
	assert.Equal(t, requestID, msg.Get(RequestID))
	assert.Equal(t, containerIDs, msg.Get(ContainerIDs))

	parsedMsg, err := TestBuilder.Parse(msg.Bytes())
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, Accepted, parsedMsg.Op())
	assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
	assert.Equal(t, requestID, parsedMsg.Get(RequestID))
	assert.Equal(t, containerIDs, parsedMsg.Get(ContainerIDs))
}

func TestBuildGet(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	deadline := uint64(15)
	containerID := ids.Empty.Prefix(1)

	msg, err := TestBuilder.Get(chainID, requestID, deadline, containerID)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, Get, msg.Op())
	assert.Equal(t, chainID[:], msg.Get(ChainID))
	assert.Equal(t, requestID, msg.Get(RequestID))
	assert.Equal(t, deadline, msg.Get(Deadline))
	assert.Equal(t, containerID[:], msg.Get(ContainerID))

	parsedMsg, err := TestBuilder.Parse(msg.Bytes())
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, Get, parsedMsg.Op())
	assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
	assert.Equal(t, requestID, parsedMsg.Get(RequestID))
	assert.Equal(t, deadline, parsedMsg.Get(Deadline))
	assert.Equal(t, containerID[:], parsedMsg.Get(ContainerID))
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
	assert.Equal(t, chainID[:], msg.Get(ChainID))
	assert.Equal(t, requestID, msg.Get(RequestID))
	assert.Equal(t, containerID[:], msg.Get(ContainerID))
	assert.Equal(t, container, msg.Get(ContainerBytes))

	parsedMsg, err := TestBuilder.Parse(msg.Bytes())
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, Put, parsedMsg.Op())
	assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
	assert.Equal(t, requestID, parsedMsg.Get(RequestID))
	assert.Equal(t, containerID[:], parsedMsg.Get(ContainerID))
	assert.Equal(t, container, parsedMsg.Get(ContainerBytes))
}

func TestBuildPushQuery(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	deadline := uint64(15)
	containerID := ids.Empty.Prefix(1)
	container := []byte{2}

	msg, err := TestBuilder.PushQuery(chainID, requestID, deadline, containerID, container)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, PushQuery, msg.Op())
	assert.Equal(t, chainID[:], msg.Get(ChainID))
	assert.Equal(t, requestID, msg.Get(RequestID))
	assert.Equal(t, deadline, msg.Get(Deadline))
	assert.Equal(t, containerID[:], msg.Get(ContainerID))
	assert.Equal(t, container, msg.Get(ContainerBytes))

	parsedMsg, err := TestBuilder.Parse(msg.Bytes())
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, PushQuery, parsedMsg.Op())
	assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
	assert.Equal(t, requestID, parsedMsg.Get(RequestID))
	assert.Equal(t, deadline, parsedMsg.Get(Deadline))
	assert.Equal(t, containerID[:], parsedMsg.Get(ContainerID))
	assert.Equal(t, container, parsedMsg.Get(ContainerBytes))
}

func TestBuildPullQuery(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	deadline := uint64(15)
	containerID := ids.Empty.Prefix(1)

	msg, err := TestBuilder.PullQuery(chainID, requestID, deadline, containerID)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, PullQuery, msg.Op())
	assert.Equal(t, chainID[:], msg.Get(ChainID))
	assert.Equal(t, requestID, msg.Get(RequestID))
	assert.Equal(t, deadline, msg.Get(Deadline))
	assert.Equal(t, containerID[:], msg.Get(ContainerID))

	parsedMsg, err := TestBuilder.Parse(msg.Bytes())
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, PullQuery, parsedMsg.Op())
	assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
	assert.Equal(t, requestID, parsedMsg.Get(RequestID))
	assert.Equal(t, deadline, parsedMsg.Get(Deadline))
	assert.Equal(t, containerID[:], parsedMsg.Get(ContainerID))
}

func TestBuildChits(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	containerID := ids.Empty.Prefix(1)
	containerIDs := [][]byte{containerID[:]}

	msg, err := TestBuilder.Chits(chainID, requestID, []ids.ID{containerID})
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, Chits, msg.Op())
	assert.Equal(t, chainID[:], msg.Get(ChainID))
	assert.Equal(t, requestID, msg.Get(RequestID))
	assert.Equal(t, containerIDs, msg.Get(ContainerIDs))

	parsedMsg, err := TestBuilder.Parse(msg.Bytes())
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, Chits, parsedMsg.Op())
	assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
	assert.Equal(t, requestID, parsedMsg.Get(RequestID))
	assert.Equal(t, containerIDs, parsedMsg.Get(ContainerIDs))
}

func TestBuildAppMsg(t *testing.T) {
	chainID := ids.GenerateTestID()
	appMsgBytes := make([]byte, 1024)
	appMsgBytes[0] = 1
	appMsgBytes[len(appMsgBytes)-1] = 1

	// Build the message
	msg, err := TestBuilder.AppMsg(chainID, appMsgBytes)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, AppMsg, msg.Op())
	assert.Equal(t, appMsgBytes, msg.Get(AppMsgBytes))
	assert.Equal(t, chainID[:], msg.Get(ChainID))

	// Parse the message
	msg, err = TestBuilder.Parse(msg.Bytes())
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, AppMsg, msg.Op())
	assert.Equal(t, appMsgBytes, msg.Get(AppMsgBytes))
	assert.Equal(t, chainID[:], msg.Get(ChainID))
}
