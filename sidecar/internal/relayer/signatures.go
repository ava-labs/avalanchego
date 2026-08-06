// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package relayer contains the machinery that the sidecar relayer commands
// (gatewayrelayer, teleporterrelayer, registryrelayer) and sidecarcheck
// share. The machinery includes ACP-118 signature collection over the p2p
// network, signature verification and quorum aggregation, EVM receipt and
// log helpers, and the Teleporter ABI mirror types.
package relayer

import (
	"context"
	"fmt"
	"log"
	"net/netip"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	p2pmessage "github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/utils/compression"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"

	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
)

// CollectSignatures connects to each signer as a network peer. It sends an
// ACP-118 SignatureRequest at the given protocol prefix. It returns the raw
// signature bytes per responding signer NodeID. Per-signer failures are
// logged and skipped. The caller enforces quorum.
//
// The requests are sequential: each signer is a separate peer handshake,
// and a relayer paces its requests instead of flooding the committee at
// once. Concurrent handshakes proved flaky. A production relayer would use
// a small bounded worker pool. Sequential requests are ample for
// committee-sized fan-out.
func CollectSignatures(
	ctx context.Context,
	networkID uint32,
	chainID ids.ID,
	protocolPrefix []byte,
	unsigned *avalancheWarp.UnsignedMessage,
	justification []byte,
	addrs []string,
) map[ids.NodeID][]byte {
	requestPayload, err := proto.Marshal(&sdk.SignatureRequest{
		Message:       unsigned.Bytes(),
		Justification: justification,
	})
	if err != nil {
		log.Fatalf("marshal SignatureRequest: %v", err)
	}
	prefixed := append(append([]byte{}, protocolPrefix...), requestPayload...)

	out := make(map[ids.NodeID][]byte)
	for _, addr := range addrs {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		nodeID, sig, err := RequestOne(ctx, networkID, chainID, prefixed, addr)
		if err != nil {
			log.Printf("signer at %s: %v", addr, err)
			continue
		}
		out[nodeID] = sig
	}
	return out
}

// RequestOne performs the peer handshake with a single signer at addr. It
// sends the prefixed ACP-118 SignatureRequest and waits for the signature.
func RequestOne(
	ctx context.Context,
	networkID uint32,
	chainID ids.ID,
	prefixedRequest []byte,
	addr string,
) (ids.NodeID, []byte, error) {
	addrPort, err := netip.ParseAddrPort(addr)
	if err != nil {
		return ids.EmptyNodeID, nil, fmt.Errorf("bad address: %w", err)
	}
	type result struct {
		sig []byte
		err error
	}
	resultCh := make(chan result, 1)
	p, err := peer.StartTestPeer(ctx, addrPort, networkID,
		router.InboundHandlerFunc(func(_ context.Context, msg *p2pmessage.InboundMessage) {
			switch msg.Op {
			case p2pmessage.AppResponseOp:
				resp, ok := msg.Message.(*p2ppb.AppResponse)
				if !ok {
					return
				}
				var sigResp sdk.SignatureResponse
				if err := proto.Unmarshal(resp.AppBytes, &sigResp); err != nil {
					resultCh <- result{err: fmt.Errorf("bad SignatureResponse: %w", err)}
					return
				}
				resultCh <- result{sig: sigResp.Signature}
			case p2pmessage.AppErrorOp:
				appErr, ok := msg.Message.(*p2ppb.AppError)
				if !ok {
					return
				}
				resultCh <- result{err: fmt.Errorf("signer refused: %s", appErr.ErrorMessage)}
			}
		}),
	)
	if err != nil {
		return ids.EmptyNodeID, nil, fmt.Errorf("peer handshake failed: %w", err)
	}
	defer func() {
		p.StartClose()
		_ = p.AwaitClosed(context.Background())
	}()

	mb, err := p2pmessage.NewCreator(prometheus.NewRegistry(), compression.TypeZstd, time.Hour)
	if err != nil {
		return ids.EmptyNodeID, nil, err
	}
	// The requestID is pinned to 1, with a fresh peer per request (the
	// odd-requestID fix).
	appRequest, err := mb.AppRequest(chainID, 1, 30*time.Second, prefixedRequest)
	if err != nil {
		return ids.EmptyNodeID, nil, err
	}
	if !p.Send(ctx, appRequest) {
		return ids.EmptyNodeID, nil, fmt.Errorf("send failed")
	}
	select {
	case r := <-resultCh:
		return p.ID(), r.sig, r.err
	case <-time.After(20 * time.Second):
		return p.ID(), nil, fmt.Errorf("timed out waiting for signature")
	case <-ctx.Done():
		return p.ID(), nil, ctx.Err()
	}
}
