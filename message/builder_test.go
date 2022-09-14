// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
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
	UncompressingBuilder = NewOutboundBuilderWithPacker(codec, false /*compress*/)
	TestInboundMsgBuilder = NewInboundBuilderWithPacker(codec)
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

	networkIDInf, err := parsedMsg.Get(NetworkID)
	require.NoError(t, err)
	require.EqualValues(t, networkID, networkIDInf)

	myTimeInf, err := parsedMsg.Get(MyTime)
	require.NoError(t, err)
	require.EqualValues(t, myTime, myTimeInf)

	ipInf, err := parsedMsg.Get(IP)
	require.NoError(t, err)
	require.EqualValues(t, ip, ipInf)

	versionStrInf, err := parsedMsg.Get(VersionStr)
	require.NoError(t, err)
	require.EqualValues(t, myVersionStr, versionStrInf)

	versionTimeInf, err := parsedMsg.Get(VersionTime)
	require.NoError(t, err)
	require.EqualValues(t, myVersionTime, versionTimeInf)

	sigBytesInf, err := parsedMsg.Get(SigBytes)
	require.NoError(t, err)
	require.EqualValues(t, sig, sigBytesInf)

	trackedSubnetsInf, err := parsedMsg.Get(TrackedSubnets)
	require.NoError(t, err)
	require.EqualValues(t, subnetIDs, trackedSubnetsInf)
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

	chainIDInf, err := parsedMsg.Get(ChainID)
	require.NoError(t, err)
	require.Equal(t, chainID[:], chainIDInf)

	requestIDInf, err := parsedMsg.Get(RequestID)
	require.NoError(t, err)
	require.Equal(t, requestID, requestIDInf)

	deadlineInf, err := parsedMsg.Get(Deadline)
	require.NoError(t, err)
	require.Equal(t, deadline, deadlineInf)
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

	chainIDInf, err := parsedMsg.Get(ChainID)
	require.NoError(t, err)
	require.Equal(t, chainID[:], chainIDInf)

	requestIDInf, err := parsedMsg.Get(RequestID)
	require.NoError(t, err)
	require.Equal(t, requestID, requestIDInf)

	containerIDsInf, err := parsedMsg.Get(ContainerIDs)
	require.NoError(t, err)
	require.Equal(t, containerIDs, containerIDsInf)
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

	chainIDInf, err := parsedMsg.Get(ChainID)
	require.NoError(t, err)
	require.Equal(t, chainID[:], chainIDInf)

	requestIDInf, err := parsedMsg.Get(RequestID)
	require.NoError(t, err)
	require.Equal(t, requestID, requestIDInf)

	deadlineInf, err := parsedMsg.Get(Deadline)
	require.NoError(t, err)
	require.Equal(t, deadline, deadlineInf)

	containerIDsInf, err := parsedMsg.Get(ContainerIDs)
	require.NoError(t, err)
	require.Equal(t, containerIDs, containerIDsInf)
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

	chainIDInf, err := parsedMsg.Get(ChainID)
	require.NoError(t, err)
	require.Equal(t, chainID[:], chainIDInf)

	requestIDInf, err := parsedMsg.Get(RequestID)
	require.NoError(t, err)
	require.Equal(t, requestID, requestIDInf)

	containerIDsInf, err := parsedMsg.Get(ContainerIDs)
	require.NoError(t, err)
	require.Equal(t, containerIDs, containerIDsInf)
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

	chainIDInf, err := parsedMsg.Get(ChainID)
	require.NoError(t, err)
	require.Equal(t, chainID[:], chainIDInf)

	requestIDInf, err := parsedMsg.Get(RequestID)
	require.NoError(t, err)
	require.Equal(t, requestID, requestIDInf)

	deadlineInf, err := parsedMsg.Get(Deadline)
	require.NoError(t, err)
	require.Equal(t, deadline, deadlineInf)

	containerIDInf, err := parsedMsg.Get(ContainerID)
	require.NoError(t, err)
	require.Equal(t, containerID[:], containerIDInf)
}

func TestBuildPut(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	containerID := ids.Empty.Prefix(1)
	container := []byte{2}

	for _, compress := range []bool{false, true} {
		builder := NewOutboundBuilderWithPacker(TestCodec, compress)
		msg, err := builder.Put(chainID, requestID, containerID, container)
		require.NoError(t, err)
		require.NotNil(t, msg)
		require.Equal(t, Put, msg.Op())

		parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
		require.NoError(t, err)
		require.NotNil(t, parsedMsg)
		require.Equal(t, Put, parsedMsg.Op())

		chainIDInf, err := parsedMsg.Get(ChainID)
		require.NoError(t, err)
		require.Equal(t, chainID[:], chainIDInf)

		requestIDInf, err := parsedMsg.Get(RequestID)
		require.NoError(t, err)
		require.Equal(t, requestID, requestIDInf)

		containerIDInf, err := parsedMsg.Get(ContainerID)
		require.NoError(t, err)
		require.Equal(t, containerID[:], containerIDInf)

		containerInf, err := parsedMsg.Get(ContainerBytes)
		require.NoError(t, err)
		require.Equal(t, container, containerInf)
	}
}

func TestBuildPushQuery(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	deadline := uint64(15)
	containerID := ids.Empty.Prefix(1)
	container := []byte{2}

	for _, compress := range []bool{false, true} {
		builder := NewOutboundBuilderWithPacker(TestCodec, compress)
		msg, err := builder.PushQuery(chainID, requestID, time.Duration(deadline), containerID, container)
		require.NoError(t, err)
		require.NotNil(t, msg)
		require.Equal(t, PushQuery, msg.Op())

		parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
		require.NoError(t, err)
		require.NotNil(t, parsedMsg)
		require.Equal(t, PushQuery, parsedMsg.Op())

		chainIDInf, err := parsedMsg.Get(ChainID)
		require.NoError(t, err)
		require.Equal(t, chainID[:], chainIDInf)

		requestIDInf, err := parsedMsg.Get(RequestID)
		require.NoError(t, err)
		require.Equal(t, requestID, requestIDInf)

		deadlineInf, err := parsedMsg.Get(Deadline)
		require.NoError(t, err)
		require.Equal(t, deadline, deadlineInf)

		containerIDInf, err := parsedMsg.Get(ContainerID)
		require.NoError(t, err)
		require.Equal(t, containerID[:], containerIDInf)

		containerInf, err := parsedMsg.Get(ContainerBytes)
		require.NoError(t, err)
		require.Equal(t, container, containerInf)
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

	chainIDInf, err := parsedMsg.Get(ChainID)
	require.NoError(t, err)
	require.Equal(t, chainID[:], chainIDInf)

	requestIDInf, err := parsedMsg.Get(RequestID)
	require.NoError(t, err)
	require.Equal(t, requestID, requestIDInf)

	deadlineInf, err := parsedMsg.Get(Deadline)
	require.NoError(t, err)
	require.Equal(t, deadline, deadlineInf)

	containerIDInf, err := parsedMsg.Get(ContainerID)
	require.NoError(t, err)
	require.Equal(t, containerID[:], containerIDInf)
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

	chainIDInf, err := parsedMsg.Get(ChainID)
	require.NoError(t, err)
	require.Equal(t, chainID[:], chainIDInf)

	requestIDInf, err := parsedMsg.Get(RequestID)
	require.NoError(t, err)
	require.Equal(t, requestID, requestIDInf)

	containerIDsInf, err := parsedMsg.Get(ContainerIDs)
	require.NoError(t, err)
	require.Equal(t, containerIDs, containerIDsInf)
}

func TestBuildChitsV2(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	containerID := ids.Empty.Prefix(1)
	containerIDs := [][]byte{containerID[:]}

	msg := TestInboundMsgBuilder.InboundChitsV2(chainID, requestID, []ids.ID{containerID}, containerID, dummyNodeID)
	require.NotNil(t, msg)
	require.Equal(t, ChitsV2, msg.Op())

	chainIDInf, err := msg.Get(ChainID)
	require.NoError(t, err)
	require.Equal(t, chainID[:], chainIDInf)

	requestIDInf, err := msg.Get(RequestID)
	require.NoError(t, err)
	require.Equal(t, requestID, requestIDInf)

	containerIDsInf, err := msg.Get(ContainerIDs)
	require.NoError(t, err)
	require.Equal(t, containerIDs, containerIDsInf)

	containerIDInf, err := msg.Get(ContainerID)
	require.NoError(t, err)
	require.Equal(t, containerID[:], containerIDInf)

	outboundMsg, err := UncompressingBuilder.ChitsV2(chainID, requestID, []ids.ID{containerID}, containerID)
	require.NoError(t, err)
	require.NotNil(t, outboundMsg)
	require.Equal(t, ChitsV2, outboundMsg.Op())

	parsedMsg, err := TestCodec.Parse(outboundMsg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
	require.NoError(t, err)
	require.NotNil(t, parsedMsg)
	require.Equal(t, ChitsV2, parsedMsg.Op())

	chainIDInf, err = parsedMsg.Get(ChainID)
	require.NoError(t, err)
	require.Equal(t, chainID[:], chainIDInf)

	requestIDInf, err = parsedMsg.Get(RequestID)
	require.NoError(t, err)
	require.Equal(t, requestID, requestIDInf)

	containerIDsInf, err = parsedMsg.Get(ContainerIDs)
	require.NoError(t, err)
	require.Equal(t, containerIDs, containerIDsInf)

	containerIDInf, err = parsedMsg.Get(ContainerID)
	require.NoError(t, err)
	require.Equal(t, containerID[:], containerIDInf)
}

func TestBuildAncestors(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	container := ids.Empty.Prefix(1)
	container2 := ids.Empty.Prefix(2)
	containers := [][]byte{container[:], container2[:]}

	for _, compress := range []bool{false, true} {
		builder := NewOutboundBuilderWithPacker(TestCodec, compress)
		msg, err := builder.Ancestors(chainID, requestID, containers)
		require.NoError(t, err)
		require.NotNil(t, msg)
		require.Equal(t, Ancestors, msg.Op())

		parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
		require.NoError(t, err)
		require.NotNil(t, parsedMsg)
		require.Equal(t, Ancestors, parsedMsg.Op())

		chainIDInf, err := parsedMsg.Get(ChainID)
		require.NoError(t, err)
		require.Equal(t, chainID[:], chainIDInf)

		requestIDInf, err := parsedMsg.Get(RequestID)
		require.NoError(t, err)
		require.Equal(t, requestID, requestIDInf)

		multiContainerBytesInf, err := parsedMsg.Get(MultiContainerBytes)
		require.NoError(t, err)
		require.Equal(t, containers, multiContainerBytesInf)
	}
}

func TestBuildAppRequestMsg(t *testing.T) {
	chainID := ids.GenerateTestID()
	appRequestBytes := make([]byte, 1024)
	appRequestBytes[0] = 1
	appRequestBytes[len(appRequestBytes)-1] = 1
	deadline := uint64(time.Now().Unix())

	for _, compress := range []bool{false, true} {
		builder := NewOutboundBuilderWithPacker(TestCodec, compress)
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
		builder := NewOutboundBuilderWithPacker(TestCodec, compress)
		msg, err := builder.AppResponse(chainID, 1, appResponseBytes)
		require.NoError(t, err)
		require.NotNil(t, msg)
		require.Equal(t, AppResponse, msg.Op())

		parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
		require.NoError(t, err)
		require.NotNil(t, msg)
		require.Equal(t, AppResponse, msg.Op())

		requestIDInf, err := parsedMsg.Get(RequestID)
		require.NoError(t, err)
		require.EqualValues(t, 1, requestIDInf)

		appBytesInf, err := parsedMsg.Get(AppBytes)
		require.NoError(t, err)
		require.Equal(t, appResponseBytes, appBytesInf)

		chainIDInf, err := parsedMsg.Get(ChainID)
		require.NoError(t, err)
		require.Equal(t, chainID[:], chainIDInf)
	}
}

func TestBuildAppGossipMsg(t *testing.T) {
	chainID := ids.GenerateTestID()
	appGossipBytes := make([]byte, 1024)
	appGossipBytes[0] = 1
	appGossipBytes[len(appGossipBytes)-1] = 1

	for _, compress := range []bool{false, true} {
		testBuilder := NewOutboundBuilderWithPacker(TestCodec, compress)
		msg, err := testBuilder.AppGossip(chainID, appGossipBytes)
		require.NoError(t, err)
		require.NotNil(t, msg)
		require.Equal(t, AppGossip, msg.Op())

		parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
		require.NoError(t, err)
		require.NotNil(t, msg)
		require.Equal(t, AppGossip, msg.Op())

		appBytesInf, err := parsedMsg.Get(AppBytes)
		require.NoError(t, err)
		require.Equal(t, appGossipBytes, appBytesInf)

		chainIDInf, err := parsedMsg.Get(ChainID)
		require.NoError(t, err)
		require.Equal(t, chainID[:], chainIDInf)
	}
}
