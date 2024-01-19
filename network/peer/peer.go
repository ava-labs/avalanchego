// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"bufio"
	"context"
	"errors"
	"io"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
)

// maxBloomSaltLen restricts the allowed size of the bloom salt to prevent
// excessively expensive bloom filter contains checks.
const maxBloomSaltLen = 32

var (
	errClosed = errors.New("closed")

	_ Peer = (*peer)(nil)
)

// Peer encapsulates all of the functionality required to send and receive
// messages with a remote peer.
type Peer interface {
	// ID returns the nodeID of the remote peer.
	ID() ids.NodeID

	// Cert returns the certificate that the remote peer is using to
	// authenticate their messages.
	Cert() *staking.Certificate

	// LastSent returns the last time a message was sent to the peer.
	LastSent() time.Time

	// LastReceived returns the last time a message was received from the peer.
	LastReceived() time.Time

	// Ready returns true if the peer has finished the p2p handshake and is
	// ready to send and receive messages.
	Ready() bool

	// AwaitReady will block until the peer has finished the p2p handshake. If
	// the context is cancelled or the peer starts closing, then an error will
	// be returned.
	AwaitReady(ctx context.Context) error

	// Info returns a description of the state of this peer. It should only be
	// called after [Ready] returns true.
	Info() Info

	// IP returns the claimed IP and signature provided by this peer during the
	// handshake. It should only be called after [Ready] returns true.
	IP() *SignedIP

	// Version returns the claimed node version this peer is running. It should
	// only be called after [Ready] returns true.
	Version() *version.Application

	// TrackedSubnets returns the subnets this peer is running. It should only
	// be called after [Ready] returns true.
	TrackedSubnets() set.Set[ids.ID]

	// ObservedUptime returns the local node's subnet uptime according to the
	// peer. The value ranges from [0, 100]. It should only be called after
	// [Ready] returns true.
	ObservedUptime(subnetID ids.ID) (uint32, bool)

	// Send attempts to send [msg] to the peer. The peer takes ownership of
	// [msg] for reference counting. This returns false if the message is
	// guaranteed not to be delivered to the peer.
	Send(ctx context.Context, msg message.OutboundMessage) bool

	// StartSendPeerList attempts to send a PeerList message to this peer on
	// this peer's gossip routine. It is not guaranteed that a PeerList will be
	// sent.
	StartSendPeerList()

	// StartSendGetPeerList attempts to send a GetPeerList message to this peer
	// on this peer's gossip routine. It is not guaranteed that a GetPeerList
	// will be sent.
	StartSendGetPeerList()

	// StartClose will begin shutting down the peer. It will not block.
	StartClose()

	// Closed returns true once the peer has been fully shutdown. It is
	// guaranteed that no more messages will be received by this peer once this
	// returns true.
	Closed() bool

	// AwaitClosed will block until the peer has been fully shutdown. If the
	// context is cancelled, then an error will be returned.
	AwaitClosed(ctx context.Context) error
}

type peer struct {
	*Config

	// the connection object that is used to read/write messages from
	conn net.Conn

	// [cert] is this peer's certificate, specifically the leaf of the
	// certificate chain they provided.
	cert *staking.Certificate

	// node ID of this peer.
	id ids.NodeID

	// queue of messages to send to this peer.
	messageQueue MessageQueue

	// ip is the claimed IP the peer gave us in the Handshake message.
	ip *SignedIP
	// version is the claimed version the peer is running that we received in
	// the Handshake message.
	version *version.Application
	// trackedSubnets is the subset of subnetIDs the peer sent us in the Handshake
	// message that we are also tracking.
	trackedSubnets set.Set[ids.ID]
	// options of ACPs provided in the Handshake message.
	supportedACPs set.Set[uint32]
	objectedACPs  set.Set[uint32]

	observedUptimesLock sync.RWMutex
	// [observedUptimesLock] must be held while accessing [observedUptime]
	// Subnet ID --> Our uptime for the given subnet as perceived by the peer
	observedUptimes map[ids.ID]uint32

	// True if this peer has sent us a valid Handshake message and
	// is running a compatible version.
	// Only modified on the connection's reader routine.
	gotHandshake utils.Atomic[bool]

	// True if the peer:
	// * Has sent us a Handshake message
	// * Has sent us a PeerList message
	// * Is running a compatible version
	// Only modified on the connection's reader routine.
	finishedHandshake utils.Atomic[bool]

	// onFinishHandshake is closed when the peer finishes the p2p handshake.
	onFinishHandshake chan struct{}

	// numExecuting is the number of goroutines this peer is currently using
	numExecuting     int64
	startClosingOnce sync.Once
	// onClosingCtx is canceled when the peer starts closing
	onClosingCtx context.Context
	// onClosingCtxCancel cancels onClosingCtx
	onClosingCtxCancel func()

	// onClosed is closed when the peer is closed
	onClosed chan struct{}

	// Unix time of the last message sent and received respectively
	// Must only be accessed atomically
	lastSent, lastReceived int64

	// peerListChan signals that we should attempt to send a PeerList to this
	// peer
	peerListChan chan struct{}

	// getPeerListChan signals that we should attempt to send a GetPeerList to
	// this peer
	getPeerListChan chan struct{}
}

// Start a new peer instance.
//
// Invariant: There must only be one peer running at a time with a reference to
// the same [config.InboundMsgThrottler].
func Start(
	config *Config,
	conn net.Conn,
	cert *staking.Certificate,
	id ids.NodeID,
	messageQueue MessageQueue,
) Peer {
	onClosingCtx, onClosingCtxCancel := context.WithCancel(context.Background())
	p := &peer{
		Config:             config,
		conn:               conn,
		cert:               cert,
		id:                 id,
		messageQueue:       messageQueue,
		onFinishHandshake:  make(chan struct{}),
		numExecuting:       3,
		onClosingCtx:       onClosingCtx,
		onClosingCtxCancel: onClosingCtxCancel,
		onClosed:           make(chan struct{}),
		observedUptimes:    make(map[ids.ID]uint32),
		peerListChan:       make(chan struct{}, 1),
		getPeerListChan:    make(chan struct{}, 1),
	}

	go p.readMessages()
	go p.writeMessages()
	go p.sendNetworkMessages()

	return p
}

func (p *peer) ID() ids.NodeID {
	return p.id
}

func (p *peer) Cert() *staking.Certificate {
	return p.cert
}

func (p *peer) LastSent() time.Time {
	return time.Unix(
		atomic.LoadInt64(&p.lastSent),
		0,
	)
}

func (p *peer) LastReceived() time.Time {
	return time.Unix(
		atomic.LoadInt64(&p.lastReceived),
		0,
	)
}

func (p *peer) Ready() bool {
	return p.finishedHandshake.Get()
}

func (p *peer) AwaitReady(ctx context.Context) error {
	select {
	case <-p.onFinishHandshake:
		return nil
	case <-p.onClosed:
		return errClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *peer) Info() Info {
	publicIPStr := ""
	if !p.ip.IsZero() {
		publicIPStr = p.ip.IPPort.String()
	}

	uptimes := make(map[ids.ID]json.Uint32, p.trackedSubnets.Len())

	for subnetID := range p.trackedSubnets {
		uptime, exist := p.ObservedUptime(subnetID)
		if !exist {
			continue
		}
		uptimes[subnetID] = json.Uint32(uptime)
	}

	primaryUptime, exist := p.ObservedUptime(constants.PrimaryNetworkID)
	if !exist {
		primaryUptime = 0
	}

	return Info{
		IP:                    p.conn.RemoteAddr().String(),
		PublicIP:              publicIPStr,
		ID:                    p.id,
		Version:               p.version.String(),
		LastSent:              p.LastSent(),
		LastReceived:          p.LastReceived(),
		ObservedUptime:        json.Uint32(primaryUptime),
		ObservedSubnetUptimes: uptimes,
		TrackedSubnets:        p.trackedSubnets,
		SupportedACPs:         p.supportedACPs,
		ObjectedACPs:          p.objectedACPs,
	}
}

func (p *peer) IP() *SignedIP {
	return p.ip
}

func (p *peer) Version() *version.Application {
	return p.version
}

func (p *peer) TrackedSubnets() set.Set[ids.ID] {
	return p.trackedSubnets
}

func (p *peer) ObservedUptime(subnetID ids.ID) (uint32, bool) {
	p.observedUptimesLock.RLock()
	defer p.observedUptimesLock.RUnlock()

	uptime, exist := p.observedUptimes[subnetID]
	return uptime, exist
}

func (p *peer) Send(ctx context.Context, msg message.OutboundMessage) bool {
	return p.messageQueue.Push(ctx, msg)
}

func (p *peer) StartSendPeerList() {
	select {
	case p.peerListChan <- struct{}{}:
	default:
	}
}

func (p *peer) StartSendGetPeerList() {
	select {
	case p.getPeerListChan <- struct{}{}:
	default:
	}
}

func (p *peer) StartClose() {
	p.startClosingOnce.Do(func() {
		if err := p.conn.Close(); err != nil {
			p.Log.Debug("failed to close connection",
				zap.Stringer("nodeID", p.id),
				zap.Error(err),
			)
		}

		p.messageQueue.Close()
		p.onClosingCtxCancel()
	})
}

func (p *peer) Closed() bool {
	select {
	case _, ok := <-p.onClosed:
		return !ok
	default:
		return false
	}
}

func (p *peer) AwaitClosed(ctx context.Context) error {
	select {
	case <-p.onClosed:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// close should be called at the end of each goroutine that has been spun up.
// When the last goroutine is exiting, the peer will be marked as closed.
func (p *peer) close() {
	if atomic.AddInt64(&p.numExecuting, -1) != 0 {
		return
	}

	p.Network.Disconnected(p.id)
	close(p.onClosed)
}

// Read and handle messages from this peer.
// When this method returns, the connection is closed.
func (p *peer) readMessages() {
	// Track this node with the inbound message throttler.
	p.InboundMsgThrottler.AddNode(p.id)
	defer func() {
		p.InboundMsgThrottler.RemoveNode(p.id)
		p.StartClose()
		p.close()
	}()

	// Continuously read and handle messages from this peer.
	reader := bufio.NewReaderSize(p.conn, p.Config.ReadBufferSize)
	msgLenBytes := make([]byte, wrappers.IntLen)
	for {
		// Time out and close connection if we can't read the message length
		if err := p.conn.SetReadDeadline(p.nextTimeout()); err != nil {
			p.Log.Verbo("error setting the connection read timeout",
				zap.Stringer("nodeID", p.id),
				zap.Error(err),
			)
			return
		}

		// Read the message length
		if _, err := io.ReadFull(reader, msgLenBytes); err != nil {
			p.Log.Verbo("error reading message",
				zap.Stringer("nodeID", p.id),
				zap.Error(err),
			)
			return
		}

		// Parse the message length
		msgLen, err := readMsgLen(msgLenBytes, constants.DefaultMaxMessageSize)
		if err != nil {
			p.Log.Verbo("error reading message length",
				zap.Stringer("nodeID", p.id),
				zap.Error(err),
			)
			return
		}

		// Wait until the throttler says we can proceed to read the message.
		//
		// Invariant: When done processing this message, onFinishedHandling() is
		// called exactly once. If this is not honored, the message throttler
		// will leak until no new messages can be read. You can look at message
		// throttler metrics to verify that there is no leak.
		//
		// Invariant: There must only be one call to Acquire at any given time
		// with the same nodeID. In this package, only this goroutine ever
		// performs Acquire. Additionally, we ensure that this goroutine has
		// exited before calling [Network.Disconnected] to guarantee that there
		// can't be multiple instances of this goroutine running over different
		// peer instances.
		onFinishedHandling := p.InboundMsgThrottler.Acquire(
			p.onClosingCtx,
			uint64(msgLen),
			p.id,
		)

		// If the peer is shutting down, there's no need to read the message.
		if err := p.onClosingCtx.Err(); err != nil {
			onFinishedHandling()
			return
		}

		// Time out and close connection if we can't read message
		if err := p.conn.SetReadDeadline(p.nextTimeout()); err != nil {
			p.Log.Verbo("error setting the connection read timeout",
				zap.Stringer("nodeID", p.id),
				zap.Error(err),
			)
			onFinishedHandling()
			return
		}

		// Read the message
		msgBytes := make([]byte, msgLen)
		if _, err := io.ReadFull(reader, msgBytes); err != nil {
			p.Log.Verbo("error reading message",
				zap.Stringer("nodeID", p.id),
				zap.Error(err),
			)
			onFinishedHandling()
			return
		}

		// Track the time it takes from now until the time the message is
		// handled (in the event this message is handled at the network level)
		// or the time the message is handed to the router (in the event this
		// message is not handled at the network level.)
		// [p.CPUTracker.StopProcessing] must be called when this loop iteration is
		// finished.
		p.ResourceTracker.StartProcessing(p.id, p.Clock.Time())

		p.Log.Verbo("parsing message",
			zap.Stringer("nodeID", p.id),
			zap.Binary("messageBytes", msgBytes),
		)

		// Parse the message
		msg, err := p.MessageCreator.Parse(msgBytes, p.id, onFinishedHandling)
		if err != nil {
			p.Log.Verbo("failed to parse message",
				zap.Stringer("nodeID", p.id),
				zap.Binary("messageBytes", msgBytes),
				zap.Error(err),
			)

			p.Metrics.FailedToParse.Inc()

			// Couldn't parse the message. Read the next one.
			onFinishedHandling()
			p.ResourceTracker.StopProcessing(p.id, p.Clock.Time())
			continue
		}

		now := p.Clock.Time()
		p.storeLastReceived(now)
		p.Metrics.Received(msg, msgLen)

		// Handle the message. Note that when we are done handling this message,
		// we must call [msg.OnFinishedHandling()].
		p.handle(msg)
		p.ResourceTracker.StopProcessing(p.id, p.Clock.Time())
	}
}

func (p *peer) writeMessages() {
	defer func() {
		p.StartClose()
		p.close()
	}()

	writer := bufio.NewWriterSize(p.conn, p.Config.WriteBufferSize)

	// Make sure that the Handshake is the first message sent
	mySignedIP, err := p.IPSigner.GetSignedIP()
	if err != nil {
		p.Log.Error("failed to get signed IP",
			zap.Stringer("nodeID", p.id),
			zap.Error(err),
		)
		return
	}
	if mySignedIP.Port == 0 {
		p.Log.Error("signed IP has invalid port",
			zap.Stringer("nodeID", p.id),
			zap.Uint16("port", mySignedIP.Port),
		)
		return
	}

	myVersion := p.VersionCompatibility.Version()
	legacyApplication := &version.Application{
		Name:  version.LegacyAppName,
		Major: myVersion.Major,
		Minor: myVersion.Minor,
		Patch: myVersion.Patch,
	}

	knownPeersFilter, knownPeersSalt := p.Network.KnownPeers()

	msg, err := p.MessageCreator.Handshake(
		p.NetworkID,
		p.Clock.Unix(),
		mySignedIP.IPPort,
		legacyApplication.String(),
		myVersion.Name,
		uint32(myVersion.Major),
		uint32(myVersion.Minor),
		uint32(myVersion.Patch),
		mySignedIP.Timestamp,
		mySignedIP.Signature,
		p.MySubnets.List(),
		p.SupportedACPs,
		p.ObjectedACPs,
		knownPeersFilter,
		knownPeersSalt,
	)
	if err != nil {
		p.Log.Error("failed to create message",
			zap.Stringer("messageOp", message.HandshakeOp),
			zap.Stringer("nodeID", p.id),
			zap.Error(err),
		)
		return
	}

	p.writeMessage(writer, msg)

	for {
		msg, ok := p.messageQueue.PopNow()
		if ok {
			p.writeMessage(writer, msg)
			continue
		}

		// Make sure the peer was fully sent all prior messages before
		// blocking.
		if err := writer.Flush(); err != nil {
			p.Log.Verbo("failed to flush writer",
				zap.Stringer("nodeID", p.id),
				zap.Error(err),
			)
			return
		}

		msg, ok = p.messageQueue.Pop()
		if !ok {
			// This peer is closing
			return
		}

		p.writeMessage(writer, msg)
	}
}

func (p *peer) writeMessage(writer io.Writer, msg message.OutboundMessage) {
	msgBytes := msg.Bytes()
	p.Log.Verbo("sending message",
		zap.Stringer("nodeID", p.id),
		zap.Binary("messageBytes", msgBytes),
	)

	if err := p.conn.SetWriteDeadline(p.nextTimeout()); err != nil {
		p.Log.Verbo("error setting write deadline",
			zap.Stringer("nodeID", p.id),
			zap.Error(err),
		)
		return
	}

	msgLen := uint32(len(msgBytes))
	msgLenBytes, err := writeMsgLen(msgLen, constants.DefaultMaxMessageSize)
	if err != nil {
		p.Log.Verbo("error writing message length",
			zap.Stringer("nodeID", p.id),
			zap.Error(err),
		)
		return
	}

	// Write the message
	var buf net.Buffers = [][]byte{msgLenBytes[:], msgBytes}
	if _, err := io.CopyN(writer, &buf, int64(wrappers.IntLen+msgLen)); err != nil {
		p.Log.Verbo("error writing message",
			zap.Stringer("nodeID", p.id),
			zap.Error(err),
		)
		return
	}

	now := p.Clock.Time()
	p.storeLastSent(now)
	p.Metrics.Sent(msg)
}

func (p *peer) sendNetworkMessages() {
	sendPingsTicker := time.NewTicker(p.PingFrequency)
	defer func() {
		sendPingsTicker.Stop()

		p.StartClose()
		p.close()
	}()

	for {
		select {
		case <-p.peerListChan:
			peerIPs := p.Config.Network.Peers(p.id, bloom.EmptyFilter, nil)
			if len(peerIPs) == 0 {
				p.Log.Verbo(
					"skipping peer gossip as there are no unknown peers",
					zap.Stringer("nodeID", p.id),
				)
				continue
			}

			// Bypass throttling is disabled here to follow the non-handshake
			// message sending pattern.
			msg, err := p.Config.MessageCreator.PeerList(peerIPs, false /*=bypassThrottling*/)
			if err != nil {
				p.Log.Error("failed to create peer list message",
					zap.Stringer("nodeID", p.id),
					zap.Error(err),
				)
				continue
			}

			if !p.Send(p.onClosingCtx, msg) {
				p.Log.Debug("failed to send peer list",
					zap.Stringer("nodeID", p.id),
				)
			}
		case <-p.getPeerListChan:
			knownPeersFilter, knownPeersSalt := p.Config.Network.KnownPeers()
			msg, err := p.Config.MessageCreator.GetPeerList(knownPeersFilter, knownPeersSalt)
			if err != nil {
				p.Log.Error("failed to create get peer list message",
					zap.Stringer("nodeID", p.id),
					zap.Error(err),
				)
				continue
			}

			if !p.Send(p.onClosingCtx, msg) {
				p.Log.Debug("failed to send get peer list",
					zap.Stringer("nodeID", p.id),
				)
			}
		case <-sendPingsTicker.C:
			if !p.Network.AllowConnection(p.id) {
				p.Log.Debug("disconnecting from peer",
					zap.String("reason", "connection is no longer desired"),
					zap.Stringer("nodeID", p.id),
				)
				return
			}

			if p.finishedHandshake.Get() {
				if err := p.VersionCompatibility.Compatible(p.version); err != nil {
					p.Log.Debug("disconnecting from peer",
						zap.String("reason", "version not compatible"),
						zap.Stringer("nodeID", p.id),
						zap.Stringer("peerVersion", p.version),
						zap.Error(err),
					)
					return
				}
			}

			primaryUptime, subnetUptimes := p.getUptimes()
			pingMessage, err := p.MessageCreator.Ping(primaryUptime, subnetUptimes)
			if err != nil {
				p.Log.Error("failed to create message",
					zap.Stringer("messageOp", message.PingOp),
					zap.Stringer("nodeID", p.id),
					zap.Error(err),
				)
				return
			}

			p.Send(p.onClosingCtx, pingMessage)
		case <-p.onClosingCtx.Done():
			return
		}
	}
}

func (p *peer) handle(msg message.InboundMessage) {
	switch m := msg.Message().(type) { // Network-related message types
	case *p2p.Ping:
		p.handlePing(m)
		msg.OnFinishedHandling()
		return
	case *p2p.Pong:
		p.handlePong(m)
		msg.OnFinishedHandling()
		return
	case *p2p.Handshake:
		p.handleHandshake(m)
		msg.OnFinishedHandling()
		return
	case *p2p.GetPeerList:
		p.handleGetPeerList(m)
		msg.OnFinishedHandling()
		return
	case *p2p.PeerList:
		p.handlePeerList(m)
		msg.OnFinishedHandling()
		return
	}
	if !p.finishedHandshake.Get() {
		p.Log.Debug(
			"dropping message",
			zap.String("reason", "handshake isn't finished"),
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", msg.Op()),
		)
		msg.OnFinishedHandling()
		return
	}

	// Consensus and app-level messages
	p.Router.HandleInbound(context.Background(), msg)
}

func (p *peer) handlePing(msg *p2p.Ping) {
	p.observeUptimes(msg.Uptime, msg.SubnetUptimes)

	primaryUptime, subnetUptimes := p.getUptimes()
	pongMessage, err := p.MessageCreator.Pong(primaryUptime, subnetUptimes)
	if err != nil {
		p.Log.Error("failed to create message",
			zap.Stringer("messageOp", message.PongOp),
			zap.Stringer("nodeID", p.id),
			zap.Error(err),
		)
		return
	}
	p.Send(p.onClosingCtx, pongMessage)
}

func (p *peer) getUptimes() (uint32, []*p2p.SubnetUptime) {
	primaryUptime, err := p.UptimeCalculator.CalculateUptimePercent(
		p.id,
		constants.PrimaryNetworkID,
	)
	if err != nil {
		p.Log.Debug("failed to get peer primary uptime percentage",
			zap.Stringer("nodeID", p.id),
			zap.Stringer("subnetID", constants.PrimaryNetworkID),
			zap.Error(err),
		)
		primaryUptime = 0
	}

	subnetUptimes := make([]*p2p.SubnetUptime, 0, p.trackedSubnets.Len())
	for subnetID := range p.trackedSubnets {
		subnetUptime, err := p.UptimeCalculator.CalculateUptimePercent(p.id, subnetID)
		if err != nil {
			p.Log.Debug("failed to get peer uptime percentage",
				zap.Stringer("nodeID", p.id),
				zap.Stringer("subnetID", subnetID),
				zap.Error(err),
			)
			continue
		}

		subnetID := subnetID
		subnetUptimes = append(subnetUptimes, &p2p.SubnetUptime{
			SubnetId: subnetID[:],
			Uptime:   uint32(subnetUptime * 100),
		})
	}

	primaryUptimePercent := uint32(primaryUptime * 100)
	return primaryUptimePercent, subnetUptimes
}

func (p *peer) handlePong(msg *p2p.Pong) {
	// TODO: Remove once everyone sends uptimes in Ping messages.
	p.observeUptimes(msg.Uptime, msg.SubnetUptimes)
}

func (p *peer) observeUptimes(primaryUptime uint32, subnetUptimes []*p2p.SubnetUptime) {
	// TODO: Remove once everyone sends uptimes in Ping messages.
	//
	// If primaryUptime is 0, the message may not include any uptimes. This may
	// happen with old Ping messages or new Pong messages.
	if primaryUptime == 0 {
		return
	}

	if primaryUptime > 100 {
		p.Log.Debug("dropping message with invalid uptime",
			zap.Stringer("nodeID", p.id),
			zap.Stringer("subnetID", constants.PrimaryNetworkID),
			zap.Uint32("uptime", primaryUptime),
		)
		p.StartClose()
		return
	}
	p.observeUptime(constants.PrimaryNetworkID, primaryUptime)

	for _, subnetUptime := range subnetUptimes {
		subnetID, err := ids.ToID(subnetUptime.SubnetId)
		if err != nil {
			p.Log.Debug("dropping message with invalid subnetID",
				zap.Stringer("nodeID", p.id),
				zap.Error(err),
			)
			p.StartClose()
			return
		}

		if !p.MySubnets.Contains(subnetID) {
			p.Log.Debug("dropping message with unexpected subnetID",
				zap.Stringer("nodeID", p.id),
				zap.Stringer("subnetID", subnetID),
			)
			p.StartClose()
			return
		}

		uptime := subnetUptime.Uptime
		if uptime > 100 {
			p.Log.Debug("dropping message with invalid uptime",
				zap.Stringer("nodeID", p.id),
				zap.Stringer("subnetID", subnetID),
				zap.Uint32("uptime", uptime),
			)
			p.StartClose()
			return
		}
		p.observeUptime(subnetID, uptime)
	}
}

// Record that the given peer perceives our uptime for the given [subnetID]
// to be [uptime].
// Assumes [uptime] is in the range [0, 100] and [subnetID] is a valid ID of a
// subnet this peer tracks.
func (p *peer) observeUptime(subnetID ids.ID, uptime uint32) {
	p.observedUptimesLock.Lock()
	p.observedUptimes[subnetID] = uptime // [0, 100] percentage
	p.observedUptimesLock.Unlock()
}

func (p *peer) handleHandshake(msg *p2p.Handshake) {
	if p.gotHandshake.Get() {
		// TODO: this should never happen, should we close the connection here?
		p.Log.Verbo("dropping duplicated handshake message",
			zap.Stringer("nodeID", p.id),
		)
		return
	}

	if msg.NetworkId != p.NetworkID {
		p.Log.Debug("networkID mismatch",
			zap.Stringer("nodeID", p.id),
			zap.Uint32("peerNetworkID", msg.NetworkId),
			zap.Uint32("ourNetworkID", p.NetworkID),
		)
		p.StartClose()
		return
	}

	myTime := p.Clock.Time()
	myTimeUnix := uint64(myTime.Unix())
	clockDifference := math.Abs(float64(msg.MyTime) - float64(myTimeUnix))

	p.Metrics.ClockSkew.Observe(clockDifference)

	if clockDifference > p.MaxClockDifference.Seconds() {
		if _, ok := p.Beacons.GetValidator(constants.PrimaryNetworkID, p.id); ok {
			p.Log.Warn("beacon reports out of sync time",
				zap.Stringer("nodeID", p.id),
				zap.Uint64("peerTime", msg.MyTime),
				zap.Uint64("myTime", myTimeUnix),
			)
		} else {
			p.Log.Debug("peer reports out of sync time",
				zap.Stringer("nodeID", p.id),
				zap.Uint64("peerTime", msg.MyTime),
				zap.Uint64("myTime", myTimeUnix),
			)
		}
		p.StartClose()
		return
	}

	if msg.Client != nil {
		p.version = &version.Application{
			Name:  msg.Client.Name,
			Major: int(msg.Client.Major),
			Minor: int(msg.Client.Minor),
			Patch: int(msg.Client.Patch),
		}
	} else {
		// Handle legacy version field
		peerVersion, err := version.ParseLegacyApplication(msg.MyVersion)
		if err != nil {
			p.Log.Debug("failed to parse peer version",
				zap.Stringer("nodeID", p.id),
				zap.Error(err),
			)
			p.StartClose()
			return
		}
		p.version = peerVersion
	}

	if p.VersionCompatibility.Version().Before(p.version) {
		if _, ok := p.Beacons.GetValidator(constants.PrimaryNetworkID, p.id); ok {
			p.Log.Info("beacon attempting to connect with newer version. You may want to update your client",
				zap.Stringer("nodeID", p.id),
				zap.Stringer("beaconVersion", p.version),
			)
		} else {
			p.Log.Debug("peer attempting to connect with newer version. You may want to update your client",
				zap.Stringer("nodeID", p.id),
				zap.Stringer("peerVersion", p.version),
			)
		}
	}

	if err := p.VersionCompatibility.Compatible(p.version); err != nil {
		p.Log.Verbo("peer version not compatible",
			zap.Stringer("nodeID", p.id),
			zap.Stringer("peerVersion", p.version),
			zap.Error(err),
		)
		p.StartClose()
		return
	}

	// handle subnet IDs
	for _, subnetIDBytes := range msg.TrackedSubnets {
		subnetID, err := ids.ToID(subnetIDBytes)
		if err != nil {
			p.Log.Debug("failed to parse peer's tracked subnets",
				zap.Stringer("nodeID", p.id),
				zap.Error(err),
			)
			p.StartClose()
			return
		}
		// add only if we also track this subnet
		if p.MySubnets.Contains(subnetID) {
			p.trackedSubnets.Add(subnetID)
		}
	}

	for _, acp := range msg.SupportedAcps {
		if constants.CurrentACPs.Contains(acp) {
			p.supportedACPs.Add(acp)
		}
	}
	for _, acp := range msg.ObjectedAcps {
		if constants.CurrentACPs.Contains(acp) {
			p.objectedACPs.Add(acp)
		}
	}

	if p.supportedACPs.Overlaps(p.objectedACPs) {
		p.Log.Debug("message with invalid field",
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.HandshakeOp),
			zap.String("field", "ACPs"),
			zap.Reflect("supportedACPs", p.supportedACPs),
			zap.Reflect("objectedACPs", p.objectedACPs),
		)
		p.StartClose()
		return
	}

	var (
		knownPeers = bloom.EmptyFilter
		salt       []byte
	)
	if msg.KnownPeers != nil {
		var err error
		knownPeers, err = bloom.Parse(msg.KnownPeers.Filter)
		if err != nil {
			p.Log.Debug("message with invalid field",
				zap.Stringer("nodeID", p.id),
				zap.Stringer("messageOp", message.HandshakeOp),
				zap.String("field", "KnownPeers.Filter"),
				zap.Error(err),
			)
			p.StartClose()
			return
		}

		salt = msg.KnownPeers.Salt
		if saltLen := len(salt); saltLen > maxBloomSaltLen {
			p.Log.Debug("message with invalid field",
				zap.Stringer("nodeID", p.id),
				zap.Stringer("messageOp", message.HandshakeOp),
				zap.String("field", "KnownPeers.Salt"),
				zap.Int("saltLen", saltLen),
			)
			p.StartClose()
			return
		}
	}

	// "net.IP" type in Golang is 16-byte
	if ipLen := len(msg.IpAddr); ipLen != net.IPv6len {
		p.Log.Debug("message with invalid field",
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.HandshakeOp),
			zap.String("field", "IP"),
			zap.Int("ipLen", ipLen),
		)
		p.StartClose()
		return
	}
	if msg.IpPort == 0 {
		p.Log.Debug("message with invalid field",
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.HandshakeOp),
			zap.String("field", "Port"),
			zap.Uint32("port", msg.IpPort),
		)
		p.StartClose()
		return
	}

	p.ip = &SignedIP{
		UnsignedIP: UnsignedIP{
			IPPort: ips.IPPort{
				IP:   msg.IpAddr,
				Port: uint16(msg.IpPort),
			},
			Timestamp: msg.IpSigningTime,
		},
		Signature: msg.Sig,
	}
	maxTimestamp := myTime.Add(p.MaxClockDifference)
	if err := p.ip.Verify(p.cert, maxTimestamp); err != nil {
		if _, ok := p.Beacons.GetValidator(constants.PrimaryNetworkID, p.id); ok {
			p.Log.Warn("beacon has invalid signature or is out of sync",
				zap.Stringer("nodeID", p.id),
				zap.Uint64("peerTime", msg.MyTime),
				zap.Uint64("myTime", myTimeUnix),
				zap.Error(err),
			)
		} else {
			p.Log.Debug("peer has invalid signature or is out of sync",
				zap.Stringer("nodeID", p.id),
				zap.Uint64("peerTime", msg.MyTime),
				zap.Uint64("myTime", myTimeUnix),
				zap.Error(err),
			)
		}

		p.StartClose()
		return
	}

	p.gotHandshake.Set(true)

	peerIPs := p.Network.Peers(p.id, knownPeers, salt)

	// We bypass throttling here to ensure that the handshake message is
	// acknowledged correctly.
	peerListMsg, err := p.Config.MessageCreator.PeerList(peerIPs, true /*=bypassThrottling*/)
	if err != nil {
		p.Log.Error("failed to create peer list handshake message",
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.PeerListOp),
			zap.Error(err),
		)
		return
	}

	if !p.Send(p.onClosingCtx, peerListMsg) {
		// Because throttling was marked to be bypassed with this message,
		// sending should only fail if the peer has started closing.
		p.Log.Debug("failed to send peer list for handshake",
			zap.Stringer("nodeID", p.id),
			zap.Error(p.onClosingCtx.Err()),
		)
	}
}

func (p *peer) handleGetPeerList(msg *p2p.GetPeerList) {
	if !p.finishedHandshake.Get() {
		p.Log.Verbo("dropping get peer list message",
			zap.Stringer("nodeID", p.id),
		)
		return
	}

	knownPeersMsg := msg.GetKnownPeers()
	filter, err := bloom.Parse(knownPeersMsg.GetFilter())
	if err != nil {
		p.Log.Debug("message with invalid field",
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.GetPeerListOp),
			zap.String("field", "KnownPeers.Filter"),
			zap.Error(err),
		)
		p.StartClose()
		return
	}

	salt := knownPeersMsg.GetSalt()
	if saltLen := len(salt); saltLen > maxBloomSaltLen {
		p.Log.Debug("message with invalid field",
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.GetPeerListOp),
			zap.String("field", "KnownPeers.Salt"),
			zap.Int("saltLen", saltLen),
		)
		p.StartClose()
		return
	}

	peerIPs := p.Network.Peers(p.id, filter, salt)
	if len(peerIPs) == 0 {
		p.Log.Debug("skipping sending of empty peer list",
			zap.Stringer("nodeID", p.id),
		)
		return
	}

	// Bypass throttling is disabled here to follow the non-handshake message
	// sending pattern.
	peerListMsg, err := p.Config.MessageCreator.PeerList(peerIPs, false /*=bypassThrottling*/)
	if err != nil {
		p.Log.Error("failed to create peer list message",
			zap.Stringer("nodeID", p.id),
			zap.Error(err),
		)
		return
	}

	if !p.Send(p.onClosingCtx, peerListMsg) {
		p.Log.Debug("failed to send peer list",
			zap.Stringer("nodeID", p.id),
		)
	}
}

func (p *peer) handlePeerList(msg *p2p.PeerList) {
	if !p.finishedHandshake.Get() {
		if !p.gotHandshake.Get() {
			return
		}

		p.Network.Connected(p.id)
		p.finishedHandshake.Set(true)
		close(p.onFinishHandshake)
	}

	// Invariant: We do not account for clock skew here, as the sender of the
	// certificate is expected to account for clock skew during the activation
	// of Durango.
	durangoTime := version.GetDurangoTime(p.NetworkID)
	beforeDurango := time.Now().Before(durangoTime)
	discoveredIPs := make([]*ips.ClaimedIPPort, len(msg.ClaimedIpPorts)) // the peers this peer told us about
	for i, claimedIPPort := range msg.ClaimedIpPorts {
		var (
			tlsCert *staking.Certificate
			err     error
		)
		if beforeDurango {
			tlsCert, err = staking.ParseCertificate(claimedIPPort.X509Certificate)
		} else {
			tlsCert, err = staking.ParseCertificatePermissive(claimedIPPort.X509Certificate)
		}
		if err != nil {
			p.Log.Debug("message with invalid field",
				zap.Stringer("nodeID", p.id),
				zap.Stringer("messageOp", message.PeerListOp),
				zap.String("field", "Cert"),
				zap.Error(err),
			)
			p.StartClose()
			return
		}

		// "net.IP" type in Golang is 16-byte
		if ipLen := len(claimedIPPort.IpAddr); ipLen != net.IPv6len {
			p.Log.Debug("message with invalid field",
				zap.Stringer("nodeID", p.id),
				zap.Stringer("messageOp", message.PeerListOp),
				zap.String("field", "IP"),
				zap.Int("ipLen", ipLen),
			)
			p.StartClose()
			return
		}
		if claimedIPPort.IpPort == 0 {
			p.Log.Debug("message with invalid field",
				zap.Stringer("nodeID", p.id),
				zap.Stringer("messageOp", message.PeerListOp),
				zap.String("field", "Port"),
				zap.Uint32("port", claimedIPPort.IpPort),
			)
			// TODO: After v1.11.x is activated, close the peer here.
			continue
		}

		discoveredIPs[i] = ips.NewClaimedIPPort(
			tlsCert,
			ips.IPPort{
				IP:   claimedIPPort.IpAddr,
				Port: uint16(claimedIPPort.IpPort),
			},
			claimedIPPort.Timestamp,
			claimedIPPort.Signature,
		)
	}

	if err := p.Network.Track(discoveredIPs); err != nil {
		p.Log.Debug("message with invalid field",
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.PeerListOp),
			zap.String("field", "claimedIP"),
			zap.Error(err),
		)
		p.StartClose()
	}
}

func (p *peer) nextTimeout() time.Time {
	return p.Clock.Time().Add(p.PongTimeout)
}

func (p *peer) storeLastSent(time time.Time) {
	unixTime := time.Unix()
	atomic.StoreInt64(&p.Config.LastSent, unixTime)
	atomic.StoreInt64(&p.lastSent, unixTime)
}

func (p *peer) storeLastReceived(time time.Time) {
	unixTime := time.Unix()
	atomic.StoreInt64(&p.Config.LastReceived, unixTime)
	atomic.StoreInt64(&p.lastReceived, unixTime)
}
