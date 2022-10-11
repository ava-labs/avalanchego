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

	networkIDIntf, err := parsedMsg.Get(NetworkID)
	require.NoError(t, err)
	require.EqualValues(t, networkID, networkIDIntf)

	myTimeIntf, err := parsedMsg.Get(MyTime)
	require.NoError(t, err)
	require.EqualValues(t, myTime, myTimeIntf)

	ipIntf, err := parsedMsg.Get(IP)
	require.NoError(t, err)
	require.EqualValues(t, ip, ipIntf)

	versionStrIntf, err := parsedMsg.Get(VersionStr)
	require.NoError(t, err)
	require.EqualValues(t, myVersionStr, versionStrIntf)

	versionTimeIntf, err := parsedMsg.Get(VersionTime)
	require.NoError(t, err)
	require.EqualValues(t, myVersionTime, versionTimeIntf)

	sigBytesIntf, err := parsedMsg.Get(SigBytes)
	require.NoError(t, err)
	require.EqualValues(t, sig, sigBytesIntf)

	trackedSubnetsIntf, err := parsedMsg.Get(TrackedSubnets)
	require.NoError(t, err)
	require.EqualValues(t, subnetIDs, trackedSubnetsIntf)
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

	chainIDIntf, err := parsedMsg.Get(ChainID)
	require.NoError(t, err)
	require.Equal(t, chainID[:], chainIDIntf)

	requestIDIntf, err := parsedMsg.Get(RequestID)
	require.NoError(t, err)
	require.Equal(t, requestID, requestIDIntf)

	deadlineIntf, err := parsedMsg.Get(Deadline)
	require.NoError(t, err)
	require.Equal(t, deadline, deadlineIntf)
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

	chainIDIntf, err := parsedMsg.Get(ChainID)
	require.NoError(t, err)
	require.Equal(t, chainID[:], chainIDIntf)

	requestIDIntf, err := parsedMsg.Get(RequestID)
	require.NoError(t, err)
	require.Equal(t, requestID, requestIDIntf)

	containerIDsIntf, err := parsedMsg.Get(ContainerIDs)
	require.NoError(t, err)
	require.Equal(t, containerIDs, containerIDsIntf)
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

	chainIDIntf, err := parsedMsg.Get(ChainID)
	require.NoError(t, err)
	require.Equal(t, chainID[:], chainIDIntf)

	requestIDIntf, err := parsedMsg.Get(RequestID)
	require.NoError(t, err)
	require.Equal(t, requestID, requestIDIntf)

	deadlineIntf, err := parsedMsg.Get(Deadline)
	require.NoError(t, err)
	require.Equal(t, deadline, deadlineIntf)

	containerIDsIntf, err := parsedMsg.Get(ContainerIDs)
	require.NoError(t, err)
	require.Equal(t, containerIDs, containerIDsIntf)
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

	chainIDIntf, err := parsedMsg.Get(ChainID)
	require.NoError(t, err)
	require.Equal(t, chainID[:], chainIDIntf)

	requestIDIntf, err := parsedMsg.Get(RequestID)
	require.NoError(t, err)
	require.Equal(t, requestID, requestIDIntf)

	containerIDsIntf, err := parsedMsg.Get(ContainerIDs)
	require.NoError(t, err)
	require.Equal(t, containerIDs, containerIDsIntf)
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

	chainIDIntf, err := parsedMsg.Get(ChainID)
	require.NoError(t, err)
	require.Equal(t, chainID[:], chainIDIntf)

	requestIDIntf, err := parsedMsg.Get(RequestID)
	require.NoError(t, err)
	require.Equal(t, requestID, requestIDIntf)

	deadlineIntf, err := parsedMsg.Get(Deadline)
	require.NoError(t, err)
	require.Equal(t, deadline, deadlineIntf)

	containerIDIntf, err := parsedMsg.Get(ContainerID)
	require.NoError(t, err)
	require.Equal(t, containerID[:], containerIDIntf)
}

func TestBuildPut(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	container := []byte{2}

	for _, compress := range []bool{false, true} {
		builder := NewOutboundBuilderWithPacker(TestCodec, compress)
		msg, err := builder.Put(chainID, requestID, container)
		require.NoError(t, err)
		require.NotNil(t, msg)
		require.Equal(t, Put, msg.Op())

		parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
		require.NoError(t, err)
		require.NotNil(t, parsedMsg)
		require.Equal(t, Put, parsedMsg.Op())

		chainIDIntf, err := parsedMsg.Get(ChainID)
		require.NoError(t, err)
		require.Equal(t, chainID[:], chainIDIntf)

		requestIDIntf, err := parsedMsg.Get(RequestID)
		require.NoError(t, err)
		require.Equal(t, requestID, requestIDIntf)

		containerIDIntf, err := parsedMsg.Get(ContainerID)
		require.NoError(t, err)
		require.Equal(t, ids.Empty[:], containerIDIntf)

		containerIntf, err := parsedMsg.Get(ContainerBytes)
		require.NoError(t, err)
		require.Equal(t, container, containerIntf)
	}
}

func TestBuildPushQuery(t *testing.T) {
	chainID := ids.Empty.Prefix(0)
	requestID := uint32(5)
	deadline := uint64(15)
	container := []byte{2}

	for _, compress := range []bool{false, true} {
		builder := NewOutboundBuilderWithPacker(TestCodec, compress)
		msg, err := builder.PushQuery(chainID, requestID, time.Duration(deadline), container)
		require.NoError(t, err)
		require.NotNil(t, msg)
		require.Equal(t, PushQuery, msg.Op())

		parsedMsg, err := TestCodec.Parse(msg.Bytes(), dummyNodeID, dummyOnFinishedHandling)
		require.NoError(t, err)
		require.NotNil(t, parsedMsg)
		require.Equal(t, PushQuery, parsedMsg.Op())

		chainIDIntf, err := parsedMsg.Get(ChainID)
		require.NoError(t, err)
		require.Equal(t, chainID[:], chainIDIntf)

		requestIDIntf, err := parsedMsg.Get(RequestID)
		require.NoError(t, err)
		require.Equal(t, requestID, requestIDIntf)

		deadlineIntf, err := parsedMsg.Get(Deadline)
		require.NoError(t, err)
		require.Equal(t, deadline, deadlineIntf)

		containerIDIntf, err := parsedMsg.Get(ContainerID)
		require.NoError(t, err)
		require.Equal(t, ids.Empty[:], containerIDIntf)

		containerIntf, err := parsedMsg.Get(ContainerBytes)
		require.NoError(t, err)
		require.Equal(t, container, containerIntf)
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

	chainIDIntf, err := parsedMsg.Get(ChainID)
	require.NoError(t, err)
	require.Equal(t, chainID[:], chainIDIntf)

	requestIDIntf, err := parsedMsg.Get(RequestID)
	require.NoError(t, err)
	require.Equal(t, requestID, requestIDIntf)

	deadlineIntf, err := parsedMsg.Get(Deadline)
	require.NoError(t, err)
	require.Equal(t, deadline, deadlineIntf)

	containerIDIntf, err := parsedMsg.Get(ContainerID)
	require.NoError(t, err)
	require.Equal(t, containerID[:], containerIDIntf)
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

	chainIDIntf, err := parsedMsg.Get(ChainID)
	require.NoError(t, err)
	require.Equal(t, chainID[:], chainIDIntf)

	requestIDIntf, err := parsedMsg.Get(RequestID)
	require.NoError(t, err)
	require.Equal(t, requestID, requestIDIntf)

	containerIDsIntf, err := parsedMsg.Get(ContainerIDs)
	require.NoError(t, err)
	require.Equal(t, containerIDs, containerIDsIntf)
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

		chainIDIntf, err := parsedMsg.Get(ChainID)
		require.NoError(t, err)
		require.Equal(t, chainID[:], chainIDIntf)

		requestIDIntf, err := parsedMsg.Get(RequestID)
		require.NoError(t, err)
		require.Equal(t, requestID, requestIDIntf)

		multiContainerBytesIntf, err := parsedMsg.Get(MultiContainerBytes)
		require.NoError(t, err)
		require.Equal(t, containers, multiContainerBytesIntf)
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

		requestIDIntf, err := parsedMsg.Get(RequestID)
		require.NoError(t, err)
		require.EqualValues(t, 1, requestIDIntf)

		appBytesIntf, err := parsedMsg.Get(AppBytes)
		require.NoError(t, err)
		require.Equal(t, appResponseBytes, appBytesIntf)

		chainIDIntf, err := parsedMsg.Get(ChainID)
		require.NoError(t, err)
		require.Equal(t, chainID[:], chainIDIntf)
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

		appBytesIntf, err := parsedMsg.Get(AppBytes)
		require.NoError(t, err)
		require.Equal(t, appGossipBytes, appBytesIntf)

		chainIDIntf, err := parsedMsg.Get(ChainID)
		require.NoError(t, err)
		require.Equal(t, chainID[:], chainIDIntf)
	}
}
