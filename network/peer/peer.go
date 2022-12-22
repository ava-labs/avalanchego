// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"bufio"
	"context"
	"crypto/x509"
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
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"

	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
)

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
	Cert() *x509.Certificate

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
	cert *x509.Certificate

	// node ID of this peer.
	id ids.NodeID

	// queue of messages to send to this peer.
	messageQueue MessageQueue

	// ip is the claimed IP the peer gave us in the Version message.
	ip *SignedIP
	// version is the claimed version the peer is running that we received in
	// the Version message.
	version *version.Application
	// trackedSubnets is the subset of subnetIDs the peer sent us in the Version
	// message that we are also tracking.
	trackedSubnets set.Set[ids.ID]

	observedUptimesLock sync.RWMutex
	// [observedUptimesLock] must be held while accessing [observedUptime]
	// Subnet ID --> Our uptime for the given subnet as perceived by the peer
	observedUptimes map[ids.ID]uint32

	// True if this peer has sent us a valid Version message and
	// is running a compatible version.
	// Only modified on the connection's reader routine.
	gotVersion utils.AtomicBool

	// True if the peer:
	// * Has sent us a Version message
	// * Has sent us a PeerList message
	// * Is running a compatible version
	// Only modified on the connection's reader routine.
	finishedHandshake utils.AtomicBool

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
}

// Start a new peer instance.
//
// Invariant: There must only be one peer running at a time with a reference to
// the same [config.InboundMsgThrottler].
func Start(
	config *Config,
	conn net.Conn,
	cert *x509.Certificate,
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
	}

	go p.readMessages()
	go p.writeMessages()
	go p.sendNetworkMessages()

	return p
}

func (p *peer) ID() ids.NodeID {
	return p.id
}

func (p *peer) Cert() *x509.Certificate {
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
	return p.finishedHandshake.GetValue()
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
	if !p.ip.IP.IP.IsZero() {
		publicIPStr = p.ip.IP.IP.String()
	}

	trackedSubnets := p.trackedSubnets.List()
	uptimes := make(map[ids.ID]json.Uint32, len(trackedSubnets))

	for _, subnetID := range trackedSubnets {
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
		LastSent:              time.Unix(atomic.LoadInt64(&p.lastSent), 0),
		LastReceived:          time.Unix(atomic.LoadInt64(&p.lastReceived), 0),
		ObservedUptime:        json.Uint32(primaryUptime),
		ObservedSubnetUptimes: uptimes,
		TrackedSubnets:        trackedSubnets,
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

		now := p.Clock.Time().Unix()
		atomic.StoreInt64(&p.Config.LastReceived, now)
		atomic.StoreInt64(&p.lastReceived, now)
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

	// Make sure that the version is the first message sent
	mySignedIP, err := p.IPSigner.GetSignedIP()
	if err != nil {
		p.Log.Error("failed to get signed IP",
			zap.Error(err),
		)
		return
	}

	msg, err := p.MessageCreator.Version(
		p.NetworkID,
		p.Clock.Unix(),
		mySignedIP.IP.IP,
		p.VersionCompatibility.Version().String(),
		mySignedIP.IP.Timestamp,
		mySignedIP.Signature,
		p.MySubnets.List(),
	)
	if err != nil {
		p.Log.Error("failed to create message",
			zap.Stringer("messageOp", message.VersionOp),
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

	now := p.Clock.Time().Unix()
	atomic.StoreInt64(&p.Config.LastSent, now)
	atomic.StoreInt64(&p.lastSent, now)
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
			peerIPs, err := p.Config.Network.Peers(p.id)
			if err != nil {
				p.Log.Error("failed to get peers to gossip",
					zap.Stringer("nodeID", p.id),
					zap.Error(err),
				)
				return
			}

			if len(peerIPs) == 0 {
				p.Log.Debug(
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
		case <-sendPingsTicker.C:
			if !p.Network.AllowConnection(p.id) {
				p.Log.Debug("disconnecting from peer",
					zap.String("reason", "connection is no longer desired"),
					zap.Stringer("nodeID", p.id),
				)
				return
			}

			if p.finishedHandshake.GetValue() {
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

			pingMessage, err := p.Config.MessageCreator.Ping()
			if err != nil {
				p.Log.Error("failed to create message",
					zap.Stringer("messageOp", message.PingOp),
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
	case *p2ppb.Ping:
		p.handlePing(m)
		msg.OnFinishedHandling()
		return
	case *p2ppb.Pong:
		p.handlePong(m)
		msg.OnFinishedHandling()
		return
	case *p2ppb.Version:
		p.handleVersion(m)
		msg.OnFinishedHandling()
		return
	case *p2ppb.PeerList:
		p.handlePeerList(m)
		msg.OnFinishedHandling()
		return
	case *p2ppb.PeerListAck:
		p.handlePeerListAck(m)
		msg.OnFinishedHandling()
		return
	}
	if !p.finishedHandshake.GetValue() {
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

func (p *peer) handlePing(_ *p2ppb.Ping) {
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

	subnetUptimes := make([]*p2ppb.SubnetUptime, 0, p.trackedSubnets.Len())
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
		subnetUptimes = append(subnetUptimes, &p2ppb.SubnetUptime{
			SubnetId: subnetID[:],
			Uptime:   uint32(subnetUptime * 100),
		})
	}

	primaryUptimePercent := uint32(primaryUptime * 100)
	msg, err := p.MessageCreator.Pong(primaryUptimePercent, subnetUptimes)
	if err != nil {
		p.Log.Error("failed to create message",
			zap.Stringer("messageOp", message.PongOp),
			zap.Error(err),
		)
		return
	}
	p.Send(p.onClosingCtx, msg)
}

func (p *peer) handlePong(msg *p2ppb.Pong) {
	if msg.Uptime > 100 {
		p.Log.Debug("dropping pong message with invalid uptime",
			zap.Stringer("nodeID", p.id),
			zap.Uint32("uptime", msg.Uptime),
		)
		p.StartClose()
		return
	}
	p.observeUptime(constants.PrimaryNetworkID, msg.Uptime)

	for _, subnetUptime := range msg.SubnetUptimes {
		subnetID, err := ids.ToID(subnetUptime.SubnetId)
		if err != nil {
			p.Log.Debug("dropping pong message with invalid subnetID",
				zap.Stringer("nodeID", p.id),
				zap.Error(err),
			)
			p.StartClose()
			return
		}

		uptime := subnetUptime.Uptime
		if uptime > 100 {
			p.Log.Debug("dropping pong message with invalid uptime",
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
func (p *peer) observeUptime(subnetID ids.ID, uptime uint32) {
	p.observedUptimesLock.Lock()
	p.observedUptimes[subnetID] = uptime // [0, 100] percentage
	p.observedUptimesLock.Unlock()
}

func (p *peer) handleVersion(msg *p2ppb.Version) {
	if p.gotVersion.GetValue() {
		// TODO: this should never happen, should we close the connection here?
		p.Log.Verbo("dropping duplicated version message",
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

	myTime := p.Clock.Unix()
	if math.Abs(float64(msg.MyTime)-float64(myTime)) > p.MaxClockDifference.Seconds() {
		if p.Beacons.Contains(p.id) {
			p.Log.Warn("beacon reports out of sync time",
				zap.Stringer("nodeID", p.id),
				zap.Uint64("peerTime", msg.MyTime),
				zap.Uint64("myTime", myTime),
			)
		} else {
			p.Log.Debug("peer reports out of sync time",
				zap.Stringer("nodeID", p.id),
				zap.Uint64("peerTime", msg.MyTime),
				zap.Uint64("myTime", myTime),
			)
		}
		p.StartClose()
		return
	}

	peerVersion, err := version.ParseApplication(msg.MyVersion)
	if err != nil {
		p.Log.Debug("failed to parse peer version",
			zap.Stringer("nodeID", p.id),
			zap.Error(err),
		)
		p.StartClose()
		return
	}
	p.version = peerVersion

	if p.VersionCompatibility.Version().Before(peerVersion) {
		if p.Beacons.Contains(p.id) {
			p.Log.Info("beacon attempting to connect with newer version. You may want to update your client",
				zap.Stringer("nodeID", p.id),
				zap.Stringer("beaconVersion", peerVersion),
			)
		} else {
			p.Log.Debug("peer attempting to connect with newer version. You may want to update your client",
				zap.Stringer("nodeID", p.id),
				zap.Stringer("peerVersion", peerVersion),
			)
		}
	}

	if err := p.VersionCompatibility.Compatible(peerVersion); err != nil {
		p.Log.Verbo("peer version not compatible",
			zap.Stringer("nodeID", p.id),
			zap.Stringer("peerVersion", peerVersion),
			zap.Error(err),
		)
		p.StartClose()
		return
	}

	// Note that it is expected that the [versionTime] can be in the past. We
	// are just verifying that the claimed signing time isn't too far in the
	// future here.
	if float64(msg.MyVersionTime)-float64(myTime) > p.MaxClockDifference.Seconds() {
		p.Log.Debug("peer attempting to connect with version timestamp too far in the future",
			zap.Stringer("nodeID", p.id),
			zap.Uint64("versionTime", msg.MyVersionTime),
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

	// "net.IP" type in Golang is 16-byte
	if ipLen := len(msg.IpAddr); ipLen != net.IPv6len {
		p.Log.Debug("message with invalid field",
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.VersionOp),
			zap.String("field", "IP"),
			zap.Int("ipLen", ipLen),
		)
		p.StartClose()
		return
	}

	p.ip = &SignedIP{
		IP: UnsignedIP{
			IP: ips.IPPort{
				IP:   msg.IpAddr,
				Port: uint16(msg.IpPort),
			},
			Timestamp: msg.MyVersionTime,
		},
		Signature: msg.Sig,
	}
	if err := p.ip.Verify(p.cert); err != nil {
		p.Log.Debug("signature verification failed",
			zap.Stringer("nodeID", p.id),
			zap.Error(err),
		)
		p.StartClose()
		return
	}

	p.gotVersion.SetValue(true)

	peerIPs, err := p.Network.Peers(p.id)
	if err != nil {
		p.Log.Error("failed to get peers to gossip for handshake",
			zap.Stringer("nodeID", p.id),
			zap.Error(err),
		)
		return
	}

	// We bypass throttling here to ensure that the version message is
	// acknowledged timely.
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
		p.Log.Error("failed to send peer list for handshake",
			zap.Stringer("nodeID", p.id),
		)
	}
}

func (p *peer) handlePeerList(msg *p2ppb.PeerList) {
	if !p.finishedHandshake.GetValue() {
		if !p.gotVersion.GetValue() {
			return
		}

		p.Network.Connected(p.id)
		p.finishedHandshake.SetValue(true)
		close(p.onFinishHandshake)
	}

	// the peers this peer told us about
	discoveredIPs := make([]*ips.ClaimedIPPort, len(msg.ClaimedIpPorts))
	for i, claimedIPPort := range msg.ClaimedIpPorts {
		tlsCert, err := x509.ParseCertificate(claimedIPPort.X509Certificate)
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
				zap.Stringer("messageOp", message.VersionOp),
				zap.String("field", "IP"),
				zap.Int("ipLen", ipLen),
			)
			p.StartClose()
			return
		}

		// TODO: After the next network upgrade, require txIDs to be populated.
		var txID ids.ID
		if len(claimedIPPort.TxId) > 0 {
			txID, err = ids.ToID(claimedIPPort.TxId)
			if err != nil {
				p.Log.Debug("message with invalid field",
					zap.Stringer("nodeID", p.id),
					zap.Stringer("messageOp", message.PeerListOp),
					zap.String("field", "txID"),
					zap.Error(err),
				)
				p.StartClose()
				return
			}
		}

		discoveredIPs[i] = &ips.ClaimedIPPort{
			Cert: tlsCert,
			IPPort: ips.IPPort{
				IP:   claimedIPPort.IpAddr,
				Port: uint16(claimedIPPort.IpPort),
			},
			Timestamp: claimedIPPort.Timestamp,
			Signature: claimedIPPort.Signature,
			TxID:      txID,
		}
	}

	trackedPeers, err := p.Network.Track(p.id, discoveredIPs)
	if err != nil {
		p.Log.Debug("message with invalid field",
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.PeerListOp),
			zap.String("field", "claimedIP"),
			zap.Error(err),
		)
		p.StartClose()
		return
	}
	if len(trackedPeers) == 0 {
		p.Log.Debug("skipping peerlist ack as there were no tracked peers",
			zap.Stringer("nodeID", p.id),
		)
		return
	}

	peerListAckMsg, err := p.Config.MessageCreator.PeerListAck(trackedPeers)
	if err != nil {
		p.Log.Error("failed to create message",
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.PeerListAckOp),
			zap.Error(err),
		)
		return
	}

	if !p.Send(p.onClosingCtx, peerListAckMsg) {
		p.Log.Debug("failed to send peer list ack",
			zap.Stringer("nodeID", p.id),
		)
	}
}

func (p *peer) handlePeerListAck(msg *p2ppb.PeerListAck) {
	err := p.Network.MarkTracked(p.id, msg.PeerAcks)
	if err != nil {
		p.Log.Debug("message with invalid field",
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.PeerListAckOp),
			zap.String("field", "txID"),
			zap.Error(err),
		)
		p.StartClose()
	}
}

func (p *peer) nextTimeout() time.Time {
	return p.Clock.Time().Add(p.PongTimeout)
}
