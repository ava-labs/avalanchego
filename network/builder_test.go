// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"net"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/version"
)

var TestBuilder Builder

func init() {
	codec, err := newCodec(prometheus.NewRegistry())
	if err != nil {
		panic(err)
	}
	TestBuilder = Builder{
		codec:        codec,
		getByteSlice: func() []byte { return nil },
	}
}

func TestBuildGetVersion(t *testing.T) {
	msg, err := TestBuilder.GetVersion()
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, GetVersion, msg.Op())

	parsedMsg, err := TestBuilder.Parse(msg.Bytes(), false)
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, GetVersion, parsedMsg.Op())
}

func TestBuildVersion(t *testing.T) {
	networkID := uint32(12345)
	nodeID := uint32(56789)
	myTime := uint64(time.Now().Unix())
	ip := utils.IPDesc{
		IP: net.IPv4(1, 2, 3, 4),
	}
	myVersion := version.NewDefaultVersion(1, 2, 3).String()
	myVersionTime := uint64(time.Now().Unix())
	sig := make([]byte, 65)
	{
		msg, err := TestBuilder.Version(
			networkID,
			nodeID,
			myTime,
			ip,
			myVersion,
			myVersionTime,
			sig,
			false,
		)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, Version, msg.Op())
		assert.EqualValues(t, networkID, msg.Get(NetworkID))
		assert.EqualValues(t, nodeID, msg.Get(NodeID))
		assert.EqualValues(t, myTime, msg.Get(MyTime))
		assert.EqualValues(t, ip, msg.Get(IP))
		assert.EqualValues(t, myVersion, msg.Get(VersionStr))
		assert.EqualValues(t, myVersionTime, msg.Get(VersionTime))
		assert.EqualValues(t, sig, msg.Get(SigBytes))

		parsedMsg, err := TestBuilder.Parse(msg.Bytes(), false)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, Version, parsedMsg.Op())
		assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
		assert.EqualValues(t, networkID, parsedMsg.Get(NetworkID))
		assert.EqualValues(t, nodeID, parsedMsg.Get(NodeID))
		assert.EqualValues(t, myTime, parsedMsg.Get(MyTime))
		assert.EqualValues(t, ip, parsedMsg.Get(IP))
		assert.EqualValues(t, myVersion, parsedMsg.Get(VersionStr))
		assert.EqualValues(t, myVersionTime, parsedMsg.Get(VersionTime))
		assert.EqualValues(t, sig, parsedMsg.Get(SigBytes))
	}

	{
		msg, err := TestBuilder.Version(
			networkID,
			nodeID,
			myTime,
			ip,
			myVersion,
			myVersionTime,
			sig,
			true,
		)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, Version, msg.Op())

		parsedMsg, err := TestBuilder.Parse(msg.Bytes(), true)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, Version, parsedMsg.Op())
		assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
	}
}

func TestBuildGetPeerList(t *testing.T) {
	{
		msg, err := TestBuilder.GetPeerList(false)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, GetPeerList, msg.Op())

		parsedMsg, err := TestBuilder.Parse(msg.Bytes(), false)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, GetPeerList, parsedMsg.Op())
		assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
	}

	{
		msg, err := TestBuilder.GetPeerList(true)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, GetPeerList, msg.Op())

		parsedMsg, err := TestBuilder.Parse(msg.Bytes(), true)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, GetPeerList, parsedMsg.Op())
		assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
	}
}

func TestBuildGetAcceptedFrontier(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	deadline := uint64(15)

	{
		msg, err := TestBuilder.GetAcceptedFrontier(chainID, requestID, deadline, false)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, GetAcceptedFrontier, msg.Op())
		assert.Equal(t, chainID[:], msg.Get(ChainID))
		assert.Equal(t, requestID, msg.Get(RequestID))
		assert.Equal(t, deadline, msg.Get(Deadline))

		parsedMsg, err := TestBuilder.Parse(msg.Bytes(), false)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, GetAcceptedFrontier, parsedMsg.Op())
		assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
		assert.Equal(t, requestID, parsedMsg.Get(RequestID))
		assert.Equal(t, deadline, parsedMsg.Get(Deadline))
		assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
	}

	{
		msg, err := TestBuilder.GetAcceptedFrontier(chainID, requestID, deadline, true)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, GetAcceptedFrontier, msg.Op())
		assert.Equal(t, chainID[:], msg.Get(ChainID))
		assert.Equal(t, requestID, msg.Get(RequestID))
		assert.Equal(t, deadline, msg.Get(Deadline))

		parsedMsg, err := TestBuilder.Parse(msg.Bytes(), true)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, GetAcceptedFrontier, parsedMsg.Op())
		assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
		assert.Equal(t, requestID, parsedMsg.Get(RequestID))
		assert.Equal(t, deadline, parsedMsg.Get(Deadline))
		assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
	}
}

func TestBuildAcceptedFrontier(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	containerID := ids.Empty.Prefix(1)
	containerIDs := [][]byte{containerID[:]}

	{
		msg, err := TestBuilder.AcceptedFrontier(chainID, requestID, []ids.ID{containerID}, false)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, AcceptedFrontier, msg.Op())
		assert.Equal(t, chainID[:], msg.Get(ChainID))
		assert.Equal(t, requestID, msg.Get(RequestID))
		assert.Equal(t, containerIDs, msg.Get(ContainerIDs))

		parsedMsg, err := TestBuilder.Parse(msg.Bytes(), false)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, AcceptedFrontier, parsedMsg.Op())
		assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
		assert.Equal(t, requestID, parsedMsg.Get(RequestID))
		assert.Equal(t, containerIDs, parsedMsg.Get(ContainerIDs))
		assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
	}

	{
		msg, err := TestBuilder.AcceptedFrontier(chainID, requestID, []ids.ID{containerID}, true)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, AcceptedFrontier, msg.Op())
		assert.Equal(t, chainID[:], msg.Get(ChainID))
		assert.Equal(t, requestID, msg.Get(RequestID))
		assert.Equal(t, containerIDs, msg.Get(ContainerIDs))

		parsedMsg, err := TestBuilder.Parse(msg.Bytes(), true)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, AcceptedFrontier, parsedMsg.Op())
		assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
		assert.Equal(t, requestID, parsedMsg.Get(RequestID))
		assert.Equal(t, containerIDs, parsedMsg.Get(ContainerIDs))
		assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
	}
}

func TestBuildGetAccepted(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	deadline := uint64(15)
	containerID := ids.Empty.Prefix(1)
	containerIDs := [][]byte{containerID[:]}

	{
		msg, err := TestBuilder.GetAccepted(chainID, requestID, deadline, []ids.ID{containerID}, false)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, GetAccepted, msg.Op())
		assert.Equal(t, chainID[:], msg.Get(ChainID))
		assert.Equal(t, requestID, msg.Get(RequestID))
		assert.Equal(t, deadline, msg.Get(Deadline))
		assert.Equal(t, containerIDs, msg.Get(ContainerIDs))

		parsedMsg, err := TestBuilder.Parse(msg.Bytes(), false)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, GetAccepted, parsedMsg.Op())
		assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
		assert.Equal(t, requestID, parsedMsg.Get(RequestID))
		assert.Equal(t, deadline, parsedMsg.Get(Deadline))
		assert.Equal(t, containerIDs, parsedMsg.Get(ContainerIDs))
		assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
	}

	{
		msg, err := TestBuilder.GetAccepted(chainID, requestID, deadline, []ids.ID{containerID}, true)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, GetAccepted, msg.Op())
		assert.Equal(t, chainID[:], msg.Get(ChainID))
		assert.Equal(t, requestID, msg.Get(RequestID))
		assert.Equal(t, deadline, msg.Get(Deadline))
		assert.Equal(t, containerIDs, msg.Get(ContainerIDs))

		parsedMsg, err := TestBuilder.Parse(msg.Bytes(), true)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, GetAccepted, parsedMsg.Op())
		assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
		assert.Equal(t, requestID, parsedMsg.Get(RequestID))
		assert.Equal(t, deadline, parsedMsg.Get(Deadline))
		assert.Equal(t, containerIDs, parsedMsg.Get(ContainerIDs))
		assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
	}
}

func TestBuildAccepted(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	containerID := ids.Empty.Prefix(1)
	containerIDs := [][]byte{containerID[:]}

	{
		msg, err := TestBuilder.Accepted(chainID, requestID, []ids.ID{containerID}, false)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, Accepted, msg.Op())
		assert.Equal(t, chainID[:], msg.Get(ChainID))
		assert.Equal(t, requestID, msg.Get(RequestID))
		assert.Equal(t, containerIDs, msg.Get(ContainerIDs))

		parsedMsg, err := TestBuilder.Parse(msg.Bytes(), false)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, Accepted, parsedMsg.Op())
		assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
		assert.Equal(t, requestID, parsedMsg.Get(RequestID))
		assert.Equal(t, containerIDs, parsedMsg.Get(ContainerIDs))
		assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
	}

	{
		msg, err := TestBuilder.Accepted(chainID, requestID, []ids.ID{containerID}, true)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, Accepted, msg.Op())
		assert.Equal(t, chainID[:], msg.Get(ChainID))
		assert.Equal(t, requestID, msg.Get(RequestID))
		assert.Equal(t, containerIDs, msg.Get(ContainerIDs))

		parsedMsg, err := TestBuilder.Parse(msg.Bytes(), true)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, Accepted, parsedMsg.Op())
		assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
		assert.Equal(t, requestID, parsedMsg.Get(RequestID))
		assert.Equal(t, containerIDs, parsedMsg.Get(ContainerIDs))
		assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
	}
}

func TestBuildGet(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	deadline := uint64(15)
	containerID := ids.Empty.Prefix(1)

	{
		msg, err := TestBuilder.Get(chainID, requestID, deadline, containerID, false)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, Get, msg.Op())
		assert.Equal(t, chainID[:], msg.Get(ChainID))
		assert.Equal(t, requestID, msg.Get(RequestID))
		assert.Equal(t, deadline, msg.Get(Deadline))
		assert.Equal(t, containerID[:], msg.Get(ContainerID))

		parsedMsg, err := TestBuilder.Parse(msg.Bytes(), false)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, Get, parsedMsg.Op())
		assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
		assert.Equal(t, requestID, parsedMsg.Get(RequestID))
		assert.Equal(t, deadline, parsedMsg.Get(Deadline))
		assert.Equal(t, containerID[:], parsedMsg.Get(ContainerID))
		assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
	}

	{
		msg, err := TestBuilder.Get(chainID, requestID, deadline, containerID, true)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, Get, msg.Op())
		assert.Equal(t, chainID[:], msg.Get(ChainID))
		assert.Equal(t, requestID, msg.Get(RequestID))
		assert.Equal(t, deadline, msg.Get(Deadline))
		assert.Equal(t, containerID[:], msg.Get(ContainerID))

		parsedMsg, err := TestBuilder.Parse(msg.Bytes(), true)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, Get, parsedMsg.Op())
		assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
		assert.Equal(t, requestID, parsedMsg.Get(RequestID))
		assert.Equal(t, deadline, parsedMsg.Get(Deadline))
		assert.Equal(t, containerID[:], parsedMsg.Get(ContainerID))
		assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
	}
}

func TestBuildPut(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	containerID := ids.Empty.Prefix(1)
	container := []byte{2}

	{
		msg, err := TestBuilder.Put(chainID, requestID, containerID, container, false)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, Put, msg.Op())
		assert.Equal(t, chainID[:], msg.Get(ChainID))
		assert.Equal(t, requestID, msg.Get(RequestID))
		assert.Equal(t, containerID[:], msg.Get(ContainerID))
		assert.Equal(t, container, msg.Get(ContainerBytes))

		parsedMsg, err := TestBuilder.Parse(msg.Bytes(), false)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, Put, parsedMsg.Op())
		assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
		assert.Equal(t, requestID, parsedMsg.Get(RequestID))
		assert.Equal(t, containerID[:], parsedMsg.Get(ContainerID))
		assert.Equal(t, container, parsedMsg.Get(ContainerBytes))
		assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
	}

	{
		msg, err := TestBuilder.Put(chainID, requestID, containerID, container, true)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, Put, msg.Op())
		assert.Equal(t, chainID[:], msg.Get(ChainID))
		assert.Equal(t, requestID, msg.Get(RequestID))
		assert.Equal(t, containerID[:], msg.Get(ContainerID))
		assert.Equal(t, container, msg.Get(ContainerBytes))

		parsedMsg, err := TestBuilder.Parse(msg.Bytes(), true)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, Put, parsedMsg.Op())
		assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
		assert.Equal(t, requestID, parsedMsg.Get(RequestID))
		assert.Equal(t, containerID[:], parsedMsg.Get(ContainerID))
		assert.Equal(t, container, parsedMsg.Get(ContainerBytes))
	}
}

func TestBuildPushQuery(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	deadline := uint64(15)
	containerID := ids.Empty.Prefix(1)
	container := []byte{2}

	{
		msg, err := TestBuilder.PushQuery(chainID, requestID, deadline, containerID, container, false)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, PushQuery, msg.Op())
		assert.Equal(t, chainID[:], msg.Get(ChainID))
		assert.Equal(t, requestID, msg.Get(RequestID))
		assert.Equal(t, deadline, msg.Get(Deadline))
		assert.Equal(t, containerID[:], msg.Get(ContainerID))
		assert.Equal(t, container, msg.Get(ContainerBytes))

		parsedMsg, err := TestBuilder.Parse(msg.Bytes(), false)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, PushQuery, parsedMsg.Op())
		assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
		assert.Equal(t, requestID, parsedMsg.Get(RequestID))
		assert.Equal(t, deadline, parsedMsg.Get(Deadline))
		assert.Equal(t, containerID[:], parsedMsg.Get(ContainerID))
		assert.Equal(t, container, parsedMsg.Get(ContainerBytes))
		assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
	}

	{
		msg, err := TestBuilder.PushQuery(chainID, requestID, deadline, containerID, container, true)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, PushQuery, msg.Op())
		assert.Equal(t, chainID[:], msg.Get(ChainID))
		assert.Equal(t, requestID, msg.Get(RequestID))
		assert.Equal(t, deadline, msg.Get(Deadline))
		assert.Equal(t, containerID[:], msg.Get(ContainerID))
		assert.Equal(t, container, msg.Get(ContainerBytes))

		parsedMsg, err := TestBuilder.Parse(msg.Bytes(), true)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, PushQuery, parsedMsg.Op())
		assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
		assert.Equal(t, requestID, parsedMsg.Get(RequestID))
		assert.Equal(t, deadline, parsedMsg.Get(Deadline))
		assert.Equal(t, containerID[:], parsedMsg.Get(ContainerID))
		assert.Equal(t, container, parsedMsg.Get(ContainerBytes))
	}
}

func TestBuildPullQuery(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	deadline := uint64(15)
	containerID := ids.Empty.Prefix(1)

	{
		msg, err := TestBuilder.PullQuery(chainID, requestID, deadline, containerID, false)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, PullQuery, msg.Op())
		assert.Equal(t, chainID[:], msg.Get(ChainID))
		assert.Equal(t, requestID, msg.Get(RequestID))
		assert.Equal(t, deadline, msg.Get(Deadline))
		assert.Equal(t, containerID[:], msg.Get(ContainerID))

		parsedMsg, err := TestBuilder.Parse(msg.Bytes(), false)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, PullQuery, parsedMsg.Op())
		assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
		assert.Equal(t, requestID, parsedMsg.Get(RequestID))
		assert.Equal(t, deadline, parsedMsg.Get(Deadline))
		assert.Equal(t, containerID[:], parsedMsg.Get(ContainerID))
		assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
	}

	{
		msg, err := TestBuilder.PullQuery(chainID, requestID, deadline, containerID, true)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, PullQuery, msg.Op())
		assert.Equal(t, chainID[:], msg.Get(ChainID))
		assert.Equal(t, requestID, msg.Get(RequestID))
		assert.Equal(t, deadline, msg.Get(Deadline))
		assert.Equal(t, containerID[:], msg.Get(ContainerID))

		parsedMsg, err := TestBuilder.Parse(msg.Bytes(), true)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, PullQuery, parsedMsg.Op())
		assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
		assert.Equal(t, requestID, parsedMsg.Get(RequestID))
		assert.Equal(t, deadline, parsedMsg.Get(Deadline))
		assert.Equal(t, containerID[:], parsedMsg.Get(ContainerID))
		assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
	}
}

func TestBuildChits(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	containerID := ids.Empty.Prefix(1)
	containerIDs := [][]byte{containerID[:]}

	{
		msg, err := TestBuilder.Chits(chainID, requestID, []ids.ID{containerID}, false)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, Chits, msg.Op())
		assert.Equal(t, chainID[:], msg.Get(ChainID))
		assert.Equal(t, requestID, msg.Get(RequestID))
		assert.Equal(t, containerIDs, msg.Get(ContainerIDs))

		parsedMsg, err := TestBuilder.Parse(msg.Bytes(), false)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, Chits, parsedMsg.Op())
		assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
		assert.Equal(t, requestID, parsedMsg.Get(RequestID))
		assert.Equal(t, containerIDs, parsedMsg.Get(ContainerIDs))
		assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
	}

	{
		msg, err := TestBuilder.Chits(chainID, requestID, []ids.ID{containerID}, true)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, Chits, msg.Op())
		assert.Equal(t, chainID[:], msg.Get(ChainID))
		assert.Equal(t, requestID, msg.Get(RequestID))
		assert.Equal(t, containerIDs, msg.Get(ContainerIDs))

		parsedMsg, err := TestBuilder.Parse(msg.Bytes(), true)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, Chits, parsedMsg.Op())
		assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
		assert.Equal(t, requestID, parsedMsg.Get(RequestID))
		assert.Equal(t, containerIDs, parsedMsg.Get(ContainerIDs))
		assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
	}
}

func TestBuildMultiPut(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	container := ids.Empty.Prefix(1)
	container2 := ids.Empty.Prefix(2)
	containers := [][]byte{container[:], container2[:]}

	{
		msg, err := TestBuilder.MultiPut(chainID, requestID, containers, false)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, MultiPut, msg.Op())
		assert.Equal(t, chainID[:], msg.Get(ChainID))
		assert.Equal(t, requestID, msg.Get(RequestID))
		assert.Equal(t, containers, msg.Get(MultiContainerBytes))

		parsedMsg, err := TestBuilder.Parse(msg.Bytes(), false)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, MultiPut, parsedMsg.Op())
		assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
		assert.Equal(t, requestID, parsedMsg.Get(RequestID))
		assert.Equal(t, containers, parsedMsg.Get(MultiContainerBytes))
		assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
	}

	{
		msg, err := TestBuilder.MultiPut(chainID, requestID, containers, true)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, MultiPut, msg.Op())
		assert.Equal(t, chainID[:], msg.Get(ChainID))
		assert.Equal(t, requestID, msg.Get(RequestID))
		assert.Equal(t, containers, msg.Get(MultiContainerBytes))

		parsedMsg, err := TestBuilder.Parse(msg.Bytes(), true)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, MultiPut, parsedMsg.Op())
		assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
		assert.Equal(t, requestID, parsedMsg.Get(RequestID))
		assert.Equal(t, containers, parsedMsg.Get(MultiContainerBytes))
	}
}
