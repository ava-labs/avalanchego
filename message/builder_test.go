// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
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
	UncompressingBuilder OutboundMsgBuilder
	TestCodec            Codec

	dummyNodeID             = ids.ShortEmpty
	dummyOnFinishedHandling = func() {}
)

func init() {
	codec, err := NewCodecWithMemoryPool("", prometheus.NewRegistry(), 2*units.MiB)
	if err != nil {
		panic(err)
	}
	TestCodec = codec
	UncompressingBuilder = NewOutboundBuilder(codec, false /*compress*/)
}

func TestBuildGetVersion(t *testing.T) {
	msg, err := UncompressingBuilder.GetVersion()
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, GetVersion, msg.Op())

	parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
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
	subnetID := ids.Empty.Prefix(1)
	subnetIDs := [][]byte{subnetID[:]}
	msg, err := UncompressingBuilder.Version(
		networkID,
		nodeID,
		myTime,
		ip,
		myVersion,
		myVersionTime,
		sig,
		[]ids.ID{subnetID},
	)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, Version, msg.Op())

	parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)

	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, Version, parsedMsg.Op())
	assert.EqualValues(t, networkID, parsedMsg.Get(NetworkID))
	assert.EqualValues(t, nodeID, parsedMsg.Get(NodeID))
	assert.EqualValues(t, myTime, parsedMsg.Get(MyTime))
	assert.EqualValues(t, ip, parsedMsg.Get(IP))
	assert.EqualValues(t, myVersion, parsedMsg.Get(VersionStr))
	assert.EqualValues(t, myVersionTime, parsedMsg.Get(VersionTime))
	assert.EqualValues(t, sig, parsedMsg.Get(SigBytes))
	assert.EqualValues(t, subnetIDs, parsedMsg.Get(TrackedSubnets))
}

func TestBuildGetPeerList(t *testing.T) {
	msg, err := UncompressingBuilder.GetPeerList()
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, GetPeerList, msg.Op())

	parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, GetPeerList, parsedMsg.Op())
}

func TestBuildGetAcceptedFrontier(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	deadline := uint64(15)

	msg, err := UncompressingBuilder.GetAcceptedFrontier(chainID, requestID, time.Duration(deadline))
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, GetAcceptedFrontier, msg.Op())

	parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
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

	msg, err := UncompressingBuilder.AcceptedFrontier(chainID, requestID, []ids.ID{containerID})
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, AcceptedFrontier, msg.Op())

	parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
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

	msg, err := UncompressingBuilder.GetAccepted(chainID, requestID, time.Duration(deadline), []ids.ID{containerID})
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, GetAccepted, msg.Op())

	parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
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

	msg, err := UncompressingBuilder.Accepted(chainID, requestID, []ids.ID{containerID})
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, Accepted, msg.Op())

	parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
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

	msg, err := UncompressingBuilder.Get(chainID, requestID, time.Duration(deadline), containerID)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, Get, msg.Op())

	parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
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

	for _, compress := range []bool{false, true} {
		builder := NewOutboundBuilder(TestCodec, compress)
		msg, err := builder.Put(chainID, requestID, containerID, container)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, Put, msg.Op())

		parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
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

	for _, compress := range []bool{false, true} {
		builder := NewOutboundBuilder(TestCodec, compress)
		msg, err := builder.PushQuery(chainID, requestID, time.Duration(deadline), containerID, container)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, PushQuery, msg.Op())

		parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
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

	msg, err := UncompressingBuilder.PullQuery(chainID, requestID, time.Duration(deadline), containerID)
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, PullQuery, msg.Op())

	parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
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

	msg, err := UncompressingBuilder.Chits(chainID, requestID, []ids.ID{containerID})
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, Chits, msg.Op())

	parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, Chits, parsedMsg.Op())
	assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
	assert.Equal(t, requestID, parsedMsg.Get(RequestID))
	assert.Equal(t, containerIDs, parsedMsg.Get(ContainerIDs))
}

func TestBuildAncestors(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	container := ids.Empty.Prefix(1)
	container2 := ids.Empty.Prefix(2)
	containers := [][]byte{container[:], container2[:]}

	for _, compress := range []bool{false, true} {
		builder := NewOutboundBuilder(TestCodec, compress)
		msg, err := builder.Ancestors(chainID, requestID, containers)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, Ancestors, msg.Op())

		parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, Ancestors, parsedMsg.Op())
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

	for _, compress := range []bool{false, true} {
		builder := NewOutboundBuilder(TestCodec, compress)
		msg, err := builder.AppRequest(chainID, 1, time.Duration(deadline), appRequestBytes)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, AppRequest, msg.Op())

		parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
		assert.NoError(t, err)
		assert.NotNil(t, parsedMsg)
		assert.Equal(t, AppRequest, parsedMsg.Op())
	}
}

func TestBuildAppResponseMsg(t *testing.T) {
	chainID := ids.GenerateTestID()
	appResponseBytes := make([]byte, 1024)
	appResponseBytes[0] = 1
	appResponseBytes[len(appResponseBytes)-1] = 1

	for _, compress := range []bool{false, true} {
		builder := NewOutboundBuilder(TestCodec, compress)
		msg, err := builder.AppResponse(chainID, 1, appResponseBytes)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, AppResponse, msg.Op())

		parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, AppResponse, msg.Op())
		assert.EqualValues(t, 1, parsedMsg.Get(RequestID))
		assert.Equal(t, appResponseBytes, parsedMsg.Get(AppBytes))
		assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
	}
}

func TestBuildAppGossipMsg(t *testing.T) {
	chainID := ids.GenerateTestID()
	appGossipBytes := make([]byte, 1024)
	appGossipBytes[0] = 1
	appGossipBytes[len(appGossipBytes)-1] = 1

	for _, compress := range []bool{false, true} {
		testBuilder := NewOutboundBuilder(TestCodec, compress)
		msg, err := testBuilder.AppGossip(chainID, appGossipBytes)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, AppGossip, msg.Op())

		parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
		assert.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, AppGossip, msg.Op())
		assert.Equal(t, appGossipBytes, parsedMsg.Get(AppBytes))
		assert.Equal(t, chainID[:], parsedMsg.Get(ChainID))
	}
}
