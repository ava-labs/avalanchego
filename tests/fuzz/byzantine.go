// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"context"
	"crypto"
	"crypto/tls"
	"errors"
	"fmt"
	"net/netip"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/network/throttling"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math/meter"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"

	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
	pvmblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
)

var errNotSent = errors.New("peer did not queue the message")

// A byzantinePeer is a peer that the fuzzer drives. It completes the real
// handshake with one honest node, and then sends whatever the fuzzer builds.
// A peer with a validator's staking key impersonates that validator, so
// honest nodes verify its blocks in that validator's proposer slots.
type byzantinePeer struct {
	peer *peer.Peer
	// votes reports whether the peer answers consensus queries (vote).
	votes bool

	requestID atomic.Uint32
	// The replies to the peer's own requests.
	puts      chan *p2ppb.Put
	chits     chan *p2ppb.Chits
	frontiers chan *p2ppb.AcceptedFrontier
}

// connectByzantine connects a new non-validator peer, over network, to the
// honest node that listens at addr. Its staking key is fresh, so it is no
// validator.
func connectByzantine(ctx context.Context, network *memNet, addr netip.AddrPort) (*byzantinePeer, error) {
	cert, err := staking.NewTLSCert()
	if err != nil {
		return nil, err
	}
	bls, err := localsigner.New()
	if err != nil {
		return nil, err
	}
	return startPeer(ctx, network, addr, cert, bls, false)
}

// connectValidator connects a peer that holds key's staking keys, so the
// honest node sees it as that validator and verifies its blocks in that
// validator's proposer slots. If votes is true, the peer answers each
// consensus query with a vote (vote).
func connectValidator(ctx context.Context, network *memNet, addr netip.AddrPort, key validatorKey, votes bool) (*byzantinePeer, error) {
	return startPeer(ctx, network, addr, key.cert, key.bls, votes)
}

// startPeer dials addr on network and completes the handshake with cert and bls. The
// node checks the handshake's BLS signature against bls's public key, so a
// validator peer must use that validator's BLS key.
func startPeer(
	ctx context.Context,
	network *memNet,
	addr netip.AddrPort,
	cert *tls.Certificate,
	bls *localsigner.LocalSigner,
	votes bool,
) (*byzantinePeer, error) {
	conn, err := network.DialContext(ctx, constants.NetworkType, addr.String())
	if err != nil {
		return nil, err
	}

	clientUpgrader := peer.NewTLSClientUpgrader(
		peer.TLSConfig(*cert, nil),
		prometheus.NewCounter(prometheus.CounterOpts{}),
	)
	peerID, conn, peerCert, err := clientUpgrader.Upgrade(conn)
	if err != nil {
		return nil, err
	}

	mc, err := message.NewCreator(prometheus.NewRegistry(), constants.DefaultNetworkCompressionType, 10*time.Second)
	if err != nil {
		return nil, err
	}

	metrics, err := peer.NewMetrics(prometheus.NewRegistry())
	if err != nil {
		return nil, err
	}

	resourceTracker, err := tracker.NewResourceTracker(
		prometheus.NewRegistry(),
		resource.NoUsage,
		meter.ContinuousFactory{},
		10*time.Second,
	)
	if err != nil {
		return nil, err
	}

	b := &byzantinePeer{
		puts:      make(chan *p2ppb.Put, 1024),
		chits:     make(chan *p2ppb.Chits, 1024),
		frontiers: make(chan *p2ppb.AcceptedFrontier, 1024),
	}
	b.votes = votes
	tlsKey := cert.PrivateKey.(crypto.Signer)

	p := peer.Start(
		&peer.Config{
			Metrics:              metrics,
			MessageCreator:       mc,
			Log:                  logging.NoLog{},
			InboundMsgThrottler:  throttling.NewNoInboundThrottler(),
			Network:              peer.TestNetwork,
			Router:               router.InboundHandlerFunc(b.handle),
			VersionCompatibility: version.GetCompatibility(upgrade.InitiallyActiveTime),
			MySubnets:            set.Set[ids.ID]{},
			Beacons:              validators.NewManager(),
			Validators:           validators.NewManager(),
			NetworkID:            networkID,
			PingFrequency:        constants.DefaultPingFrequency,
			PongTimeout:          constants.DefaultPingPongTimeout,
			MaxClockDifference:   time.Minute,
			ResourceTracker:      resourceTracker,
			UptimeCalculator:     uptime.TestCalculator{},
			IPSigner: peer.NewIPSigner(
				utils.NewAtomic(netip.AddrPortFrom(netip.IPv6Loopback(), 1)),
				tlsKey,
				bls,
			),
		},
		conn,
		peerCert,
		peerID,
		peer.NewBlockingMessageQueue(metrics, logging.NoLog{}, 1024),
		false,
	)
	b.peer = p
	return b, p.AwaitReady(ctx)
}

// handle keeps the honest node's messages from blocking it. It keeps the
// replies to the peer's requests, answers consensus queries if the peer
// votes (vote), and discards the rest.
func (b *byzantinePeer) handle(ctx context.Context, msg *message.InboundMessage) {
	defer msg.OnFinishedHandling()

	switch m := msg.Message.(type) {
	case *p2ppb.Put:
		offer(b.puts, m)
	case *p2ppb.Chits:
		offer(b.chits, m)
	case *p2ppb.AcceptedFrontier:
		offer(b.frontiers, m)
	}
	if !b.votes {
		return
	}

	switch m := msg.Message.(type) {
	case *p2ppb.PushQuery:
		if blk, err := pvmblock.ParseWithoutVerification(m.Container); err == nil {
			b.vote(ctx, m.ChainId, m.RequestId, blk.ID(), m.RequestedHeight)
		}
	case *p2ppb.PullQuery:
		if blkID, err := ids.ToID(m.ContainerId); err == nil {
			b.vote(ctx, m.ChainId, m.RequestId, blkID, m.RequestedHeight)
		}
	}
}

// offer queues m on c, or drops it if c is full, so that a reply that nobody
// reads never blocks the honest node.
func offer[M any](c chan<- M, m M) {
	select {
	case c <- m:
	default:
	}
}

// vote answers a query with a vote for the block it asks about, at height.
//
// A validator peer that never answered would be benched after its second
// query timed out (snow/networking/benchlist/benchlist.go), and a node counts a benched validator as disconnected for
// uptime (snow/networking/router/chain_router.go Benched), so an online
// staker's reward would abort. Only a validator is queried.
func (b *byzantinePeer) vote(ctx context.Context, chainIDBytes []byte, requestID uint32, blkID ids.ID, height uint64) {
	chainID, err := ids.ToID(chainIDBytes)
	if err != nil {
		return
	}

	msg, err := b.peer.MessageCreator.Chits(chainID, requestID, blkID, blkID, blkID, height)
	if err != nil {
		return
	}
	b.peer.Send(ctx, msg)
}

// gossipTx push-gossips tx to the honest node, framed as the P-Chain's own tx
// gossip frames it.
func (b *byzantinePeer) gossipTx(ctx context.Context, tx *platform.Tx) error {
	gossipBytes, err := gossip.MarshalAppGossip([][]byte{tx.Bytes()})
	if err != nil {
		return err
	}

	msg, err := b.peer.MessageCreator.AppGossip(
		constants.PlatformChainID,
		p2p.PrefixMessage(p2p.ProtocolPrefix(p2p.TxGossipHandlerID), gossipBytes),
	)
	if err != nil {
		return err
	}

	if !b.peer.Send(ctx, msg) {
		return errNotSent
	}
	return nil
}

// getBlock asks the honest node for the block that blkID names, on chainID,
// and returns its bytes. For the P-Chain, chainID is the proposervm chain and
// the bytes are a proposervm block.
func (b *byzantinePeer) getBlock(ctx context.Context, chainID, blkID ids.ID) ([]byte, error) {
	requestID := b.requestID.Add(1)
	msg, err := b.peer.MessageCreator.Get(chainID, requestID, 10*time.Second, blkID)
	if err != nil {
		return nil, err
	}
	if !b.peer.Send(ctx, msg) {
		return nil, errNotSent
	}

	put, err := reply(ctx, b.puts, requestID)
	if err != nil {
		return nil, fmt.Errorf("getting block %s: %w", blkID, err)
	}

	return put.Container, nil
}

// preferred returns the ID of the block that the honest node prefers on
// chainID: it asks for the node's last accepted block, and then sends a pull
// query for that block, which the node answers with its preference
// (snow/engine/snowman/engine.go sendChits). For the P-Chain, it is a
// proposervm block ID.
func (b *byzantinePeer) preferred(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	requestID := b.requestID.Add(1)
	msg, err := b.peer.MessageCreator.GetAcceptedFrontier(chainID, requestID, 10*time.Second)
	if err != nil {
		return ids.Empty, err
	}
	if !b.peer.Send(ctx, msg) {
		return ids.Empty, errNotSent
	}
	frontier, err := reply(ctx, b.frontiers, requestID)
	if err != nil {
		return ids.Empty, err
	}
	acceptedID, err := ids.ToID(frontier.ContainerId)
	if err != nil {
		return ids.Empty, err
	}

	requestID = b.requestID.Add(1)
	msg, err = b.peer.MessageCreator.PullQuery(chainID, requestID, 10*time.Second, acceptedID, 0)
	if err != nil {
		return ids.Empty, err
	}
	if !b.peer.Send(ctx, msg) {
		return ids.Empty, errNotSent
	}
	chits, err := reply(ctx, b.chits, requestID)
	if err != nil {
		return ids.Empty, err
	}

	return ids.ToID(chits.PreferredId)
}

// reply returns the message from c that answers requestID, and discards the
// replies to earlier requests.
func reply[M interface{ GetRequestId() uint32 }](ctx context.Context, c <-chan M, requestID uint32) (M, error) {
	for {
		select {
		case m := <-c:
			if m.GetRequestId() == requestID {
				return m, nil
			}
		case <-ctx.Done():
			var zero M
			return zero, fmt.Errorf("awaiting the reply to request %d: %w", requestID, ctx.Err())
		}
	}
}

// proposeBlock sends blockBytes to the honest node as a push query, as a
// proposer does. blockBytes is a proposervm block.
func (b *byzantinePeer) proposeBlock(ctx context.Context, chainID ids.ID, blockBytes []byte) error {
	msg, err := b.peer.MessageCreator.PushQuery(chainID, b.requestID.Add(1), 10*time.Second, blockBytes, 0)
	if err != nil {
		return err
	}
	if !b.peer.Send(ctx, msg) {
		return errNotSent
	}
	return nil
}

// close disconnects the peer and waits until it has stopped. A closed peer
// closes again at once.
func (b *byzantinePeer) close(ctx context.Context) error {
	b.peer.StartClose()
	return b.peer.AwaitClosed(ctx)
}

// closePeers closes each of peers.
func closePeers(ctx context.Context, peers []*byzantinePeer) error {
	for _, p := range peers {
		if err := p.close(ctx); err != nil {
			return err
		}
	}

	return nil
}
