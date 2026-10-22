// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"context"
	"crypto"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
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
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
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
	"github.com/ava-labs/avalanchego/vms/proposervm/acp181"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"

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

	requestID atomic.Uint32
	puts      chan *p2ppb.Put // inbound Put messages
}

// connectByzantine connects a new non-validator peer to the honest node that
// listens at addr. Its staking key is fresh, so it is no validator.
func connectByzantine(ctx context.Context, addr netip.AddrPort) (*byzantinePeer, error) {
	cert, err := staking.NewTLSCert()
	if err != nil {
		return nil, err
	}
	bls, err := localsigner.New()
	if err != nil {
		return nil, err
	}
	return startPeer(ctx, addr, cert, bls)
}

// connectValidator connects a peer that holds key's staking keys, so the
// honest node sees it as that validator and verifies its blocks in that
// validator's proposer slots.
func connectValidator(ctx context.Context, addr netip.AddrPort, key validatorKey) (*byzantinePeer, error) {
	return startPeer(ctx, addr, key.cert, key.bls)
}

// startPeer dials addr and completes the handshake with cert and bls. The
// node checks the handshake's BLS signature against bls's public key, so a
// validator peer must use that validator's BLS key.
func startPeer(ctx context.Context, addr netip.AddrPort, cert *tls.Certificate, bls *localsigner.LocalSigner) (*byzantinePeer, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, constants.NetworkType, addr.String())
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

	b := &byzantinePeer{puts: make(chan *p2ppb.Put, 1024)}
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

// handle keeps the honest node's messages from blocking it. It keeps Put
// replies and discards the rest.
func (b *byzantinePeer) handle(_ context.Context, msg *message.InboundMessage) {
	defer msg.OnFinishedHandling()

	if put, ok := msg.Message.(*p2ppb.Put); ok {
		select {
		case b.puts <- put:
		default:
		}
	}
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
	// Drain stale replies so that the next Put is the answer to this Get.
	for len(b.puts) > 0 {
		<-b.puts
	}

	msg, err := b.peer.MessageCreator.Get(chainID, b.requestID.Add(1), 10*time.Second, blkID)
	if err != nil {
		return nil, err
	}
	if !b.peer.Send(ctx, msg) {
		return nil, errNotSent
	}

	select {
	case put := <-b.puts:
		return put.Container, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("getting block %s: %w", blkID, ctx.Err())
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

// close disconnects the peer and waits until it has stopped.
func (b *byzantinePeer) close(ctx context.Context) error {
	b.peer.StartClose()
	return b.peer.AwaitClosed(ctx)
}

// proposerClock mirrors proposervm's slot timing.
const windowDuration = 5 * time.Second

// buildProposerBlock builds a proposervm block that the validator key is the
// expected proposer for, wrapping inner, on top of the parent proposervm
// block parentBytes. It waits until the block's slot so that an honest node
// verifies it. vdrs is every validator's node ID; they have equal weight.
func (key validatorKey) buildProposerBlock(
	ctx context.Context,
	chainID ids.ID,
	parentBytes []byte,
	inner platform.Block,
	vdrs []ids.NodeID,
) ([]byte, error) {
	parent, err := pvmblock.Parse(parentBytes, chainID)
	if err != nil {
		return nil, err
	}
	signed, ok := parent.(pvmblock.SignedBlock)
	if !ok {
		return nil, fmt.Errorf("parent %s is not a signed block", parent.ID())
	}

	windower := proposer.New(staticValidators(vdrs), constants.PrimaryNetworkID, chainID, logging.NoLog{})
	blkHeight := inner.Height() // the proposervm height equals the inner height
	pChainHeight := signed.PChainHeight()

	// Find the next slot that this validator is the proposer for, from now.
	parentTime := signed.Timestamp()
	stakingCert, err := staking.ParseCertificate(key.cert.Leaf.Raw)
	if err != nil {
		return nil, err
	}
	nodeID := ids.NodeIDFromCert(stakingCert)

	const maxSlots = 720 // one hour of 5s slots
	startSlot := proposer.TimeToSlot(parentTime, time.Now())
	for slot := startSlot; slot < startSlot+maxSlots; slot++ {
		proposerID, err := windower.ExpectedProposer(ctx, blkHeight, pChainHeight, slot)
		if err != nil {
			return nil, err
		}
		if proposerID != nodeID {
			continue
		}

		blkTime := parentTime.Add(time.Duration(slot) * windowDuration)
		if blkTime.Before(parentTime) {
			continue
		}
		// Wait until the node's clock is at or past the slot, within maxSkew.
		if wait := time.Until(blkTime); wait > 0 {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		epoch := acp181.NewEpoch(testUpgrades(), pChainHeight, signed.PChainEpoch(), parentTime, blkTime)
		block, err := pvmblock.Build(
			parent.ID(),
			blkTime,
			pChainHeight,
			epoch,
			stakingCert,
			inner.Bytes(),
			chainID,
			key.cert.PrivateKey.(crypto.Signer),
		)
		if err != nil {
			return nil, err
		}
		return block.Bytes(), nil
	}
	return nil, fmt.Errorf("validator %s has no proposer slot within %d slots", nodeID, maxSlots)
}

// staticValidators returns a validator state where every node in vdrs has
// equal weight, as the test genesis sets.
func staticValidators(vdrs []ids.NodeID) validators.State {
	set := make(map[ids.NodeID]*validators.GetValidatorOutput, len(vdrs))
	for _, id := range vdrs {
		set[id] = &validators.GetValidatorOutput{NodeID: id, Weight: 1}
	}
	return &validatorstest.State{
		GetSubnetIDF:      func(context.Context, ids.ID) (ids.ID, error) { return constants.PrimaryNetworkID, nil },
		GetMinimumHeightF: func(context.Context) (uint64, error) { return 0, nil },
		GetCurrentHeightF: func(context.Context) (uint64, error) { return 0, nil },
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			return set, nil
		},
	}
}
