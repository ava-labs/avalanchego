// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"bufio"
	"context"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
)

var (
	errClosed = errors.New("closed")

	_ Peer = &peer{}
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
	TrackedSubnets() ids.Set

	// ObservedUptime returns the local node's uptime according to the peer. The
	// value ranges from [0, 100]. It should only be called after [Ready]
	// returns true.
	ObservedUptime() uint8

	// Send attempts to send [msg] to the peer. The peer takes ownership of
	// [msg] for reference counting. This returns false if the message is
	// guaranteed not to be delivered to the peer.
	Send(ctx context.Context, msg message.OutboundMessage) bool

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
	trackedSubnets ids.Set

	observedUptimeLock sync.RWMutex
	// [observedUptimeLock] must be held while accessing [observedUptime]
	observedUptime uint8

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
	}

	go p.readMessages()
	go p.writeMessages()
	go p.sendPings()

	return p
}

func (p *peer) ID() ids.NodeID { return p.id }

func (p *peer) Cert() *x509.Certificate { return p.cert }

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

func (p *peer) Ready() bool { return p.finishedHandshake.GetValue() }

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
	return Info{
		IP:             p.conn.RemoteAddr().String(),
		PublicIP:       publicIPStr,
		ID:             p.id,
		Version:        p.version.String(),
		LastSent:       time.Unix(atomic.LoadInt64(&p.lastSent), 0),
		LastReceived:   time.Unix(atomic.LoadInt64(&p.lastReceived), 0),
		ObservedUptime: json.Uint8(p.ObservedUptime()),
		TrackedSubnets: p.trackedSubnets.List(),
	}
}

func (p *peer) IP() *SignedIP { return p.ip }

func (p *peer) Version() *version.Application { return p.version }

func (p *peer) TrackedSubnets() ids.Set { return p.trackedSubnets }

func (p *peer) ObservedUptime() uint8 {
	p.observedUptimeLock.RLock()
	uptime := p.observedUptime
	p.observedUptimeLock.RUnlock()
	return uptime
}

func (p *peer) Send(ctx context.Context, msg message.OutboundMessage) bool {
	return p.messageQueue.Push(ctx, msg)
}

func (p *peer) StartClose() {
	p.startClosingOnce.Do(func() {
		if err := p.conn.Close(); err != nil {
			p.Log.Debug(
				"closing connection to %s resulted in an error: %s",
				p.id, err,
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
			p.Log.Verbo(
				"error setting the connection read timeout on %s: %s",
				p.id, err,
			)
			return
		}

		// Read the message length
		if _, err := io.ReadFull(reader, msgLenBytes); err != nil {
			p.Log.Verbo(
				"error reading from %s: %s",
				p.id, err,
			)
			return
		}

		// Parse the message length
		msgLen := binary.BigEndian.Uint32(msgLenBytes)

		// Make sure the message length is valid.
		if msgLen > constants.DefaultMaxMessageSize {
			p.Log.Verbo("too large message length %d from %s", msgLen, p.id)
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
		if p.onClosingCtx.Err() != nil {
			onFinishedHandling()
			return
		}

		// Time out and close connection if we can't read message
		if err := p.conn.SetReadDeadline(p.nextTimeout()); err != nil {
			p.Log.Verbo(
				"error setting the connection read timeout on %s: %s",
				p.id, err,
			)
			onFinishedHandling()
			return
		}

		// Read the message
		msgBytes := make([]byte, msgLen)
		if _, err := io.ReadFull(reader, msgBytes); err != nil {
			p.Log.Verbo(
				"error reading from %s: %s",
				p.id, err,
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

		p.Log.Verbo(
			"parsing message from %s:\n%s",
			p.id, formatting.DumpBytes(msgBytes),
		)

		// Parse the message
		msg, err := p.MessageCreator.Parse(msgBytes, p.id, onFinishedHandling)
		if err != nil {
			p.Log.Verbo(
				"failed to parse message from %s: %s\n%s",
				p.id, err, formatting.DumpBytes(msgBytes),
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
	msg, err := p.Network.Version()
	p.Log.AssertNoError(err)

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
			p.Log.Verbo(
				"couldn't flush writer to %s: %s",
				p.id, err,
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
	p.Log.Verbo(
		"sending message to %s:\n%s",
		p.id, formatting.DumpBytes(msgBytes),
	)

	msgLen := uint32(len(msgBytes))
	msgLenBytes := [wrappers.IntLen]byte{}
	binary.BigEndian.PutUint32(msgLenBytes[:], msgLen)

	if err := p.conn.SetWriteDeadline(p.nextTimeout()); err != nil {
		p.Log.Verbo(
			"error setting write deadline to %s due to: %s",
			p.id, err,
		)
		msg.DecRef()
		return
	}

	// Write the message
	var buf net.Buffers = [][]byte{msgLenBytes[:], msgBytes}
	if _, err := io.CopyN(writer, &buf, int64(wrappers.IntLen+msgLen)); err != nil {
		p.Log.Verbo("error writing to %s: %s", p.id, err)
		msg.DecRef()
		return
	}

	now := p.Clock.Time().Unix()
	atomic.StoreInt64(&p.Config.LastSent, now)
	atomic.StoreInt64(&p.lastSent, now)
	p.Metrics.Sent(msg)
}

func (p *peer) sendPings() {
	sendPingsTicker := time.NewTicker(p.PingFrequency)
	defer func() {
		sendPingsTicker.Stop()

		p.StartClose()
		p.close()
	}()

	for {
		select {
		case <-sendPingsTicker.C:
			if !p.Network.AllowConnection(p.id) {
				p.Log.Debug(
					"disconnecting from peer %s because the peer's connection is no longer desired",
					p.id,
				)
				return
			}

			if p.finishedHandshake.GetValue() {
				if err := p.VersionCompatibility.Compatible(p.version); err != nil {
					p.Log.Debug(
						"disconnecting from peer %s version (%s) not compatible: %s",
						p.id, p.version, err,
					)
					return
				}
			}

			p.Config.PingMessage.AddRef()
			p.Send(p.onClosingCtx, p.Config.PingMessage)
		case <-p.onClosingCtx.Done():
			return
		}
	}
}

func (p *peer) handle(msg message.InboundMessage) {
	op := msg.Op()
	switch op { // Network-related message types
	case message.Ping:
		p.handlePing(msg)
		msg.OnFinishedHandling()
		return
	case message.Pong:
		p.handlePong(msg)
		msg.OnFinishedHandling()
		return
	case message.Version:
		p.handleVersion(msg)
		msg.OnFinishedHandling()
		return
	case message.PeerList:
		p.handlePeerList(msg)
		msg.OnFinishedHandling()
		return
	}
	if !p.finishedHandshake.GetValue() {
		p.Log.Debug(
			"dropping %s from %s because handshake isn't finished",
			op, p.id,
		)
		msg.OnFinishedHandling()
		return
	}

	// Consensus and app-level messages
	p.Router.HandleInbound(msg)
}

func (p *peer) handlePing(_ message.InboundMessage) {
	msg, err := p.Network.Pong(p.id)
	p.Log.AssertNoError(err)
	p.Send(p.onClosingCtx, msg)
}

func (p *peer) handlePong(msg message.InboundMessage) {
	uptime := msg.Get(message.Uptime).(uint8)
	if uptime > 100 {
		return
	}

	p.observedUptimeLock.Lock()
	p.observedUptime = uptime // [0, 100] percentage
	p.observedUptimeLock.Unlock()
}

func (p *peer) handleVersion(msg message.InboundMessage) {
	if p.gotVersion.GetValue() {
		p.Log.Verbo("dropping duplicated version message from %s", p.id)
		return
	}

	if peerNetworkID := msg.Get(message.NetworkID).(uint32); peerNetworkID != p.NetworkID {
		p.Log.Debug(
			"networkID of %s (%d) doesn't match our's (%d)",
			p.id, peerNetworkID, p.NetworkID,
		)
		p.StartClose()
		return
	}

	myTime := float64(p.Clock.Unix())
	peerTime := float64(msg.Get(message.MyTime).(uint64))
	if math.Abs(peerTime-myTime) > p.MaxClockDifference.Seconds() {
		if p.Beacons.Contains(p.id) {
			p.Log.Warn(
				"beacon %s reports time (%d) that is too far out of sync with our's (%d)",
				p.id, uint64(peerTime), uint64(myTime),
			)
		} else {
			p.Log.Debug(
				"peer %s reports time (%d) that is too far out of sync with our's (%d)",
				p.id, uint64(peerTime), uint64(myTime),
			)
		}
		p.StartClose()
		return
	}

	peerVersionStr := msg.Get(message.VersionStr).(string)
	peerVersion, err := version.ParseApplication(peerVersionStr)
	if err != nil {
		p.Log.Debug(
			"version of %s could not be parsed: %s",
			p.id, err,
		)
		p.StartClose()
		return
	}
	p.version = peerVersion

	if p.VersionCompatibility.Version().Before(peerVersion) {
		if p.Beacons.Contains(p.id) {
			p.Log.Info(
				"beacon %s attempting to connect with newer version %s. You may want to update your client",
				p.id, peerVersion,
			)
		} else {
			p.Log.Debug(
				"peer %s attempting to connect with newer version %s. You may want to update your client",
				p.id, peerVersion,
			)
		}
	}

	if err := p.VersionCompatibility.Compatible(peerVersion); err != nil {
		p.Log.Verbo("peer %s version (%s) not compatible: %s",
			p.id, peerVersion, err,
		)
		p.StartClose()
		return
	}

	// Note that it is expected that the [versionTime] can be in the past. We
	// are just verifying that the claimed signing time isn't too far in the
	// future here.
	versionTime := msg.Get(message.VersionTime).(uint64)
	if float64(versionTime)-myTime > p.MaxClockDifference.Seconds() {
		p.Log.Debug(
			"peer %s attempting to connect with version timestamp (%d) too far in the future",
			p.id, versionTime,
		)
		p.StartClose()
		return
	}

	peerIP := msg.Get(message.IP).(ips.IPPort)

	// handle subnet IDs
	subnetIDsBytes := msg.Get(message.TrackedSubnets).([][]byte)
	for _, subnetIDBytes := range subnetIDsBytes {
		subnetID, err := ids.ToID(subnetIDBytes)
		if err != nil {
			p.Log.Debug(
				"tracked subnet of %s could not be parsed: %s",
				p.id, err,
			)
			p.StartClose()
			return
		}
		// add only if we also track this subnet
		if p.MySubnets.Contains(subnetID) {
			p.trackedSubnets.Add(subnetID)
		}
	}

	p.ip = &SignedIP{
		IP: UnsignedIP{
			IP:        peerIP,
			Timestamp: versionTime,
		},
		Signature: msg.Get(message.SigBytes).([]byte),
	}
	if err := p.ip.Verify(p.cert); err != nil {
		p.Log.Debug("signature verification failed for %s: %s",
			p.id, err,
		)
		p.StartClose()
		return
	}

	p.gotVersion.SetValue(true)

	peerlistMsg, err := p.Network.Peers()
	p.Log.AssertNoError(err)
	p.Send(p.onClosingCtx, peerlistMsg)
}

func (p *peer) handlePeerList(msg message.InboundMessage) {
	if !p.finishedHandshake.GetValue() {
		if !p.gotVersion.GetValue() {
			return
		}

		p.Network.Connected(p.id)
		p.finishedHandshake.SetValue(true)
		close(p.onFinishHandshake)
	}

	ips := msg.Get(message.Peers).([]ips.ClaimedIPPort)
	for _, ip := range ips {
		if !p.Network.Track(ip) {
			p.Metrics.NumUselessPeerListBytes.Add(float64(ip.BytesLen()))
		}
	}
}

func (p *peer) nextTimeout() time.Time {
	return p.Clock.Time().Add(p.PongTimeout)
}
