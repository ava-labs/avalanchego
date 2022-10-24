// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"bytes"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/units"

	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
)

func TestNewOutboundInboundMessage(t *testing.T) {
	t.Parallel()

	require := require.New(t)

	mb, err := newMsgBuilder(
		"test",
		prometheus.NewRegistry(),
		units.MiB,
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
		desc                string
		op                  Op
		msg                 *p2ppb.Message
		gzipCompress        bool
		bypassThrottling    bool
		bytesSaved          bool                  // if true, outbound message saved bytes must be non-zero
		expectedOutboundErr error                 // expected error for creating outbound message
		fields              map[Field]interface{} // expected fields from the inbound message
		expectedGetFieldErr map[Field]error       // expected error for getting the specified field
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
			gzipCompress:        false,
			bypassThrottling:    true,
			bytesSaved:          false,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				Uptime: uint8(1),
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "valid ping outbound message with no compression",
			op:   Ping,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_Ping{
					Ping: &p2ppb.Ping{},
				},
			},
			gzipCompress:        false,
			bypassThrottling:    true,
			bytesSaved:          false,
			expectedOutboundErr: nil,
			fields:              nil,
			expectedGetFieldErr: nil,
		},
		{
			desc: "valid get_accepted_frontier outbound message with no compression",
			op:   GetAcceptedFrontier,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_GetAcceptedFrontier{
					GetAcceptedFrontier: &p2ppb.GetAcceptedFrontier{
						ChainId:   testID[:],
						RequestId: 1,
						Deadline:  1,
					},
				},
			},
			gzipCompress:        false,
			bypassThrottling:    true,
			bytesSaved:          false,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				ChainID:   testID[:],
				RequestID: uint32(1),
				Deadline:  uint64(1),
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "valid accepted_frontier outbound message with no compression",
			op:   AcceptedFrontier,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_AcceptedFrontier_{
					AcceptedFrontier_: &p2ppb.AcceptedFrontier{
						ChainId:      testID[:],
						RequestId:    1,
						ContainerIds: [][]byte{testID[:], testID[:]},
					},
				},
			},
			gzipCompress:        false,
			bypassThrottling:    true,
			bytesSaved:          false,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				ChainID:      testID[:],
				RequestID:    uint32(1),
				ContainerIDs: [][]byte{testID[:], testID[:]},
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "valid get_accepted outbound message with no compression",
			op:   GetAccepted,
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
			gzipCompress:        false,
			bypassThrottling:    true,
			bytesSaved:          false,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				ChainID:      testID[:],
				RequestID:    uint32(1),
				Deadline:     uint64(1),
				ContainerIDs: [][]byte{testID[:], testID[:]},
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "valid accepted outbound message with no compression",
			op:   Accepted,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_Accepted_{
					Accepted_: &p2ppb.Accepted{
						ChainId:      testID[:],
						RequestId:    1,
						ContainerIds: [][]byte{testID[:], testID[:]},
					},
				},
			},
			gzipCompress:        false,
			bypassThrottling:    true,
			bytesSaved:          false,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				ChainID:      testID[:],
				RequestID:    uint32(1),
				ContainerIDs: [][]byte{testID[:], testID[:]},
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "valid get_ancestors outbound message with no compression",
			op:   GetAncestors,
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
			gzipCompress:        false,
			bypassThrottling:    true,
			bytesSaved:          false,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				ChainID:     testID[:],
				RequestID:   uint32(1),
				Deadline:    uint64(1),
				ContainerID: testID[:],
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "valid ancestor outbound message with no compression",
			op:   Ancestors,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_Ancestors_{
					Ancestors_: &p2ppb.Ancestors{
						ChainId:    testID[:],
						RequestId:  12345,
						Containers: compressibleContainers,
					},
				},
			},
			gzipCompress:        false,
			bypassThrottling:    true,
			bytesSaved:          false,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				ChainID:             testID[:],
				RequestID:           uint32(12345),
				MultiContainerBytes: compressibleContainers,
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "valid ancestor outbound message with compression",
			op:   Ancestors,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_Ancestors_{
					Ancestors_: &p2ppb.Ancestors{
						ChainId:    testID[:],
						RequestId:  12345,
						Containers: compressibleContainers,
					},
				},
			},
			gzipCompress:        true,
			bypassThrottling:    true,
			bytesSaved:          true,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				ChainID:             testID[:],
				RequestID:           uint32(12345),
				MultiContainerBytes: compressibleContainers,
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "valid get outbound message with no compression",
			op:   Get,
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
			gzipCompress:        false,
			bypassThrottling:    true,
			bytesSaved:          false,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				ChainID:     testID[:],
				RequestID:   uint32(1),
				Deadline:    uint64(1),
				ContainerID: testID[:],
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "valid put outbound message with no compression",
			op:   Put,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_Put{
					Put: &p2ppb.Put{
						ChainId:   testID[:],
						RequestId: 1,
						Container: []byte{0},
					},
				},
			},
			gzipCompress:        false,
			bypassThrottling:    true,
			bytesSaved:          false,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				ChainID:        testID[:],
				RequestID:      uint32(1),
				ContainerBytes: []byte{0},
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "valid put outbound message with compression",
			op:   Put,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_Put{
					Put: &p2ppb.Put{
						ChainId:   testID[:],
						RequestId: 1,
						Container: compressibleContainers[0],
					},
				},
			},
			gzipCompress:        true,
			bypassThrottling:    true,
			bytesSaved:          true,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				ChainID:        testID[:],
				RequestID:      uint32(1),
				ContainerBytes: compressibleContainers[0],
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "valid push_query outbound message with no compression",
			op:   PushQuery,
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
			gzipCompress:        false,
			bypassThrottling:    true,
			bytesSaved:          false,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				ChainID:        testID[:],
				RequestID:      uint32(1),
				Deadline:       uint64(1),
				ContainerBytes: []byte{0},
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "valid push_query outbound message with compression",
			op:   PushQuery,
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
			gzipCompress:        true,
			bypassThrottling:    true,
			bytesSaved:          true,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				ChainID:        testID[:],
				RequestID:      uint32(1),
				Deadline:       uint64(1),
				ContainerBytes: compressibleContainers[0],
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "valid pull_query outbound message with no compression",
			op:   PullQuery,
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
			gzipCompress:        false,
			bypassThrottling:    true,
			bytesSaved:          false,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				ChainID:     testID[:],
				RequestID:   uint32(1),
				Deadline:    uint64(1),
				ContainerID: testID[:],
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "valid chits outbound message with no compression",
			op:   Chits,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_Chits{
					Chits: &p2ppb.Chits{
						ChainId:      testID[:],
						RequestId:    1,
						ContainerIds: [][]byte{testID[:], testID[:]},
					},
				},
			},
			gzipCompress:        false,
			bypassThrottling:    true,
			bytesSaved:          false,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				ChainID:      testID[:],
				RequestID:    uint32(1),
				ContainerIDs: [][]byte{testID[:], testID[:]},
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "valid peer_list outbound message with no compression",
			op:   PeerList,
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
			gzipCompress:        false,
			bypassThrottling:    true,
			bytesSaved:          false,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				Peers: []ips.ClaimedIPPort{
					{
						Cert:      testTLSCert.Leaf,
						IPPort:    ips.IPPort{IP: net.IPv4zero, Port: uint16(10)},
						Timestamp: uint64(1),
						Signature: []byte{0},
					},
				},
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "invalid peer_list inbound message with invalid cert",
			op:   PeerList,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_PeerList{
					PeerList: &p2ppb.PeerList{
						ClaimedIpPorts: []*p2ppb.ClaimedIpPort{
							{
								X509Certificate: []byte{0},
								IpAddr:          []byte(net.IPv4zero[4:]),
								IpPort:          10,
								Timestamp:       1,
								Signature:       []byte{0},
							},
						},
					},
				},
			},
			gzipCompress:        false,
			bypassThrottling:    true,
			bytesSaved:          false,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				Peers: nil,
			},
			expectedGetFieldErr: map[Field]error{Peers: errInvalidCert},
		},
		{
			desc: "invalid peer_list inbound message with invalid ip",
			op:   PeerList,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_PeerList{
					PeerList: &p2ppb.PeerList{
						ClaimedIpPorts: []*p2ppb.ClaimedIpPort{
							{
								X509Certificate: testTLSCert.Certificate[0],
								IpAddr:          []byte(net.IPv4zero[4:]),
								IpPort:          10,
								Timestamp:       1,
								Signature:       []byte{0},
							},
						},
					},
				},
			},
			gzipCompress:        false,
			bypassThrottling:    true,
			bytesSaved:          false,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				Peers: nil,
			},
			expectedGetFieldErr: map[Field]error{Peers: errInvalidIPAddrLen},
		},
		{
			desc: "valid peer_list outbound message with compression",
			op:   PeerList,
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
			gzipCompress:        true,
			bypassThrottling:    true,
			bytesSaved:          true,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				Peers: []ips.ClaimedIPPort{
					{
						Cert:      testTLSCert.Leaf,
						IPPort:    ips.IPPort{IP: net.IPv6zero, Port: uint16(9651)},
						Timestamp: uint64(nowUnix),
						Signature: compressibleContainers[0],
					},
				},
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "invalid peer_list outbound message with compression and invalid cert",
			op:   PeerList,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_PeerList{
					PeerList: &p2ppb.PeerList{
						ClaimedIpPorts: []*p2ppb.ClaimedIpPort{
							{
								X509Certificate: testTLSCert.Certificate[0][10:],
								IpAddr:          []byte(net.IPv6zero),
								IpPort:          9651,
								Timestamp:       uint64(nowUnix),
								Signature:       compressibleContainers[0],
							},
						},
					},
				},
			},
			gzipCompress:        true,
			bypassThrottling:    true,
			bytesSaved:          true,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				Peers: nil,
			},
			expectedGetFieldErr: map[Field]error{Peers: errInvalidCert},
		},
		{
			desc: "invalid peer_list outbound message with compression and invalid ip",
			op:   PeerList,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_PeerList{
					PeerList: &p2ppb.PeerList{
						ClaimedIpPorts: []*p2ppb.ClaimedIpPort{
							{
								X509Certificate: testTLSCert.Certificate[0],
								IpAddr:          []byte(net.IPv6zero[:5]),
								IpPort:          9651,
								Timestamp:       uint64(nowUnix),
								Signature:       compressibleContainers[0],
							},
						},
					},
				},
			},
			gzipCompress:        true,
			bypassThrottling:    true,
			bytesSaved:          true,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				Peers: nil,
			},
			expectedGetFieldErr: map[Field]error{Peers: errInvalidIPAddrLen},
		},
		{
			desc: "valid version outbound message with no compression",
			op:   Version,
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
			bypassThrottling:    true,
			bytesSaved:          false,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				NetworkID:      uint32(1337),
				MyTime:         uint64(nowUnix),
				IP:             ips.IPPort{IP: net.IPv6zero, Port: uint16(9651)},
				VersionStr:     "v1.2.3",
				VersionTime:    uint64(nowUnix),
				SigBytes:       []byte{'y', 'e', 'e', 't'},
				TrackedSubnets: [][]byte{testID[:]},
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "invalid version inbound message with invalid ip",
			op:   Version,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_Version{
					Version: &p2ppb.Version{
						NetworkId:      uint32(1337),
						MyTime:         uint64(nowUnix),
						IpAddr:         []byte(net.IPv6zero[1:]),
						IpPort:         9651,
						MyVersion:      "v1.2.3",
						MyVersionTime:  uint64(nowUnix),
						Sig:            []byte{'y', 'e', 'e', 't'},
						TrackedSubnets: [][]byte{testID[:]},
					},
				},
			},
			bypassThrottling:    true,
			bytesSaved:          false,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				NetworkID:      uint32(1337),
				MyTime:         uint64(nowUnix),
				IP:             nil,
				VersionStr:     "v1.2.3",
				VersionTime:    uint64(nowUnix),
				SigBytes:       []byte{'y', 'e', 'e', 't'},
				TrackedSubnets: [][]byte{testID[:]},
			},
			expectedGetFieldErr: map[Field]error{IP: errInvalidIPAddrLen},
		},
		{
			desc: "valid app_request outbound message with no compression",
			op:   AppRequest,
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
			gzipCompress:        false,
			bypassThrottling:    true,
			bytesSaved:          false,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				ChainID:   testID[:],
				RequestID: uint32(1),
				Deadline:  uint64(1),
				AppBytes:  compressibleContainers[0],
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "valid app_request outbound message with compression",
			op:   AppRequest,
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
			gzipCompress:        true,
			bypassThrottling:    true,
			bytesSaved:          true,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				ChainID:   testID[:],
				RequestID: uint32(1),
				Deadline:  uint64(1),
				AppBytes:  compressibleContainers[0],
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "valid app_response outbound message with no compression",
			op:   AppResponse,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_AppResponse{
					AppResponse: &p2ppb.AppResponse{
						ChainId:   testID[:],
						RequestId: 1,
						AppBytes:  compressibleContainers[0],
					},
				},
			},
			gzipCompress:        false,
			bypassThrottling:    true,
			bytesSaved:          false,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				ChainID:   testID[:],
				RequestID: uint32(1),
				AppBytes:  compressibleContainers[0],
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "valid app_response outbound message with compression",
			op:   AppResponse,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_AppResponse{
					AppResponse: &p2ppb.AppResponse{
						ChainId:   testID[:],
						RequestId: 1,
						AppBytes:  compressibleContainers[0],
					},
				},
			},
			gzipCompress:        true,
			bypassThrottling:    true,
			bytesSaved:          true,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				ChainID:   testID[:],
				RequestID: uint32(1),
				AppBytes:  compressibleContainers[0],
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "valid app_gossip outbound message with no compression",
			op:   AppGossip,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_AppGossip{
					AppGossip: &p2ppb.AppGossip{
						ChainId:  testID[:],
						AppBytes: compressibleContainers[0],
					},
				},
			},
			gzipCompress:        false,
			bypassThrottling:    true,
			bytesSaved:          false,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				ChainID:  testID[:],
				AppBytes: compressibleContainers[0],
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "valid app_gossip outbound message with compression",
			op:   AppGossip,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_AppGossip{
					AppGossip: &p2ppb.AppGossip{
						ChainId:  testID[:],
						AppBytes: compressibleContainers[0],
					},
				},
			},
			gzipCompress:        true,
			bypassThrottling:    true,
			bytesSaved:          true,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				ChainID:  testID[:],
				AppBytes: compressibleContainers[0],
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "valid get_state_summary_frontier outbound message with no compression",
			op:   GetStateSummaryFrontier,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_GetStateSummaryFrontier{
					GetStateSummaryFrontier: &p2ppb.GetStateSummaryFrontier{
						ChainId:   testID[:],
						RequestId: 1,
						Deadline:  1,
					},
				},
			},
			gzipCompress:        false,
			bypassThrottling:    true,
			bytesSaved:          false,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				ChainID:   testID[:],
				RequestID: uint32(1),
				Deadline:  uint64(1),
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "valid state_summary_frontier outbound message with no compression",
			op:   StateSummaryFrontier,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_StateSummaryFrontier_{
					StateSummaryFrontier_: &p2ppb.StateSummaryFrontier{
						ChainId:   testID[:],
						RequestId: 1,
						Summary:   []byte{0},
					},
				},
			},
			gzipCompress:        false,
			bypassThrottling:    true,
			bytesSaved:          false,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				ChainID:      testID[:],
				RequestID:    uint32(1),
				SummaryBytes: []byte{0},
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "valid state_summary_frontier outbound message with compression",
			op:   StateSummaryFrontier,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_StateSummaryFrontier_{
					StateSummaryFrontier_: &p2ppb.StateSummaryFrontier{
						ChainId:   testID[:],
						RequestId: 1,
						Summary:   compressibleContainers[0],
					},
				},
			},
			gzipCompress:        true,
			bypassThrottling:    true,
			bytesSaved:          true,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				ChainID:      testID[:],
				RequestID:    uint32(1),
				SummaryBytes: compressibleContainers[0],
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "valid get_accepted_state_summary_frontier outbound message with no compression",
			op:   GetAcceptedStateSummary,
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
			gzipCompress:        false,
			bypassThrottling:    true,
			bytesSaved:          false,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				ChainID:        testID[:],
				RequestID:      uint32(1),
				Deadline:       uint64(1),
				SummaryHeights: []uint64{0},
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "valid get_accepted_state_summary_frontier outbound message with compression",
			op:   GetAcceptedStateSummary,
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
			gzipCompress:        true,
			bypassThrottling:    true,
			bytesSaved:          false,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				ChainID:        testID[:],
				RequestID:      uint32(1),
				Deadline:       uint64(1),
				SummaryHeights: []uint64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "valid accepted_state_summary_frontier outbound message with no compression",
			op:   AcceptedStateSummary,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_AcceptedStateSummary_{
					AcceptedStateSummary_: &p2ppb.AcceptedStateSummary{
						ChainId:    testID[:],
						RequestId:  1,
						SummaryIds: [][]byte{testID[:], testID[:]},
					},
				},
			},
			gzipCompress:        false,
			bypassThrottling:    true,
			bytesSaved:          false,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				ChainID:    testID[:],
				RequestID:  uint32(1),
				SummaryIDs: [][]byte{testID[:], testID[:]},
			},
			expectedGetFieldErr: nil,
		},
		{
			desc: "valid accepted_state_summary_frontier outbound message with compression",
			op:   AcceptedStateSummary,
			msg: &p2ppb.Message{
				Message: &p2ppb.Message_AcceptedStateSummary_{
					AcceptedStateSummary_: &p2ppb.AcceptedStateSummary{
						ChainId:    testID[:],
						RequestId:  1,
						SummaryIds: [][]byte{testID[:], testID[:], testID[:], testID[:], testID[:], testID[:], testID[:], testID[:], testID[:]},
					},
				},
			},
			gzipCompress:        true,
			bypassThrottling:    true,
			bytesSaved:          true,
			expectedOutboundErr: nil,
			fields: map[Field]interface{}{
				ChainID:    testID[:],
				RequestID:  uint32(1),
				SummaryIDs: [][]byte{testID[:], testID[:], testID[:], testID[:], testID[:], testID[:], testID[:], testID[:], testID[:]},
			},
		},
	}

	for _, tv := range tests {
		require.True(t.Run(tv.desc, func(t2 *testing.T) {
			// copy before we in-place update via marshal
			oldProtoMsgS := tv.msg.String()

			encodedMsg, err := mb.createOutbound(tv.op, tv.msg, tv.gzipCompress, tv.bypassThrottling)
			require.ErrorIs(err, tv.expectedOutboundErr, fmt.Errorf("unexpected error %v (%T)", err, err))
			if tv.expectedOutboundErr != nil {
				return
			}

			require.Equal(encodedMsg.BypassThrottling(), tv.bypassThrottling)

			bytesSaved := encodedMsg.BytesSavedCompression()
			if bytesSaved > 0 {
				t2.Logf("saved %d bytes", bytesSaved)
			}
			require.Equal(tv.bytesSaved, bytesSaved > 0)

			if (bytesSaved > 0) != tv.bytesSaved {
				// if bytes saved expected via compression,
				// the outbound message BytesSavedCompression should return >bytesSaved
				t.Fatalf("unexpected BytesSavedCompression>0 (%d), expected bytes saved %v", bytesSaved, tv.bytesSaved)
			}

			parsedMsg, err := mb.parseInbound(encodedMsg.Bytes(), ids.EmptyNodeID, func() {})
			require.NoError(err)

			// before/after compression, the message should be the same
			require.Equal(parsedMsg.msg.String(), oldProtoMsgS)

			for field, v1 := range tv.fields {
				v2, err := getField(parsedMsg.msg, field)

				// expects "getField" error
				if expectedGetFieldErr, ok := tv.expectedGetFieldErr[field]; ok {
					require.ErrorIs(err, expectedGetFieldErr)
					continue
				}

				require.NoError(err)
				require.Equal(v1, v2)
			}
		}))
	}
}
