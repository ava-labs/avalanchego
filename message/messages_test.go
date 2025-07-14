// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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

	"github.com/ava-labs/avalanchego/buf/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/compression"
)

func TestMessage(t *testing.T) {
	t.Parallel()

	mb, err := newMsgBuilder(
		prometheus.NewRegistry(),
		5*time.Second,
	)
	require.NoError(t, err)

	testID := ids.GenerateTestID()
	compressibleContainers := [][]byte{
		bytes.Repeat([]byte{0}, 100),
		bytes.Repeat([]byte{0}, 32),
		bytes.Repeat([]byte{0}, 32),
	}

	testCertRaw, testKeyRaw, err := staking.NewCertAndKeyBytes()
	require.NoError(t, err)

	testTLSCert, err := staking.LoadTLSCertFromBytes(testKeyRaw, testCertRaw)
	require.NoError(t, err)

	nowUnix := time.Now().Unix()

	tests := []struct {
		desc             string
		op               Op
		msg              *p2p.Message
		compressionType  compression.Type
		bypassThrottling bool
		bytesSaved       bool // if true, outbound message saved bytes must be non-zero
	}{
		{
			desc: "ping message with no compression no uptime",
			op:   PingOp,
			msg: &p2p.Message{
				Message: &p2p.Message_Ping{
					Ping: &p2p.Ping{},
				},
			},
			compressionType:  compression.TypeNone,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "pong message with no compression",
			op:   PongOp,
			msg: &p2p.Message{
				Message: &p2p.Message_Pong{
					Pong: &p2p.Pong{},
				},
			},
			compressionType:  compression.TypeNone,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "ping message with no compression and uptime",
			op:   PingOp,
			msg: &p2p.Message{
				Message: &p2p.Message_Ping{
					Ping: &p2p.Ping{
						Uptime: 100,
					},
				},
			},
			compressionType:  compression.TypeNone,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "Handshake message with no compression",
			op:   HandshakeOp,
			msg: &p2p.Message{
				Message: &p2p.Message_Handshake{
					Handshake: &p2p.Handshake{
						NetworkId:      uint32(1337),
						MyTime:         uint64(nowUnix),
						IpAddr:         []byte(net.IPv6zero),
						IpPort:         9651,
						IpSigningTime:  uint64(nowUnix),
						IpNodeIdSig:    []byte{'y', 'e', 'e', 't'},
						TrackedSubnets: [][]byte{testID[:]},
						IpBlsSig:       []byte{'y', 'e', 'e', 't', '2'},
					},
				},
			},
			compressionType:  compression.TypeNone,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "get_peer_list message with no compression",
			op:   GetPeerListOp,
			msg: &p2p.Message{
				Message: &p2p.Message_GetPeerList{
					GetPeerList: &p2p.GetPeerList{
						KnownPeers: &p2p.BloomFilter{
							Filter: make([]byte, 2048),
							Salt:   make([]byte, 32),
						},
					},
				},
			},
			compressionType:  compression.TypeNone,
			bypassThrottling: false,
			bytesSaved:       false,
		},
		{
			desc: "get_peer_list message with zstd compression",
			op:   GetPeerListOp,
			msg: &p2p.Message{
				Message: &p2p.Message_GetPeerList{
					GetPeerList: &p2p.GetPeerList{
						KnownPeers: &p2p.BloomFilter{
							Filter: make([]byte, 2048),
							Salt:   make([]byte, 32),
						},
					},
				},
			},
			compressionType:  compression.TypeZstd,
			bypassThrottling: false,
			bytesSaved:       true,
		},
		{
			desc: "peer_list message with no compression",
			op:   PeerListOp,
			msg: &p2p.Message{
				Message: &p2p.Message_PeerList_{
					PeerList_: &p2p.PeerList{
						ClaimedIpPorts: []*p2p.ClaimedIpPort{
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
			compressionType:  compression.TypeNone,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "peer_list message with zstd compression",
			op:   PeerListOp,
			msg: &p2p.Message{
				Message: &p2p.Message_PeerList_{
					PeerList_: &p2p.PeerList{
						ClaimedIpPorts: []*p2p.ClaimedIpPort{
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
			compressionType:  compression.TypeZstd,
			bypassThrottling: true,
			bytesSaved:       true,
		},
		{
			desc: "get_state_summary_frontier message with no compression",
			op:   GetStateSummaryFrontierOp,
			msg: &p2p.Message{
				Message: &p2p.Message_GetStateSummaryFrontier{
					GetStateSummaryFrontier: &p2p.GetStateSummaryFrontier{
						ChainId:   testID[:],
						RequestId: 1,
						Deadline:  1,
					},
				},
			},
			compressionType:  compression.TypeNone,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "state_summary_frontier message with no compression",
			op:   StateSummaryFrontierOp,
			msg: &p2p.Message{
				Message: &p2p.Message_StateSummaryFrontier_{
					StateSummaryFrontier_: &p2p.StateSummaryFrontier{
						ChainId:   testID[:],
						RequestId: 1,
						Summary:   []byte{0},
					},
				},
			},
			compressionType:  compression.TypeNone,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "state_summary_frontier message with zstd compression",
			op:   StateSummaryFrontierOp,
			msg: &p2p.Message{
				Message: &p2p.Message_StateSummaryFrontier_{
					StateSummaryFrontier_: &p2p.StateSummaryFrontier{
						ChainId:   testID[:],
						RequestId: 1,
						Summary:   compressibleContainers[0],
					},
				},
			},
			compressionType:  compression.TypeZstd,
			bypassThrottling: true,
			bytesSaved:       true,
		},
		{
			desc: "get_accepted_state_summary message with no compression",
			op:   GetAcceptedStateSummaryOp,
			msg: &p2p.Message{
				Message: &p2p.Message_GetAcceptedStateSummary{
					GetAcceptedStateSummary: &p2p.GetAcceptedStateSummary{
						ChainId:   testID[:],
						RequestId: 1,
						Deadline:  1,
						Heights:   []uint64{0},
					},
				},
			},
			compressionType:  compression.TypeNone,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "get_accepted_state_summary message with zstd compression",
			op:   GetAcceptedStateSummaryOp,
			msg: &p2p.Message{
				Message: &p2p.Message_GetAcceptedStateSummary{
					GetAcceptedStateSummary: &p2p.GetAcceptedStateSummary{
						ChainId:   testID[:],
						RequestId: 1,
						Deadline:  1,
						Heights:   []uint64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
					},
				},
			},
			compressionType:  compression.TypeZstd,
			bypassThrottling: true,
			bytesSaved:       true,
		},
		{
			desc: "accepted_state_summary message with no compression",
			op:   AcceptedStateSummaryOp,
			msg: &p2p.Message{
				Message: &p2p.Message_AcceptedStateSummary_{
					AcceptedStateSummary_: &p2p.AcceptedStateSummary{
						ChainId:    testID[:],
						RequestId:  1,
						SummaryIds: [][]byte{testID[:], testID[:]},
					},
				},
			},
			compressionType:  compression.TypeNone,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "accepted_state_summary message with zstd compression",
			op:   AcceptedStateSummaryOp,
			msg: &p2p.Message{
				Message: &p2p.Message_AcceptedStateSummary_{
					AcceptedStateSummary_: &p2p.AcceptedStateSummary{
						ChainId:    testID[:],
						RequestId:  1,
						SummaryIds: [][]byte{testID[:], testID[:], testID[:], testID[:], testID[:], testID[:], testID[:], testID[:], testID[:]},
					},
				},
			},
			compressionType:  compression.TypeZstd,
			bypassThrottling: true,
			bytesSaved:       true,
		},
		{
			desc: "get_accepted_frontier message with no compression",
			op:   GetAcceptedFrontierOp,
			msg: &p2p.Message{
				Message: &p2p.Message_GetAcceptedFrontier{
					GetAcceptedFrontier: &p2p.GetAcceptedFrontier{
						ChainId:   testID[:],
						RequestId: 1,
						Deadline:  1,
					},
				},
			},
			compressionType:  compression.TypeNone,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "accepted_frontier message with no compression",
			op:   AcceptedFrontierOp,
			msg: &p2p.Message{
				Message: &p2p.Message_AcceptedFrontier_{
					AcceptedFrontier_: &p2p.AcceptedFrontier{
						ChainId:     testID[:],
						RequestId:   1,
						ContainerId: testID[:],
					},
				},
			},
			compressionType:  compression.TypeNone,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "get_accepted message with no compression",
			op:   GetAcceptedOp,
			msg: &p2p.Message{
				Message: &p2p.Message_GetAccepted{
					GetAccepted: &p2p.GetAccepted{
						ChainId:      testID[:],
						RequestId:    1,
						Deadline:     1,
						ContainerIds: [][]byte{testID[:], testID[:]},
					},
				},
			},
			compressionType:  compression.TypeNone,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "accepted message with no compression",
			op:   AcceptedOp,
			msg: &p2p.Message{
				Message: &p2p.Message_Accepted_{
					Accepted_: &p2p.Accepted{
						ChainId:      testID[:],
						RequestId:    1,
						ContainerIds: [][]byte{testID[:], testID[:]},
					},
				},
			},
			compressionType:  compression.TypeNone,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "get_ancestors message with no compression",
			op:   GetAncestorsOp,
			msg: &p2p.Message{
				Message: &p2p.Message_GetAncestors{
					GetAncestors: &p2p.GetAncestors{
						ChainId:     testID[:],
						RequestId:   1,
						Deadline:    1,
						ContainerId: testID[:],
						EngineType:  p2p.EngineType_ENGINE_TYPE_AVALANCHE,
					},
				},
			},
			compressionType:  compression.TypeNone,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "ancestors message with no compression",
			op:   AncestorsOp,
			msg: &p2p.Message{
				Message: &p2p.Message_Ancestors_{
					Ancestors_: &p2p.Ancestors{
						ChainId:    testID[:],
						RequestId:  12345,
						Containers: compressibleContainers,
					},
				},
			},
			compressionType:  compression.TypeNone,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "ancestors message with zstd compression",
			op:   AncestorsOp,
			msg: &p2p.Message{
				Message: &p2p.Message_Ancestors_{
					Ancestors_: &p2p.Ancestors{
						ChainId:    testID[:],
						RequestId:  12345,
						Containers: compressibleContainers,
					},
				},
			},
			compressionType:  compression.TypeZstd,
			bypassThrottling: true,
			bytesSaved:       true,
		},
		{
			desc: "get message with no compression",
			op:   GetOp,
			msg: &p2p.Message{
				Message: &p2p.Message_Get{
					Get: &p2p.Get{
						ChainId:     testID[:],
						RequestId:   1,
						Deadline:    1,
						ContainerId: testID[:],
					},
				},
			},
			compressionType:  compression.TypeNone,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "put message with no compression",
			op:   PutOp,
			msg: &p2p.Message{
				Message: &p2p.Message_Put{
					Put: &p2p.Put{
						ChainId:   testID[:],
						RequestId: 1,
						Container: []byte{0},
					},
				},
			},
			compressionType:  compression.TypeNone,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "put message with zstd compression",
			op:   PutOp,
			msg: &p2p.Message{
				Message: &p2p.Message_Put{
					Put: &p2p.Put{
						ChainId:   testID[:],
						RequestId: 1,
						Container: compressibleContainers[0],
					},
				},
			},
			compressionType:  compression.TypeZstd,
			bypassThrottling: true,
			bytesSaved:       true,
		},
		{
			desc: "push_query message with no compression",
			op:   PushQueryOp,
			msg: &p2p.Message{
				Message: &p2p.Message_PushQuery{
					PushQuery: &p2p.PushQuery{
						ChainId:   testID[:],
						RequestId: 1,
						Deadline:  1,
						Container: []byte{0},
					},
				},
			},
			compressionType:  compression.TypeNone,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "push_query message with zstd compression",
			op:   PushQueryOp,
			msg: &p2p.Message{
				Message: &p2p.Message_PushQuery{
					PushQuery: &p2p.PushQuery{
						ChainId:   testID[:],
						RequestId: 1,
						Deadline:  1,
						Container: compressibleContainers[0],
					},
				},
			},
			compressionType:  compression.TypeZstd,
			bypassThrottling: true,
			bytesSaved:       true,
		},
		{
			desc: "pull_query message with no compression",
			op:   PullQueryOp,
			msg: &p2p.Message{
				Message: &p2p.Message_PullQuery{
					PullQuery: &p2p.PullQuery{
						ChainId:     testID[:],
						RequestId:   1,
						Deadline:    1,
						ContainerId: testID[:],
					},
				},
			},
			compressionType:  compression.TypeNone,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "chits message with no compression",
			op:   ChitsOp,
			msg: &p2p.Message{
				Message: &p2p.Message_Chits{
					Chits: &p2p.Chits{
						ChainId:     testID[:],
						RequestId:   1,
						PreferredId: testID[:],
					},
				},
			},
			compressionType:  compression.TypeNone,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "app_request message with no compression",
			op:   AppRequestOp,
			msg: &p2p.Message{
				Message: &p2p.Message_AppRequest{
					AppRequest: &p2p.AppRequest{
						ChainId:   testID[:],
						RequestId: 1,
						Deadline:  1,
						AppBytes:  compressibleContainers[0],
					},
				},
			},
			compressionType:  compression.TypeNone,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "app_request message with zstd compression",
			op:   AppRequestOp,
			msg: &p2p.Message{
				Message: &p2p.Message_AppRequest{
					AppRequest: &p2p.AppRequest{
						ChainId:   testID[:],
						RequestId: 1,
						Deadline:  1,
						AppBytes:  compressibleContainers[0],
					},
				},
			},
			compressionType:  compression.TypeZstd,
			bypassThrottling: true,
			bytesSaved:       true,
		},
		{
			desc: "app_response message with no compression",
			op:   AppResponseOp,
			msg: &p2p.Message{
				Message: &p2p.Message_AppResponse{
					AppResponse: &p2p.AppResponse{
						ChainId:   testID[:],
						RequestId: 1,
						AppBytes:  compressibleContainers[0],
					},
				},
			},
			compressionType:  compression.TypeNone,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "app_response message with zstd compression",
			op:   AppResponseOp,
			msg: &p2p.Message{
				Message: &p2p.Message_AppResponse{
					AppResponse: &p2p.AppResponse{
						ChainId:   testID[:],
						RequestId: 1,
						AppBytes:  compressibleContainers[0],
					},
				},
			},
			compressionType:  compression.TypeZstd,
			bypassThrottling: true,
			bytesSaved:       true,
		},
		{
			desc: "app_gossip message with no compression",
			op:   AppGossipOp,
			msg: &p2p.Message{
				Message: &p2p.Message_AppGossip{
					AppGossip: &p2p.AppGossip{
						ChainId:  testID[:],
						AppBytes: compressibleContainers[0],
					},
				},
			},
			compressionType:  compression.TypeNone,
			bypassThrottling: true,
			bytesSaved:       false,
		},
		{
			desc: "app_gossip message with zstd compression",
			op:   AppGossipOp,
			msg: &p2p.Message{
				Message: &p2p.Message_AppGossip{
					AppGossip: &p2p.AppGossip{
						ChainId:  testID[:],
						AppBytes: compressibleContainers[0],
					},
				},
			},
			compressionType:  compression.TypeZstd,
			bypassThrottling: true,
			bytesSaved:       true,
		},
		{
			desc: "simplex message with no compression",
			op:   SimplexOp,
			msg: &p2p.Message{
				Message: &p2p.Message_Simplex{
					Simplex: &p2p.Simplex{
						ChainId: testID[:],
						Message: &p2p.Simplex_ReplicationRequest{
							ReplicationRequest: &p2p.ReplicationRequest{
								Seqs:        []uint64{1, 2, 3},
								LatestRound: 1,
							},
						},
					},
				},
			},
			compressionType:  compression.TypeNone,
			bypassThrottling: true,
			bytesSaved:       false,
		},
	}

	for _, tv := range tests {
		t.Run(tv.desc, func(t *testing.T) {
			require := require.New(t)

			encodedMsg, err := mb.createOutbound(tv.msg, tv.compressionType, tv.bypassThrottling)
			require.NoError(err)

			require.Equal(tv.bypassThrottling, encodedMsg.BypassThrottling())
			require.Equal(tv.op, encodedMsg.Op())

			if bytesSaved := encodedMsg.BytesSavedCompression(); tv.bytesSaved {
				require.Positive(bytesSaved)
			}

			parsedMsg, err := mb.parseInbound(encodedMsg.Bytes(), ids.EmptyNodeID, func() {})
			require.NoError(err)
			require.Equal(tv.op, parsedMsg.Op())
		})
	}
}

// Tests the Stringer interface on inbound messages
func TestInboundMessageToString(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	mb, err := newMsgBuilder(
		prometheus.NewRegistry(),
		5*time.Second,
	)
	require.NoError(err)

	// msg that will become the tested InboundMessage
	msg := &p2p.Message{
		Message: &p2p.Message_Pong{
			Pong: &p2p.Pong{},
		},
	}
	msgBytes, err := proto.Marshal(msg)
	require.NoError(err)

	inboundMsg, err := mb.parseInbound(msgBytes, ids.EmptyNodeID, func() {})
	require.NoError(err)

	require.Equal("NodeID-111111111111111111116DBWJs Op: pong Message: ", inboundMsg.String())

	internalMsg := InternalGetStateSummaryFrontierFailed(ids.EmptyNodeID, ids.Empty, 1)
	require.Equal("NodeID-111111111111111111116DBWJs Op: get_state_summary_frontier_failed Message: ChainID: 11111111111111111111111111111111LpoYY RequestID: 1", internalMsg.String())
}

func TestEmptyInboundMessage(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	mb, err := newMsgBuilder(
		prometheus.NewRegistry(),
		5*time.Second,
	)
	require.NoError(err)

	msg := &p2p.Message{}
	msgBytes, err := proto.Marshal(msg)
	require.NoError(err)

	_, err = mb.parseInbound(msgBytes, ids.EmptyNodeID, func() {})
	require.ErrorIs(err, errUnknownMessageType)
}

func TestNilInboundMessage(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	mb, err := newMsgBuilder(
		prometheus.NewRegistry(),
		5*time.Second,
	)
	require.NoError(err)

	msg := &p2p.Message{
		Message: &p2p.Message_Ping{
			Ping: nil,
		},
	}
	msgBytes, err := proto.Marshal(msg)
	require.NoError(err)

	parsedMsg, err := mb.parseInbound(msgBytes, ids.EmptyNodeID, func() {})
	require.NoError(err)

	require.IsType(&p2p.Ping{}, parsedMsg.message)
	pingMsg := parsedMsg.message.(*p2p.Ping)
	require.NotNil(pingMsg)
}
