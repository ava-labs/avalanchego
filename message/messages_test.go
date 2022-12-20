// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"bytes"
	"net"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"

	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
)

func TestMessage(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	mb, err := newMsgBuilder(
		"test",
		prometheus.NewRegistry(),
		5*time.Second,
	)
	require.NoError(err)

	testID := ids.GenerateTestID()
	compressibleContainers := [][]byte{
		bytes.Repeat([]byte{0}, 100),
		bytes.Repeat([]byte{0}, 32),
		bytes.Repeat([]byte{0}, 32),
	}

	testCertRaw, testKeyRaw, err := staking.NewCertAndKeyBytes()
	require.NoError(err)

	testTLSCert, err := staking.LoadTLSCertFromBytes(testKeyRaw, testCertRaw)
	require.NoError(err)

	nowUnix := time.Now().Unix()

	tests := []struct {
		desc             string
		op               Op
		msg              *p2ppb.Message
		gzipCompress     bool
		bypassThrottling bool
		bytesSaved       bool // if true, outbound message saved bytes must be non-zero
	}{
		{
			desc: "ping message with no compression",
			op:   PingOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_Ping{
					Ping: &p2ppb.Ping{},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "pong message with no compression no subnet uptimes",
			op:   PongOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_Pong{
					Pong: &p2ppb.Pong{
						Uptime: 100,
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "pong message with no compression and subnet uptimes",
			op:   PongOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_Pong{
					Pong: &p2ppb.Pong{
						Uptime: 100,
						SubnetUptimes: []*p2ppb.SubnetUptime{
							{
								SubnetId: testID[:],
								Uptime:   100,
							},
						},
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "version message with no compression",
			op:   VersionOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_Version{
					Version: &p2ppb.Version{
						NetworkId:      uint32(1337),
						MyTime:         uint64(nowUnix),
						IpAddr:         []byte(net.IPv6zero),
						IpPort:         9651,
						MyVersion:      "v1.2.3",
						MyVersionTime:  uint64(nowUnix),
						Sig:            []byte{'y', 'e', 'e', 't'},
						TrackedSubnets: [][]byte{testID[:]},
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "peer_list message with no compression",
			op:   PeerListOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_PeerList{
					PeerList: &p2ppb.PeerList{
						ClaimedIpPorts: []*p2ppb.ClaimedIpPort{
							{
								X509Certificate: testTLSCert.Certificate[0],
								IpAddr:          []byte(net.IPv4zero),
								IpPort:          10,
								Timestamp:       1,
								Signature:       []byte{0},
							},
						},
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "peer_list message with compression",
			op:   PeerListOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_PeerList{
					PeerList: &p2ppb.PeerList{
						ClaimedIpPorts: []*p2ppb.ClaimedIpPort{
							{
								X509Certificate: testTLSCert.Certificate[0],
								IpAddr:          []byte(net.IPv6zero),
								IpPort:          9651,
								Timestamp:       uint64(nowUnix),
								Signature:       compressibleContainers[0],
							},
						},
					},
				},
			},
			gzipCompress:     true,
			bypassThrottling: true,
			bytesSaved:       true,
		},
		{
			desc: "peer_list_ack message with no compression",
			op:   PeerListAckOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_PeerListAck{
					PeerListAck: &p2ppb.PeerListAck{
						PeerAcks: []*p2ppb.PeerAck{
							{
								TxId:      testID[:],
								Timestamp: 1,
							},
						},
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: false,
			bytesSaved:       false,
		},
		{
			desc: "get_state_summary_frontier message with no compression",
			op:   GetStateSummaryFrontierOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_GetStateSummaryFrontier{
					GetStateSummaryFrontier: &p2ppb.GetStateSummaryFrontier{
						ChainId:   testID[:],
						RequestId: 1,
						Deadline:  1,
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "state_summary_frontier message with no compression",
			op:   StateSummaryFrontierOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_StateSummaryFrontier_{
					StateSummaryFrontier_: &p2ppb.StateSummaryFrontier{
						ChainId:   testID[:],
						RequestId: 1,
						Summary:   []byte{0},
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "state_summary_frontier message with compression",
			op:   StateSummaryFrontierOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_StateSummaryFrontier_{
					StateSummaryFrontier_: &p2ppb.StateSummaryFrontier{
						ChainId:   testID[:],
						RequestId: 1,
						Summary:   compressibleContainers[0],
					},
				},
			},
			gzipCompress:     true,
			bypassThrottling: true,
			bytesSaved:       true,
		},
		{
			desc: "get_accepted_state_summary message with no compression",
			op:   GetAcceptedStateSummaryOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_GetAcceptedStateSummary{
					GetAcceptedStateSummary: &p2ppb.GetAcceptedStateSummary{
						ChainId:   testID[:],
						RequestId: 1,
						Deadline:  1,
						Heights:   []uint64{0},
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "get_accepted_state_summary message with compression",
			op:   GetAcceptedStateSummaryOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_GetAcceptedStateSummary{
					GetAcceptedStateSummary: &p2ppb.GetAcceptedStateSummary{
						ChainId:   testID[:],
						RequestId: 1,
						Deadline:  1,
						Heights:   []uint64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
					},
				},
			},
			gzipCompress:     true,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "accepted_state_summary message with no compression",
			op:   AcceptedStateSummaryOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_AcceptedStateSummary_{
					AcceptedStateSummary_: &p2ppb.AcceptedStateSummary{
						ChainId:    testID[:],
						RequestId:  1,
						SummaryIds: [][]byte{testID[:], testID[:]},
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "accepted_state_summary message with compression",
			op:   AcceptedStateSummaryOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_AcceptedStateSummary_{
					AcceptedStateSummary_: &p2ppb.AcceptedStateSummary{
						ChainId:    testID[:],
						RequestId:  1,
						SummaryIds: [][]byte{testID[:], testID[:], testID[:], testID[:], testID[:], testID[:], testID[:], testID[:], testID[:]},
					},
				},
			},
			gzipCompress:     true,
			bypassThrottling: true,
			bytesSaved:       true,
		},
		{
			desc: "get_accepted_frontier message with no compression",
			op:   GetAcceptedFrontierOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_GetAcceptedFrontier{
					GetAcceptedFrontier: &p2ppb.GetAcceptedFrontier{
						ChainId:   testID[:],
						RequestId: 1,
						Deadline:  1,
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "accepted_frontier message with no compression",
			op:   AcceptedFrontierOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_AcceptedFrontier_{
					AcceptedFrontier_: &p2ppb.AcceptedFrontier{
						ChainId:      testID[:],
						RequestId:    1,
						ContainerIds: [][]byte{testID[:], testID[:]},
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "get_accepted message with no compression",
			op:   GetAcceptedOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_GetAccepted{
					GetAccepted: &p2ppb.GetAccepted{
						ChainId:      testID[:],
						RequestId:    1,
						Deadline:     1,
						ContainerIds: [][]byte{testID[:], testID[:]},
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "accepted message with no compression",
			op:   AcceptedOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_Accepted_{
					Accepted_: &p2ppb.Accepted{
						ChainId:      testID[:],
						RequestId:    1,
						ContainerIds: [][]byte{testID[:], testID[:]},
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "get_ancestors message with no compression",
			op:   GetAncestorsOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_GetAncestors{
					GetAncestors: &p2ppb.GetAncestors{
						ChainId:     testID[:],
						RequestId:   1,
						Deadline:    1,
						ContainerId: testID[:],
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "ancestors message with no compression",
			op:   AncestorsOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_Ancestors_{
					Ancestors_: &p2ppb.Ancestors{
						ChainId:    testID[:],
						RequestId:  12345,
						Containers: compressibleContainers,
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "ancestors message with compression",
			op:   AncestorsOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_Ancestors_{
					Ancestors_: &p2ppb.Ancestors{
						ChainId:    testID[:],
						RequestId:  12345,
						Containers: compressibleContainers,
					},
				},
			},
			gzipCompress:     true,
			bypassThrottling: true,
			bytesSaved:       true,
		},
		{
			desc: "get message with no compression",
			op:   GetOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_Get{
					Get: &p2ppb.Get{
						ChainId:     testID[:],
						RequestId:   1,
						Deadline:    1,
						ContainerId: testID[:],
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "put message with no compression",
			op:   PutOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_Put{
					Put: &p2ppb.Put{
						ChainId:   testID[:],
						RequestId: 1,
						Container: []byte{0},
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "put message with compression",
			op:   PutOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_Put{
					Put: &p2ppb.Put{
						ChainId:   testID[:],
						RequestId: 1,
						Container: compressibleContainers[0],
					},
				},
			},
			gzipCompress:     true,
			bypassThrottling: true,
			bytesSaved:       true,
		},
		{
			desc: "push_query message with no compression",
			op:   PushQueryOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_PushQuery{
					PushQuery: &p2ppb.PushQuery{
						ChainId:   testID[:],
						RequestId: 1,
						Deadline:  1,
						Container: []byte{0},
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "push_query message with compression",
			op:   PushQueryOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_PushQuery{
					PushQuery: &p2ppb.PushQuery{
						ChainId:   testID[:],
						RequestId: 1,
						Deadline:  1,
						Container: compressibleContainers[0],
					},
				},
			},
			gzipCompress:     true,
			bypassThrottling: true,
			bytesSaved:       true,
		},
		{
			desc: "pull_query message with no compression",
			op:   PullQueryOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_PullQuery{
					PullQuery: &p2ppb.PullQuery{
						ChainId:     testID[:],
						RequestId:   1,
						Deadline:    1,
						ContainerId: testID[:],
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "chits message with no compression",
			op:   ChitsOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_Chits{
					Chits: &p2ppb.Chits{
						ChainId:      testID[:],
						RequestId:    1,
						ContainerIds: [][]byte{testID[:], testID[:]},
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "app_request message with no compression",
			op:   AppRequestOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_AppRequest{
					AppRequest: &p2ppb.AppRequest{
						ChainId:   testID[:],
						RequestId: 1,
						Deadline:  1,
						AppBytes:  compressibleContainers[0],
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "app_request message with compression",
			op:   AppRequestOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_AppRequest{
					AppRequest: &p2ppb.AppRequest{
						ChainId:   testID[:],
						RequestId: 1,
						Deadline:  1,
						AppBytes:  compressibleContainers[0],
					},
				},
			},
			gzipCompress:     true,
			bypassThrottling: true,
			bytesSaved:       true,
		},
		{
			desc: "app_response message with no compression",
			op:   AppResponseOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_AppResponse{
					AppResponse: &p2ppb.AppResponse{
						ChainId:   testID[:],
						RequestId: 1,
						AppBytes:  compressibleContainers[0],
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "app_response message with compression",
			op:   AppResponseOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_AppResponse{
					AppResponse: &p2ppb.AppResponse{
						ChainId:   testID[:],
						RequestId: 1,
						AppBytes:  compressibleContainers[0],
					},
				},
			},
			gzipCompress:     true,
			bypassThrottling: true,
			bytesSaved:       true,
		},
		{
			desc: "app_gossip message with no compression",
			op:   AppGossipOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_AppGossip{
					AppGossip: &p2ppb.AppGossip{
						ChainId:  testID[:],
						AppBytes: compressibleContainers[0],
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "app_gossip message with compression",
			op:   AppGossipOp,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_AppGossip{
					AppGossip: &p2ppb.AppGossip{
						ChainId:  testID[:],
						AppBytes: compressibleContainers[0],
					},
				},
			},
			gzipCompress:     true,
			bypassThrottling: true,
			bytesSaved:       true,
		},
	}

	for _, tv := range tests {
		require.True(t.Run(tv.desc, func(t2 *testing.T) {
			encodedMsg, err := mb.createOutbound(tv.msg, tv.gzipCompress, tv.bypassThrottling)
			require.NoError(err)

			require.Equal(tv.bypassThrottling, encodedMsg.BypassThrottling())
			require.Equal(tv.op, encodedMsg.Op())

			bytesSaved := encodedMsg.BytesSavedCompression()
			require.Equal(tv.bytesSaved, bytesSaved > 0)

			parsedMsg, err := mb.parseInbound(encodedMsg.Bytes(), ids.EmptyNodeID, func() {})
			require.NoError(err)
			require.Equal(tv.op, parsedMsg.Op())
		}))
	}
}

func TestEmptyInboundMessage(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	mb, err := newMsgBuilder(
		"test",
		prometheus.NewRegistry(),
		5*time.Second,
	)
	require.NoError(err)

	msg := &p2ppb.Message{}
	msgBytes, err := proto.Marshal(msg)
	require.NoError(err)

	_, err = mb.parseInbound(msgBytes, ids.EmptyNodeID, func() {})
	require.ErrorIs(err, errUnknownMessageType)
}

func TestNilInboundMessage(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	mb, err := newMsgBuilder(
		"test",
		prometheus.NewRegistry(),
		5*time.Second,
	)
	require.NoError(err)

	msg := &p2ppb.Message{
		Message: &p2ppb.Message_Ping{
			Ping: nil,
		},
	}
	msgBytes, err := proto.Marshal(msg)
	require.NoError(err)

	parsedMsg, err := mb.parseInbound(msgBytes, ids.EmptyNodeID, func() {})
	require.NoError(err)

	pingMsg, ok := parsedMsg.message.(*p2ppb.Ping)
	require.True(ok)
	require.NotNil(pingMsg)
}
