// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"net"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/version"
)

var (
	UncompressingBuilder  OutboundMsgBuilder
	TestInboundMsgBuilder InboundMsgBuilder
	TestCodec             Codec

	dummyNodeID             = ids.EmptyNodeID
	dummyOnFinishedHandling = func() {}
)

func init() {
	codec, err := NewCodecWithMemoryPool("", prometheus.NewRegistry(), 2*units.MiB, 10*time.Second)
	if err != nil {
		panic(err)
	}
	TestCodec = codec
	UncompressingBuilder = NewOutboundBuilder(codec, false /*compress*/)
	TestInboundMsgBuilder = NewInboundBuilder(codec)
}

func TestBuildVersion(t *testing.T) {
	networkID := uint32(12345)
	myTime := uint64(time.Now().Unix())
	ip := ips.IPPort{
		IP: net.IPv4(1, 2, 3, 4),
	}

	myVersion := &version.Semantic{
		Major: 1,
		Minor: 2,
		Patch: 3,
	}
	myVersionStr := myVersion.String()
	myVersionTime := uint64(time.Now().Unix())
	sig := make([]byte, 65)
	subnetID := ids.Empty.Prefix(1)
	subnetIDs := [][]byte{subnetID[:]}
	msg, err := UncompressingBuilder.Version(
		networkID,
		myTime,
		ip,
		myVersionStr,
		myVersionTime,
		sig,
		[]ids.ID{subnetID},
	)
	require.NoError(t, err)
	require.NotNil(t, msg)
	require.Equal(t, Version, msg.Op())

	parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)

	require.NoError(t, err)
	require.NotNil(t, parsedMsg)
	require.Equal(t, Version, parsedMsg.Op())
	require.EqualValues(t, networkID, parsedMsg.Get(NetworkID))
	require.EqualValues(t, myTime, parsedMsg.Get(MyTime))
	require.EqualValues(t, ip, parsedMsg.Get(IP))
	require.EqualValues(t, myVersionStr, parsedMsg.Get(VersionStr))
	require.EqualValues(t, myVersionTime, parsedMsg.Get(VersionTime))
	require.EqualValues(t, sig, parsedMsg.Get(SigBytes))
	require.EqualValues(t, subnetIDs, parsedMsg.Get(TrackedSubnets))
}

func TestBuildGetAcceptedFrontier(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	deadline := uint64(15)

	msg, err := UncompressingBuilder.GetAcceptedFrontier(chainID, requestID, time.Duration(deadline))
	require.NoError(t, err)
	require.NotNil(t, msg)
	require.Equal(t, GetAcceptedFrontier, msg.Op())

	parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
	require.NoError(t, err)
	require.NotNil(t, parsedMsg)
	require.Equal(t, GetAcceptedFrontier, parsedMsg.Op())
	require.Equal(t, chainID[:], parsedMsg.Get(ChainID))
	require.Equal(t, requestID, parsedMsg.Get(RequestID))
	require.Equal(t, deadline, parsedMsg.Get(Deadline))
}

func TestBuildAcceptedFrontier(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	containerID := ids.Empty.Prefix(1)
	containerIDs := [][]byte{containerID[:]}

	msg, err := UncompressingBuilder.AcceptedFrontier(chainID, requestID, []ids.ID{containerID})
	require.NoError(t, err)
	require.NotNil(t, msg)
	require.Equal(t, AcceptedFrontier, msg.Op())

	parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
	require.NoError(t, err)
	require.NotNil(t, parsedMsg)
	require.Equal(t, AcceptedFrontier, parsedMsg.Op())
	require.Equal(t, chainID[:], parsedMsg.Get(ChainID))
	require.Equal(t, requestID, parsedMsg.Get(RequestID))
	require.Equal(t, containerIDs, parsedMsg.Get(ContainerIDs))
}

func TestBuildGetAccepted(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	deadline := uint64(15)
	containerID := ids.Empty.Prefix(1)
	containerIDs := [][]byte{containerID[:]}

	msg, err := UncompressingBuilder.GetAccepted(chainID, requestID, time.Duration(deadline), []ids.ID{containerID})
	require.NoError(t, err)
	require.NotNil(t, msg)
	require.Equal(t, GetAccepted, msg.Op())

	parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
	require.NoError(t, err)
	require.NotNil(t, parsedMsg)
	require.Equal(t, GetAccepted, parsedMsg.Op())
	require.Equal(t, chainID[:], parsedMsg.Get(ChainID))
	require.Equal(t, requestID, parsedMsg.Get(RequestID))
	require.Equal(t, deadline, parsedMsg.Get(Deadline))
	require.Equal(t, containerIDs, parsedMsg.Get(ContainerIDs))
}

func TestBuildAccepted(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	containerID := ids.Empty.Prefix(1)
	containerIDs := [][]byte{containerID[:]}

	msg, err := UncompressingBuilder.Accepted(chainID, requestID, []ids.ID{containerID})
	require.NoError(t, err)
	require.NotNil(t, msg)
	require.Equal(t, Accepted, msg.Op())

	parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
	require.NoError(t, err)
	require.NotNil(t, parsedMsg)
	require.Equal(t, Accepted, parsedMsg.Op())
	require.Equal(t, chainID[:], parsedMsg.Get(ChainID))
	require.Equal(t, requestID, parsedMsg.Get(RequestID))
	require.Equal(t, containerIDs, parsedMsg.Get(ContainerIDs))
}

func TestBuildGet(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	deadline := uint64(15)
	containerID := ids.Empty.Prefix(1)

	msg, err := UncompressingBuilder.Get(chainID, requestID, time.Duration(deadline), containerID)
	require.NoError(t, err)
	require.NotNil(t, msg)
	require.Equal(t, Get, msg.Op())

	parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
	require.NoError(t, err)
	require.NotNil(t, parsedMsg)
	require.Equal(t, Get, parsedMsg.Op())
	require.Equal(t, chainID[:], parsedMsg.Get(ChainID))
	require.Equal(t, requestID, parsedMsg.Get(RequestID))
	require.Equal(t, deadline, parsedMsg.Get(Deadline))
	require.Equal(t, containerID[:], parsedMsg.Get(ContainerID))
}

func TestBuildPut(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	containerID := ids.Empty.Prefix(1)
	container := []byte{2}

	for _, compress := range []bool{false, true} {
		builder := NewOutboundBuilder(TestCodec, compress)
		msg, err := builder.Put(chainID, requestID, containerID, container)
		require.NoError(t, err)
		require.NotNil(t, msg)
		require.Equal(t, Put, msg.Op())

		parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
		require.NoError(t, err)
		require.NotNil(t, parsedMsg)
		require.Equal(t, Put, parsedMsg.Op())
		require.Equal(t, chainID[:], parsedMsg.Get(ChainID))
		require.Equal(t, requestID, parsedMsg.Get(RequestID))
		require.Equal(t, containerID[:], parsedMsg.Get(ContainerID))
		require.Equal(t, container, parsedMsg.Get(ContainerBytes))
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
		require.NoError(t, err)
		require.NotNil(t, msg)
		require.Equal(t, PushQuery, msg.Op())

		parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
		require.NoError(t, err)
		require.NotNil(t, parsedMsg)
		require.Equal(t, PushQuery, parsedMsg.Op())
		require.Equal(t, chainID[:], parsedMsg.Get(ChainID))
		require.Equal(t, requestID, parsedMsg.Get(RequestID))
		require.Equal(t, deadline, parsedMsg.Get(Deadline))
		require.Equal(t, containerID[:], parsedMsg.Get(ContainerID))
		require.Equal(t, container, parsedMsg.Get(ContainerBytes))
	}
}

func TestBuildPullQuery(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	deadline := uint64(15)
	containerID := ids.Empty.Prefix(1)

	msg, err := UncompressingBuilder.PullQuery(chainID, requestID, time.Duration(deadline), containerID)
	require.NoError(t, err)
	require.NotNil(t, msg)
	require.Equal(t, PullQuery, msg.Op())

	parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
	require.NoError(t, err)
	require.NotNil(t, parsedMsg)
	require.Equal(t, PullQuery, parsedMsg.Op())
	require.Equal(t, chainID[:], parsedMsg.Get(ChainID))
	require.Equal(t, requestID, parsedMsg.Get(RequestID))
	require.Equal(t, deadline, parsedMsg.Get(Deadline))
	require.Equal(t, containerID[:], parsedMsg.Get(ContainerID))
}

func TestBuildChits(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	containerID := ids.Empty.Prefix(1)
	containerIDs := [][]byte{containerID[:]}

	msg, err := UncompressingBuilder.Chits(chainID, requestID, []ids.ID{containerID})
	require.NoError(t, err)
	require.NotNil(t, msg)
	require.Equal(t, Chits, msg.Op())

	parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
	require.NoError(t, err)
	require.NotNil(t, parsedMsg)
	require.Equal(t, Chits, parsedMsg.Op())
	require.Equal(t, chainID[:], parsedMsg.Get(ChainID))
	require.Equal(t, requestID, parsedMsg.Get(RequestID))
	require.Equal(t, containerIDs, parsedMsg.Get(ContainerIDs))
}

func TestBuildChitsV2(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	containerID := ids.Empty.Prefix(1)
	containerIDs := [][]byte{containerID[:]}

	msg := TestInboundMsgBuilder.InboundChitsV2(chainID, requestID, []ids.ID{containerID}, containerID, dummyNodeID)
	require.NotNil(t, msg)
	require.Equal(t, ChitsV2, msg.Op())
	require.Equal(t, chainID[:], msg.Get(ChainID))
	require.Equal(t, requestID, msg.Get(RequestID))
	require.Equal(t, containerIDs, msg.Get(ContainerIDs))
	require.Equal(t, containerID[:], msg.Get(ContainerID))

	outboundMsg, err := UncompressingBuilder.ChitsV2(chainID, requestID, []ids.ID{containerID}, containerID)
	require.NoError(t, err)
	require.NotNil(t, outboundMsg)
	require.Equal(t, ChitsV2, outboundMsg.Op())

	parsedMsg, err := TestCodec.Parse(outboundMsg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
	require.NoError(t, err)
	require.NotNil(t, parsedMsg)
	require.Equal(t, ChitsV2, parsedMsg.Op())
	require.Equal(t, chainID[:], parsedMsg.Get(ChainID))
	require.Equal(t, requestID, parsedMsg.Get(RequestID))
	require.Equal(t, containerIDs, parsedMsg.Get(ContainerIDs))
	require.Equal(t, containerID[:], parsedMsg.Get(ContainerID))
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
		require.NoError(t, err)
		require.NotNil(t, msg)
		require.Equal(t, Ancestors, msg.Op())

		parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
		require.NoError(t, err)
		require.NotNil(t, parsedMsg)
		require.Equal(t, Ancestors, parsedMsg.Op())
		require.Equal(t, chainID[:], parsedMsg.Get(ChainID))
		require.Equal(t, requestID, parsedMsg.Get(RequestID))
		require.Equal(t, containers, parsedMsg.Get(MultiContainerBytes))
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
		require.NoError(t, err)
		require.NotNil(t, msg)
		require.Equal(t, AppRequest, msg.Op())

		parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
		require.NoError(t, err)
		require.NotNil(t, parsedMsg)
		require.Equal(t, AppRequest, parsedMsg.Op())
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
		require.NoError(t, err)
		require.NotNil(t, msg)
		require.Equal(t, AppResponse, msg.Op())

		parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
		require.NoError(t, err)
		require.NotNil(t, msg)
		require.Equal(t, AppResponse, msg.Op())
		require.EqualValues(t, 1, parsedMsg.Get(RequestID))
		require.Equal(t, appResponseBytes, parsedMsg.Get(AppBytes))
		require.Equal(t, chainID[:], parsedMsg.Get(ChainID))
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
		require.NoError(t, err)
		require.NotNil(t, msg)
		require.Equal(t, AppGossip, msg.Op())

		parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
		require.NoError(t, err)
		require.NotNil(t, msg)
		require.Equal(t, AppGossip, msg.Op())
		require.Equal(t, appGossipBytes, parsedMsg.Get(AppBytes))
		require.Equal(t, chainID[:], parsedMsg.Get(ChainID))
	}
}
