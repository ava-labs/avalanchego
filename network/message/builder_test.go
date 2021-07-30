// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"net"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/version"
)

var (
	TestBuilder Builder
	TestCodec   Codec
)

func init() {
	codec, err := NewCodec("", prometheus.NewRegistry(), 2*units.MiB)
	if err != nil {
		panic(err)
	}
	TestCodec = codec
	TestBuilder = NewBuilder(codec)
}

func TestBuildGetVersion(t *testing.T) {
	msg, err := TestBuilder.GetVersion()
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, GetVersion, msg.Op())

	parsedMsg, err := TestCodec.Parse(msg.Bytes(), false)
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
	msg, err := TestBuilder.Version(
		networkID,
		nodeID,
		myTime,
		ip,
		myVersion,
		myVersionTime,
		sig,
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

	parsedMsg, err := TestCodec.Parse(msg.Bytes(), false)

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

func TestBuildGetPeerList(t *testing.T) {
	msg, err := TestBuilder.GetPeerList()
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, GetPeerList, msg.Op())

	parsedMsg, err := TestCodec.Parse(msg.Bytes(), false)
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, GetPeerList, parsedMsg.Op())
	assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
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

	parsedMsg, err := TestCodec.Parse(msg.Bytes(), false)
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, GetAcceptedFrontier, parsedMsg.Op())
	assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
	assert.Equal(t, requestID, parsedMsg.Get(RequestID))
	assert.Equal(t, deadline, parsedMsg.Get(Deadline))
	assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
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

	parsedMsg, err := TestCodec.Parse(msg.Bytes(), false)
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, AcceptedFrontier, parsedMsg.Op())
	assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
	assert.Equal(t, requestID, parsedMsg.Get(RequestID))
	assert.Equal(t, containerIDs, parsedMsg.Get(ContainerIDs))
	assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
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

	parsedMsg, err := TestCodec.Parse(msg.Bytes(), false)
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, GetAccepted, parsedMsg.Op())
	assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
	assert.Equal(t, requestID, parsedMsg.Get(RequestID))
	assert.Equal(t, deadline, parsedMsg.Get(Deadline))
	assert.Equal(t, containerIDs, parsedMsg.Get(ContainerIDs))
	assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
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

	parsedMsg, err := TestCodec.Parse(msg.Bytes(), false)
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, Accepted, parsedMsg.Op())
	assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
	assert.Equal(t, requestID, parsedMsg.Get(RequestID))
	assert.Equal(t, containerIDs, parsedMsg.Get(ContainerIDs))
	assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
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

	parsedMsg, err := TestCodec.Parse(msg.Bytes(), false)
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, Get, parsedMsg.Op())
	assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
	assert.Equal(t, requestID, parsedMsg.Get(RequestID))
	assert.Equal(t, deadline, parsedMsg.Get(Deadline))
	assert.Equal(t, containerID[:], parsedMsg.Get(ContainerID))
	assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
}

func TestBuildPut(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	containerID := ids.Empty.Prefix(1)
	container := []byte{2}

	{ // no compression
		msg, err := TestBuilder.Put(chainID, requestID, containerID, container, false, false)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, Put, msg.Op())
		assert.Equal(t, chainID[:], msg.Get(ChainID))
		assert.Equal(t, requestID, msg.Get(RequestID))
		assert.Equal(t, containerID[:], msg.Get(ContainerID))
		assert.Equal(t, container, msg.Get(ContainerBytes))

		parsedMsg, err := TestCodec.Parse(msg.Bytes(), false)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, Put, parsedMsg.Op())
		assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
		assert.Equal(t, requestID, parsedMsg.Get(RequestID))
		assert.Equal(t, containerID[:], parsedMsg.Get(ContainerID))
		assert.Equal(t, container, parsedMsg.Get(ContainerBytes))
		assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
	}

	{ // no compression, with isCompressed flag
		msg, err := TestBuilder.Put(chainID, requestID, containerID, container, true, false)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, Put, msg.Op())
		assert.Equal(t, chainID[:], msg.Get(ChainID))
		assert.Equal(t, requestID, msg.Get(RequestID))
		assert.Equal(t, containerID[:], msg.Get(ContainerID))
		assert.Equal(t, container, msg.Get(ContainerBytes))

		parsedMsg, err := TestCodec.Parse(msg.Bytes(), true)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, Put, parsedMsg.Op())
		assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
		assert.Equal(t, requestID, parsedMsg.Get(RequestID))
		assert.Equal(t, containerID[:], parsedMsg.Get(ContainerID))
		assert.Equal(t, container, parsedMsg.Get(ContainerBytes))
		assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())

		_, err = TestCodec.Parse(msg.Bytes(), false)
		assert.Error(t, err)
	}

	{ // with compression
		msg, err := TestBuilder.Put(chainID, requestID, containerID, container, true, true)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, Put, msg.Op())
		assert.Equal(t, chainID[:], msg.Get(ChainID))
		assert.Equal(t, requestID, msg.Get(RequestID))
		assert.Equal(t, containerID[:], msg.Get(ContainerID))
		assert.Equal(t, container, msg.Get(ContainerBytes))

		parsedMsg, err := TestCodec.Parse(msg.Bytes(), true)
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

	{ // no compression
		msg, err := TestBuilder.PushQuery(chainID, requestID, deadline, containerID, container, false, false)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, PushQuery, msg.Op())
		assert.Equal(t, chainID[:], msg.Get(ChainID))
		assert.Equal(t, requestID, msg.Get(RequestID))
		assert.Equal(t, deadline, msg.Get(Deadline))
		assert.Equal(t, containerID[:], msg.Get(ContainerID))
		assert.Equal(t, container, msg.Get(ContainerBytes))

		parsedMsg, err := TestCodec.Parse(msg.Bytes(), false)
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

	{ // no compression, with isCompressed flag
		msg, err := TestBuilder.PushQuery(chainID, requestID, deadline, containerID, container, true, false)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, PushQuery, msg.Op())
		assert.Equal(t, chainID[:], msg.Get(ChainID))
		assert.Equal(t, requestID, msg.Get(RequestID))
		assert.Equal(t, deadline, msg.Get(Deadline))
		assert.Equal(t, containerID[:], msg.Get(ContainerID))
		assert.Equal(t, container, msg.Get(ContainerBytes))

		parsedMsg, err := TestCodec.Parse(msg.Bytes(), true)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, PushQuery, parsedMsg.Op())
		assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
		assert.Equal(t, requestID, parsedMsg.Get(RequestID))
		assert.Equal(t, deadline, parsedMsg.Get(Deadline))
		assert.Equal(t, containerID[:], parsedMsg.Get(ContainerID))
		assert.Equal(t, container, parsedMsg.Get(ContainerBytes))
		assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())

		_, err = TestCodec.Parse(msg.Bytes(), false)
		assert.Error(t, err)
	}

	{ // with compression
		msg, err := TestBuilder.PushQuery(chainID, requestID, deadline, containerID, container, true, true)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, PushQuery, msg.Op())
		assert.Equal(t, chainID[:], msg.Get(ChainID))
		assert.Equal(t, requestID, msg.Get(RequestID))
		assert.Equal(t, deadline, msg.Get(Deadline))
		assert.Equal(t, containerID[:], msg.Get(ContainerID))
		assert.Equal(t, container, msg.Get(ContainerBytes))

		parsedMsg, err := TestCodec.Parse(msg.Bytes(), true)
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

	msg, err := TestBuilder.PullQuery(chainID, requestID, deadline, containerID)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, PullQuery, msg.Op())
	assert.Equal(t, chainID[:], msg.Get(ChainID))
	assert.Equal(t, requestID, msg.Get(RequestID))
	assert.Equal(t, deadline, msg.Get(Deadline))
	assert.Equal(t, containerID[:], msg.Get(ContainerID))

	parsedMsg, err := TestCodec.Parse(msg.Bytes(), false)
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, PullQuery, parsedMsg.Op())
	assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
	assert.Equal(t, requestID, parsedMsg.Get(RequestID))
	assert.Equal(t, deadline, parsedMsg.Get(Deadline))
	assert.Equal(t, containerID[:], parsedMsg.Get(ContainerID))
	assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
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

	parsedMsg, err := TestCodec.Parse(msg.Bytes(), false)
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, Chits, parsedMsg.Op())
	assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
	assert.Equal(t, requestID, parsedMsg.Get(RequestID))
	assert.Equal(t, containerIDs, parsedMsg.Get(ContainerIDs))
	assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
}

func TestBuildMultiPut(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	container := ids.Empty.Prefix(1)
	container2 := ids.Empty.Prefix(2)
	containers := [][]byte{container[:], container2[:]}

	{ // no compression
		msg, err := TestBuilder.MultiPut(chainID, requestID, containers, false, false)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, MultiPut, msg.Op())
		assert.Equal(t, chainID[:], msg.Get(ChainID))
		assert.Equal(t, requestID, msg.Get(RequestID))
		assert.Equal(t, containers, msg.Get(MultiContainerBytes))

		parsedMsg, err := TestCodec.Parse(msg.Bytes(), false)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, MultiPut, parsedMsg.Op())
		assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
		assert.Equal(t, requestID, parsedMsg.Get(RequestID))
		assert.Equal(t, containers, parsedMsg.Get(MultiContainerBytes))
		assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())
	}

	{ // no compression, with isCompress flag
		msg, err := TestBuilder.MultiPut(chainID, requestID, containers, true, false)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, MultiPut, msg.Op())
		assert.Equal(t, chainID[:], msg.Get(ChainID))
		assert.Equal(t, requestID, msg.Get(RequestID))
		assert.Equal(t, containers, msg.Get(MultiContainerBytes))

		parsedMsg, err := TestCodec.Parse(msg.Bytes(), true)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, MultiPut, parsedMsg.Op())
		assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
		assert.Equal(t, requestID, parsedMsg.Get(RequestID))
		assert.Equal(t, containers, parsedMsg.Get(MultiContainerBytes))
		assert.EqualValues(t, msg.Bytes(), parsedMsg.Bytes())

		_, err = TestCodec.Parse(msg.Bytes(), false)
		assert.Error(t, err)
	}

	{ // with compression
		msg, err := TestBuilder.MultiPut(chainID, requestID, containers, true, true)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, MultiPut, msg.Op())
		assert.Equal(t, chainID[:], msg.Get(ChainID))
		assert.Equal(t, requestID, msg.Get(RequestID))
		assert.Equal(t, containers, msg.Get(MultiContainerBytes))

		parsedMsg, err := TestCodec.Parse(msg.Bytes(), true)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, MultiPut, parsedMsg.Op())
		assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
		assert.Equal(t, requestID, parsedMsg.Get(RequestID))
		assert.Equal(t, containers, parsedMsg.Get(MultiContainerBytes))
	}
}

func TestBuildAppRequestMsg(t *testing.T) {
	chainID := ids.GenerateTestID()
	appRequestBytes := make([]byte, 1024)
	appRequestBytes[0] = 1
	appRequestBytes[len(appRequestBytes)-1] = 1
	deadline := uint64(time.Now().Unix())

	// Build the message
	msg, err := TestBuilder.AppRequest(chainID, 1, deadline, appRequestBytes)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, AppRequest, msg.Op())
	assert.Equal(t, deadline, msg.Get(Deadline))
	assert.EqualValues(t, 1, msg.Get(RequestID))
	assert.Equal(t, appRequestBytes, msg.Get(AppRequestBytes))
	assert.Equal(t, chainID[:], msg.Get(ChainID))

	// Parse the message
	msg, err = TestCodec.Parse(msg.Bytes(), false)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, AppRequest, msg.Op())
	assert.Equal(t, deadline, msg.Get(Deadline))
	assert.EqualValues(t, 1, msg.Get(RequestID))
	assert.Equal(t, appRequestBytes, msg.Get(AppRequestBytes))
	assert.Equal(t, chainID[:], msg.Get(ChainID))
}

func TestBuildAppResponseMsg(t *testing.T) {
	chainID := ids.GenerateTestID()
	appResponseBytes := make([]byte, 1024)
	appResponseBytes[0] = 1
	appResponseBytes[len(appResponseBytes)-1] = 1

	// Build the message
	msg, err := TestBuilder.AppResponse(chainID, 1, appResponseBytes)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, AppResponse, msg.Op())
	assert.EqualValues(t, 1, msg.Get(RequestID))
	assert.Equal(t, appResponseBytes, msg.Get(AppResponseBytes))
	assert.Equal(t, chainID[:], msg.Get(ChainID))

	// Parse the message
	msg, err = TestCodec.Parse(msg.Bytes(), false)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, AppResponse, msg.Op())
	assert.EqualValues(t, 1, msg.Get(RequestID))
	assert.Equal(t, appResponseBytes, msg.Get(AppResponseBytes))
	assert.Equal(t, chainID[:], msg.Get(ChainID))
}

func TestBuildAppGossipMsg(t *testing.T) {
	chainID := ids.GenerateTestID()
	appGossipBytes := make([]byte, 1024)
	appGossipBytes[0] = 1
	appGossipBytes[len(appGossipBytes)-1] = 1

	// Build the message
	msg, err := TestBuilder.AppGossip(chainID, appGossipBytes)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, AppGossip, msg.Op())
	assert.Equal(t, appGossipBytes, msg.Get(AppGossipBytes))
	assert.Equal(t, chainID[:], msg.Get(ChainID))

	// Parse the message
	msg, err = TestCodec.Parse(msg.Bytes(), false)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, AppGossip, msg.Op())
	assert.Equal(t, appGossipBytes, msg.Get(AppGossipBytes))
	assert.Equal(t, chainID[:], msg.Get(ChainID))
}
