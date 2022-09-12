// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"net"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/compression"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/units"

	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
)

// Ensures the message size with proto not blow up compared to packer.
func TestProtoMarshalSizeVersion(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	id := ids.GenerateTestID()
	inboundMsg := inboundMessageWithPacker{
		inboundMessage: inboundMessage{
			op: Version,
		},
		fields: map[Field]interface{}{
			NetworkID:      uint32(1337),
			NodeID:         uint32(0),
			MyTime:         uint64(time.Now().Unix()),
			IP:             ips.IPPort{IP: net.IPv4(1, 2, 3, 4)},
			VersionStr:     "v1.2.3",
			VersionTime:    uint64(time.Now().Unix()),
			SigBytes:       []byte{'y', 'e', 'e', 't'},
			TrackedSubnets: [][]byte{id[:]},
		},
	}
	packerCodec, err := NewCodecWithMemoryPool("", prometheus.NewRegistry(), 2*units.MiB, 10*time.Second)
	require.NoError(err)

	packerMsg, err := packerCodec.Pack(inboundMsg.op, inboundMsg.fields, inboundMsg.op.Compressible(), false)
	require.NoError(err)

	packerMsgN := len(packerMsg.Bytes())

	protoMsg := p2ppb.Message{
		Message: &p2ppb.Message_Version{
			Version: &p2ppb.Version{
				NetworkId:      uint32(1337),
				MyTime:         uint64(time.Now().Unix()),
				IpAddr:         []byte(net.IPv4(1, 2, 3, 4).To16()),
				IpPort:         0,
				MyVersion:      "v1.2.3",
				MyVersionTime:  uint64(time.Now().Unix()),
				Sig:            []byte{'y', 'e', 'e', 't'},
				TrackedSubnets: [][]byte{id[:]},
			},
		},
	}
	protoMsgN := proto.Size(&protoMsg)

	t.Logf("marshaled; packer %d-byte, proto %d-byte", packerMsgN, protoMsgN)
	require.Greater(packerMsgN, protoMsgN)
}

// Ensures the message size with proto not blow up compared to packer.
func TestProtoMarshalSizeAncestors(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	id := ids.GenerateTestID()
	inboundMsg := inboundMessageWithPacker{
		inboundMessage: inboundMessage{
			op: Ancestors,
		},
		fields: map[Field]interface{}{
			ChainID:   id[:],
			RequestID: uint32(12345),
			MultiContainerBytes: [][]byte{
				bytes.Repeat([]byte{0}, 100),
				bytes.Repeat([]byte{0}, 100),
				bytes.Repeat([]byte{0}, 100),
			},
		},
	}
	packerCodec, err := NewCodecWithMemoryPool("", prometheus.NewRegistry(), 2*units.MiB, 10*time.Second)
	require.NoError(err)

	compressible := inboundMsg.op.Compressible()
	require.True(compressible)

	packerMsg, err := packerCodec.Pack(inboundMsg.op, inboundMsg.fields, compressible, false)
	require.NoError(err)

	packerMsgN := len(packerMsg.Bytes())

	protoMsg := p2ppb.Message{
		Message: &p2ppb.Message_Ancestors_{
			Ancestors_: &p2ppb.Ancestors{
				ChainId:   id[:],
				RequestId: 12345,
				Containers: [][]byte{
					bytes.Repeat([]byte{0}, 100),
					bytes.Repeat([]byte{0}, 100),
					bytes.Repeat([]byte{0}, 100),
				},
			},
		},
	}

	mc := newMsgBuilderProtobuf(2 * units.MiB)
	b, _, err := mc.marshal(&protoMsg, compressible)
	require.NoError(err)

	protoMsgN := len(b)
	t.Logf("marshaled; packer %d-byte, proto %d-byte", packerMsgN, protoMsgN)

	require.GreaterOrEqual(packerMsgN, protoMsgN)
}

func TestNewOutboundMessageWithProto(t *testing.T) {
	t.Parallel()

	require := require.New(t)
	mc := newMsgBuilderProtobuf(math.MaxInt64)

	id := ids.GenerateTestID()
	tt := []struct {
		desc             string
		op               Op
		msg              *p2ppb.Message
		gzipCompress     bool
		bypassThrottling bool
		bytesSaved       bool
		expectedErr      error
	}{
		{
			desc: "valid pong outbound message with no compression",
			op:   Pong,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_Pong{
					Pong: &p2ppb.Pong{
						UptimePct: 1,
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
			expectedErr:      nil,
		},
		{
			desc: "valid ping outbound message with no compression",
			op:   Ping,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_Ping{
					Ping: &p2ppb.Ping{},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
			expectedErr:      nil,
		},
		{
			desc: "valid get_accepted_frontier outbound message with no compression",
			op:   GetAcceptedFrontier,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_GetAcceptedFrontier{
					GetAcceptedFrontier: &p2ppb.GetAcceptedFrontier{
						ChainId:   id[:],
						RequestId: 1,
						Deadline:  1,
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
			expectedErr:      nil,
		},
		{
			desc: "valid accepted_frontier outbound message with no compression",
			op:   AcceptedFrontier,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_AcceptedFrontier_{
					AcceptedFrontier_: &p2ppb.AcceptedFrontier{
						ChainId:      id[:],
						RequestId:    1,
						ContainerIds: [][]byte{id[:], id[:]},
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
			expectedErr:      nil,
		},
		{
			desc: "valid accepted outbound message with no compression",
			op:   Accepted,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_Accepted_{
					Accepted_: &p2ppb.Accepted{
						ChainId:      id[:],
						RequestId:    1,
						ContainerIds: [][]byte{id[:], id[:]},
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
			expectedErr:      nil,
		},
		{
			desc: "valid get_ancestors outbound message with no compression",
			op:   GetAncestors,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_GetAncestors{
					GetAncestors: &p2ppb.GetAncestors{
						ChainId:     id[:],
						RequestId:   1,
						Deadline:    1,
						ContainerId: id[:],
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
			expectedErr:      nil,
		},
		{
			desc: "valid ancestor outbound message with no compression",
			op:   Ancestors,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_Ancestors_{
					Ancestors_: &p2ppb.Ancestors{
						ChainId:   id[:],
						RequestId: 12345,
						Containers: [][]byte{
							bytes.Repeat([]byte{0}, 32),
							bytes.Repeat([]byte{0}, 32),
							bytes.Repeat([]byte{0}, 32),
						},
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
			expectedErr:      nil,
		},
		{
			desc: "valid ancestor outbound message with compression",
			op:   Ancestors,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_Ancestors_{
					Ancestors_: &p2ppb.Ancestors{
						ChainId:   id[:],
						RequestId: 12345,
						Containers: [][]byte{
							bytes.Repeat([]byte{0}, 32),
							bytes.Repeat([]byte{0}, 32),
							bytes.Repeat([]byte{0}, 32),
						},
					},
				},
			},
			gzipCompress:     true,
			bypassThrottling: true,
			bytesSaved:       true,
			expectedErr:      nil,
		},
		{
			desc: "valid get outbound message with no compression",
			op:   Get,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_Get{
					Get: &p2ppb.Get{
						ChainId:     id[:],
						RequestId:   1,
						Deadline:    1,
						ContainerId: id[:],
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
			expectedErr:      nil,
		},
		{
			desc: "valid put outbound message with no compression",
			op:   Put,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_Put{
					Put: &p2ppb.Put{
						ChainId:   id[:],
						RequestId: 1,
						Container: []byte{0},
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
			expectedErr:      nil,
		},
		{
			desc: "valid put outbound message with compression",
			op:   Put,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_Put{
					Put: &p2ppb.Put{
						ChainId:   id[:],
						RequestId: 1,
						Container: bytes.Repeat([]byte{0}, 100),
					},
				},
			},
			gzipCompress:     true,
			bypassThrottling: true,
			bytesSaved:       true,
			expectedErr:      nil,
		},
		{
			desc: "valid push_query outbound message with no compression",
			op:   PushQuery,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_PushQuery{
					PushQuery: &p2ppb.PushQuery{
						ChainId:   id[:],
						RequestId: 1,
						Deadline:  1,
						Container: []byte{0},
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
			expectedErr:      nil,
		},
		{
			desc: "valid push_query outbound message with compression",
			op:   PushQuery,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_PushQuery{
					PushQuery: &p2ppb.PushQuery{
						ChainId:   id[:],
						RequestId: 1,
						Deadline:  1,
						Container: bytes.Repeat([]byte{0}, 100),
					},
				},
			},
			gzipCompress:     true,
			bypassThrottling: true,
			bytesSaved:       true,
			expectedErr:      nil,
		},
		{
			desc: "valid pull_query outbound message with no compression",
			op:   PullQuery,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_PullQuery{
					PullQuery: &p2ppb.PullQuery{
						ChainId:     id[:],
						RequestId:   1,
						Deadline:    1,
						ContainerId: id[:],
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
			expectedErr:      nil,
		},
		{
			desc: "valid chits outbound message with no compression",
			op:   Chits,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_Chits{
					Chits: &p2ppb.Chits{
						ChainId:      id[:],
						RequestId:    1,
						ContainerIds: [][]byte{id[:], id[:]},
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
			expectedErr:      nil,
		},
		{
			desc: "valid peer_list outbound message with no compression",
			op:   PeerList,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_PeerList{
					PeerList: &p2ppb.PeerList{
						ClaimedIpPorts: []*p2ppb.ClaimedIpPort{
							{
								X509Certificate: []byte{0, 1, 2},
								IpAddr:          []byte{0},
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
			expectedErr:      nil,
		},
		{
			desc: "valid peer_list outbound message with compression",
			op:   PeerList,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_PeerList{
					PeerList: &p2ppb.PeerList{
						ClaimedIpPorts: []*p2ppb.ClaimedIpPort{
							{
								X509Certificate: bytes.Repeat([]byte{0}, 100),
								IpAddr:          []byte{0},
								IpPort:          10,
								Timestamp:       1,
								Signature:       []byte{0},
							},
						},
					},
				},
			},
			gzipCompress:     true,
			bypassThrottling: true,
			bytesSaved:       true,
			expectedErr:      nil,
		},
		{
			desc: "valid version outbound message with no compression",
			op:   Version,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_Version{
					Version: &p2ppb.Version{
						NetworkId:      uint32(1337),
						MyTime:         uint64(time.Now().Unix()),
						IpAddr:         []byte(net.IPv4(1, 2, 3, 4).To16()),
						IpPort:         0,
						MyVersion:      "v1.2.3",
						MyVersionTime:  uint64(time.Now().Unix()),
						Sig:            []byte{'y', 'e', 'e', 't'},
						TrackedSubnets: [][]byte{id[:]},
					},
				},
			},
			bypassThrottling: true,
			bytesSaved:       false,
			expectedErr:      nil,
		},
		{
			desc: "valid app_request outbound message with no compression",
			op:   AppRequest,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_AppRequest{
					AppRequest: &p2ppb.AppRequest{
						ChainId:   id[:],
						RequestId: 1,
						Deadline:  1,
						AppBytes:  bytes.Repeat([]byte{0}, 10),
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
			expectedErr:      nil,
		},
		{
			desc: "valid app_request outbound message with compression",
			op:   AppRequest,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_AppRequest{
					AppRequest: &p2ppb.AppRequest{
						ChainId:   id[:],
						RequestId: 1,
						Deadline:  1,
						AppBytes:  bytes.Repeat([]byte{0}, 100),
					},
				},
			},
			gzipCompress:     true,
			bypassThrottling: true,
			bytesSaved:       true,
			expectedErr:      nil,
		},
		{
			desc: "valid app_response outbound message with no compression",
			op:   AppResponse,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_AppResponse{
					AppResponse: &p2ppb.AppResponse{
						ChainId:   id[:],
						RequestId: 1,
						AppBytes:  bytes.Repeat([]byte{0}, 10),
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
			expectedErr:      nil,
		},
		{
			desc: "valid app_response outbound message with compression",
			op:   AppResponse,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_AppResponse{
					AppResponse: &p2ppb.AppResponse{
						ChainId:   id[:],
						RequestId: 1,
						AppBytes:  bytes.Repeat([]byte{0}, 100),
					},
				},
			},
			gzipCompress:     true,
			bypassThrottling: true,
			bytesSaved:       true,
			expectedErr:      nil,
		},
		{
			desc: "valid app_gossip outbound message with no compression",
			op:   AppGossip,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_AppGossip{
					AppGossip: &p2ppb.AppGossip{
						ChainId:  id[:],
						AppBytes: bytes.Repeat([]byte{0}, 10),
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
			expectedErr:      nil,
		},
		{
			desc: "valid app_gossip outbound message with compression",
			op:   AppGossip,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_AppGossip{
					AppGossip: &p2ppb.AppGossip{
						ChainId:  id[:],
						AppBytes: bytes.Repeat([]byte{0}, 100),
					},
				},
			},
			gzipCompress:     true,
			bypassThrottling: true,
			bytesSaved:       true,
			expectedErr:      nil,
		},
		{
			desc: "valid get_state_summary_frontier outbound message with no compression",
			op:   GetStateSummaryFrontier,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_GetStateSummaryFrontier{
					GetStateSummaryFrontier: &p2ppb.GetStateSummaryFrontier{
						ChainId:   id[:],
						RequestId: 1,
						Deadline:  1,
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
			expectedErr:      nil,
		},
		{
			desc: "valid state_summary_frontier outbound message with no compression",
			op:   StateSummaryFrontier,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_StateSummaryFrontier_{
					StateSummaryFrontier_: &p2ppb.StateSummaryFrontier{
						ChainId:   id[:],
						RequestId: 1,
						Summary:   []byte{0},
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
			expectedErr:      nil,
		},
		{
			desc: "valid state_summary_frontier outbound message with compression",
			op:   StateSummaryFrontier,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_StateSummaryFrontier_{
					StateSummaryFrontier_: &p2ppb.StateSummaryFrontier{
						ChainId:   id[:],
						RequestId: 1,
						Summary:   bytes.Repeat([]byte{0}, 100),
					},
				},
			},
			gzipCompress:     true,
			bypassThrottling: true,
			bytesSaved:       true,
			expectedErr:      nil,
		},
		{
			desc: "valid get_accepted_state_summary_frontier outbound message with no compression",
			op:   GetAcceptedStateSummary,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_GetAcceptedStateSummary{
					GetAcceptedStateSummary: &p2ppb.GetAcceptedStateSummary{
						ChainId:   id[:],
						RequestId: 1,
						Deadline:  1,
						Heights:   []uint64{0},
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
			expectedErr:      nil,
		},
		{
			desc: "valid get_accepted_state_summary_frontier outbound message with compression",
			op:   GetAcceptedStateSummary,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_GetAcceptedStateSummary{
					GetAcceptedStateSummary: &p2ppb.GetAcceptedStateSummary{
						ChainId:   id[:],
						RequestId: 1,
						Deadline:  1,
						Heights:   []uint64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
					},
				},
			},
			gzipCompress:     true,
			bypassThrottling: true,
			bytesSaved:       false,
			expectedErr:      nil,
		},
		{
			desc: "valid accepted_state_summary_frontier outbound message with no compression",
			op:   AcceptedStateSummary,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_AcceptedStateSummary_{
					AcceptedStateSummary_: &p2ppb.AcceptedStateSummary{
						ChainId:    id[:],
						RequestId:  1,
						SummaryIds: [][]byte{id[:], id[:]},
					},
				},
			},
			gzipCompress:     false,
			bypassThrottling: true,
			bytesSaved:       false,
			expectedErr:      nil,
		},
		{
			desc: "valid accepted_state_summary_frontier outbound message with no compression",
			op:   AcceptedStateSummary,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_AcceptedStateSummary_{
					AcceptedStateSummary_: &p2ppb.AcceptedStateSummary{
						ChainId:    id[:],
						RequestId:  1,
						SummaryIds: [][]byte{id[:], id[:], id[:], id[:], id[:], id[:], id[:], id[:], id[:]},
					},
				},
			},
			gzipCompress:     true,
			bypassThrottling: true,
			bytesSaved:       true,
			expectedErr:      nil,
		},
	}

	decompressor := compression.NewGzipCompressor(2 * units.MiB)

	for _, tv := range tt {
		require.True(t.Run(tv.desc, func(tt *testing.T) {
			// copy before we in-place update via marshal
			oldProtoMsg := tv.msg.String()

			out, err := mc.createOutbound(tv.op, tv.msg, tv.gzipCompress, tv.bypassThrottling)
			require.True(errors.Is(err, tv.expectedErr), fmt.Errorf("unexpected error %v (%T)", err, err))
			require.Equal(out.BypassThrottling(), tv.bypassThrottling)

			bytesSaved := out.BytesSavedCompression()
			if bytesSaved > 0 {
				tt.Logf("saved %d bytes for %+v", bytesSaved, tv.msg.GetMessage())
			}
			require.Equal(tv.bytesSaved, bytesSaved > 0)

			if (bytesSaved > 0) != tv.bytesSaved {
				t.Fatalf("unexpected BytesSavedCompression %d, expected bytes saved %v (boolean)", bytesSaved, tv.bytesSaved)
			}

			if tv.gzipCompress {
				gzipCompressedBytes := tv.msg.GetCompressedGzip()
				require.Truef(len(gzipCompressedBytes) > 0, "%v unexpected empty GetCompressedGzip after compression", tv.msg.GetMessage())

				// TODO: use inbound message helper
				decompressed, err := decompressor.Decompress(gzipCompressedBytes)
				require.NoError(err)

				var newProtoMsg p2ppb.Message
				require.NoError(proto.Unmarshal(decompressed, &newProtoMsg))

				require.True(newProtoMsg.GetCompressedGzip() == nil)

				require.Equal(newProtoMsg.String(), oldProtoMsg)
			}
		}))
	}
}
