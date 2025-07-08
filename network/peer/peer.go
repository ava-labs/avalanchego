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
	"net/netip"
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
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
)

const (
	// maxBloomSaltLen restricts the allowed size of the bloom salt to prevent
	// excessively expensive bloom filter contains checks.
	maxBloomSaltLen = 32
	// maxNumTrackedSubnets limits how many subnets a peer can track to prevent
	// excessive memory usage.
	maxNumTrackedSubnets = 16

	disconnectingLog         = "disconnecting from peer"
	failedToCreateMessageLog = "failed to create message"
	failedToSetDeadlineLog   = "failed to set connection deadline"
	failedToGetUptimeLog     = "failed to get peer uptime percentage"
	malformedMessageLog      = "malformed message"
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

	// ObservedUptime returns the local node's primary network uptime according to the
	// peer. The value ranges from [0, 100]. It should only be called after
	// [Ready] returns true.
	ObservedUptime() uint32

	// Send attempts to send [msg] to the peer. The peer takes ownership of
	// [msg] for reference counting. This returns false if the message is
	// guaranteed not to be delivered to the peer.
	Send(ctx context.Context, msg message.OutboundMessage) bool

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
	// trackedSubnets are the subnetIDs the peer sent us in the Handshake
	// message. The primary network ID is always included.
	trackedSubnets set.Set[ids.ID]
	// options of ACPs provided in the Handshake message.
	supportedACPs set.Set[uint32]
	objectedACPs  set.Set[uint32]

	// txIDOfVerifiedBLSKey is the txID that added the BLS key that was most
	// recently verified to have signed the IP.
	//
	// Invariant: Prior to the handshake being completed, this can only be
	// accessed by the reader goroutine. After the handshake has been completed,
	// this can only be accessed by the message sender goroutine.
	txIDOfVerifiedBLSKey ids.ID

	// Our primary network uptime perceived by the peer
	observedUptime utils.Atomic[uint32]

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

	// getPeerListChan signals that we should attempt to send a GetPeerList to
	// this peer
	getPeerListChan chan struct{}

	// isIngress is true only if the remote peer is connected to this node,
	// in contrast of this node being connected to the remote peer.
	isIngress bool

	// isAppRequestClient is true only if the remote peer is an AppRequest only client.
	isAppRequestClient bool
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
	isIngress bool,
) Peer {
	onClosingCtx, onClosingCtxCancel := context.WithCancel(context.Background())
	p := &peer{
		isIngress:          isIngress,
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
		getPeerListChan:    make(chan struct{}, 1),
	}

	if isIngress {
		p.IngressConnectionCount.Add(1)
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
	primaryUptime := p.ObservedUptime()

	ip, _ := ips.ParseAddrPort(p.conn.RemoteAddr().String())
	return Info{
		IP:             ip,
		PublicIP:       p.ip.AddrPort,
		ID:             p.id,
		Version:        p.version.String(),
		LastSent:       p.LastSent(),
		LastReceived:   p.LastReceived(),
		ObservedUptime: json.Uint32(primaryUptime),
		TrackedSubnets: p.trackedSubnets,
		SupportedACPs:  p.supportedACPs,
		ObjectedACPs:   p.objectedACPs,
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

func (p *peer) ObservedUptime() uint32 {
	return p.observedUptime.Get()
}

func (p *peer) Send(ctx context.Context, msg message.OutboundMessage) bool {
	return p.messageQueue.Push(ctx, msg)
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

	if p.isIngress {
		p.IngressConnectionCount.Add(-1)
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
			p.Log.Verbo(failedToSetDeadlineLog,
				zap.Stringer("nodeID", p.id),
				zap.String("direction", "read"),
				zap.Error(err),
			)
			return
		}

		// Read the message length
		if _, err := io.ReadFull(reader, msgLenBytes); err != nil {
			p.Log.Verbo("error reading message length",
				zap.Stringer("nodeID", p.id),
				zap.Error(err),
			)
			return
		}

		// Parse the message length
		msgLen, err := readMsgLen(msgLenBytes, constants.DefaultMaxMessageSize)
		if err != nil {
			p.Log.Verbo("error parsing message length",
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
			p.Log.Verbo(failedToSetDeadlineLog,
				zap.Stringer("nodeID", p.id),
				zap.String("direction", "read"),
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

			p.Metrics.NumFailedToParse.Inc()

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
	if port := mySignedIP.AddrPort.Port(); port == 0 {
		p.Log.Error("signed IP has invalid port",
			zap.Stringer("nodeID", p.id),
			zap.Uint16("port", port),
		)
		return
	}

	myVersion := p.VersionCompatibility.Version()
	knownPeersFilter, knownPeersSalt := p.Network.KnownPeers()

	_, areWeAPrimaryNetworkValidator := p.Validators.GetValidator(constants.PrimaryNetworkID, p.MyNodeID)

	var requestedSubnets []ids.ID
	if p.Config.AppRequestOnlyClient {
		requestedSubnets = p.Config.Network.Subnets().List()
	}

	msg, err := p.MessageCreator.Handshake(
		p.NetworkID,
		p.Clock.Unix(),
		mySignedIP.AddrPort,
		myVersion.Name,
		uint32(myVersion.Major),
		uint32(myVersion.Minor),
		uint32(myVersion.Patch),
		mySignedIP.Timestamp,
		mySignedIP.TLSSignature,
		mySignedIP.BLSSignatureBytes,
		p.MySubnets.List(),
		p.SupportedACPs,
		p.ObjectedACPs,
		knownPeersFilter,
		knownPeersSalt,
		areWeAPrimaryNetworkValidator,
		p.Config.AppRequestOnlyClient,
		requestedSubnets,
	)
	if err != nil {
		p.Log.Error(failedToCreateMessageLog,
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.HandshakeOp),
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
		zap.Stringer("op", msg.Op()),
		zap.Stringer("nodeID", p.id),
		zap.Binary("messageBytes", msgBytes),
	)

	if err := p.conn.SetWriteDeadline(p.nextTimeout()); err != nil {
		p.Log.Verbo(failedToSetDeadlineLog,
			zap.Stringer("nodeID", p.id),
			zap.String("direction", "write"),
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
		case <-p.getPeerListChan:
			knownPeersFilter, knownPeersSalt := p.Config.Network.KnownPeers()
			_, areWeAPrimaryNetworkValidator := p.Validators.GetValidator(constants.PrimaryNetworkID, p.MyNodeID)
			msg, err := p.Config.MessageCreator.GetPeerList(
				knownPeersFilter,
				knownPeersSalt,
				areWeAPrimaryNetworkValidator,
				p.Config.Network.Subnets().List(),
			)
			if err != nil {
				p.Log.Error(failedToCreateMessageLog,
					zap.Stringer("nodeID", p.id),
					zap.Stringer("messageOp", message.GetPeerListOp),
					zap.Error(err),
				)
				return
			}

			p.Send(p.onClosingCtx, msg)
		case <-sendPingsTicker.C:
			if !p.Network.AllowConnection(p.id) {
				p.Log.Debug(disconnectingLog,
					zap.String("reason", "connection is no longer desired"),
					zap.Stringer("nodeID", p.id),
				)
				return
			}

			// Only check if we should disconnect after the handshake is
			// finished to avoid race conditions and accessing uninitialized
			// values.
			if p.finishedHandshake.Get() && p.shouldDisconnect() {
				return
			}

			primaryUptime := p.getUptime()
			pingMessage, err := p.MessageCreator.Ping(primaryUptime)
			if err != nil {
				p.Log.Error(failedToCreateMessageLog,
					zap.Stringer("nodeID", p.id),
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

// shouldDisconnect is called both during receipt of the Handshake message and
// periodically when sending a Ping message (after finishing the handshake!).
//
// It is called during the Handshake to prevent marking a peer as connected and
// then immediately disconnecting from them.
//
// It is called when sending a Ping message to account for validator set
// changes. It's called when sending a Ping rather than in a validator set
// callback to avoid signature verification on the P-chain accept path.
func (p *peer) shouldDisconnect() bool {
	if err := p.VersionCompatibility.Compatible(p.version); err != nil {
		p.Log.Debug(disconnectingLog,
			zap.String("reason", "version not compatible"),
			zap.Stringer("nodeID", p.id),
			zap.Stringer("peerVersion", p.version),
			zap.Error(err),
		)
		return true
	}

	// Enforce that all validators that have registered a BLS key are signing
	// their IP with it after the activation of Durango.
	vdr, ok := p.Validators.GetValidator(constants.PrimaryNetworkID, p.id)
	if !ok || vdr.PublicKey == nil || vdr.TxID == p.txIDOfVerifiedBLSKey {
		return false
	}

	validSignature := bls.VerifyProofOfPossession(
		vdr.PublicKey,
		p.ip.BLSSignature,
		p.ip.UnsignedIP.bytes(),
	)
	if !validSignature {
		p.Log.Debug(disconnectingLog,
			zap.String("reason", "invalid BLS signature"),
			zap.Stringer("nodeID", p.id),
		)
		return true
	}

	// Avoid unnecessary signature verifications by only verifying the signature
	// once per validation period.
	p.txIDOfVerifiedBLSKey = vdr.TxID
	return false
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
		p.Log.Debug("dropping message",
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", msg.Op()),
			zap.String("reason", "handshake isn't finished"),
		)
		msg.OnFinishedHandling()
		return
	}

	// Consensus and app-level messages
	p.Router.HandleInbound(context.Background(), msg)
}

func (p *peer) handlePing(msg *p2p.Ping) {
	if msg.Uptime > 100 {
		p.Log.Debug(malformedMessageLog,
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.PingOp),
			zap.Stringer("subnetID", constants.PrimaryNetworkID),
			zap.Uint32("uptime", msg.Uptime),
		)
		p.StartClose()
		return
	}
	p.observedUptime.Set(msg.Uptime)

	pongMessage, err := p.MessageCreator.Pong()
	if err != nil {
		p.Log.Error(failedToCreateMessageLog,
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.PongOp),
			zap.Error(err),
		)
		p.StartClose()
		return
	}

	p.Send(p.onClosingCtx, pongMessage)
}

func (p *peer) getUptime() uint32 {
	primaryUptime, err := p.UptimeCalculator.CalculateUptimePercent(
		p.id,
	)
	if err != nil {
		p.Log.Debug(failedToGetUptimeLog,
			zap.Stringer("nodeID", p.id),
			zap.Stringer("subnetID", constants.PrimaryNetworkID),
			zap.Error(err),
		)
		primaryUptime = 0
	}

	primaryUptimePercent := uint32(primaryUptime * 100)
	return primaryUptimePercent
}

func (*peer) handlePong(*p2p.Pong) {}

func (p *peer) handleHandshake(msg *p2p.Handshake) {
	if p.gotHandshake.Get() {
		p.Log.Debug(malformedMessageLog,
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.HandshakeOp),
			zap.String("reason", "already received handshake"),
		)
		p.StartClose()
		return
	}

	if msg.NetworkId != p.NetworkID {
		p.Log.Debug(malformedMessageLog,
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.HandshakeOp),
			zap.String("field", "networkID"),
			zap.Uint32("peerNetworkID", msg.NetworkId),
			zap.Uint32("ourNetworkID", p.NetworkID),
		)
		p.StartClose()
		return
	}

	localTime := p.Clock.Time()
	localUnixTime := uint64(localTime.Unix())
	clockDifference := math.Abs(float64(msg.MyTime) - float64(localUnixTime))

	p.Metrics.ClockSkewCount.Inc()
	p.Metrics.ClockSkewSum.Add(clockDifference)

	if clockDifference > p.MaxClockDifference.Seconds() {
		log := p.Log.Debug
		if _, ok := p.Beacons.GetValidator(constants.PrimaryNetworkID, p.id); ok {
			log = p.Log.Warn
		}
		log(malformedMessageLog,
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.HandshakeOp),
			zap.String("field", "myTime"),
			zap.Uint64("peerTime", msg.MyTime),
			zap.Uint64("localTime", localUnixTime),
		)
		p.StartClose()
		return
	}

	p.version = &version.Application{
		Name:  msg.Client.GetName(),
		Major: int(msg.Client.GetMajor()),
		Minor: int(msg.Client.GetMinor()),
		Patch: int(msg.Client.GetPatch()),
	}

	if p.VersionCompatibility.Version().Before(p.version) {
		log := p.Log.Debug
		if _, ok := p.Beacons.GetValidator(constants.PrimaryNetworkID, p.id); ok {
			log = p.Log.Info
		}
		log("peer attempting to connect with newer version. You may want to update your client",
			zap.Stringer("nodeID", p.id),
			zap.Stringer("peerVersion", p.version),
		)
	}

	p.isAppRequestClient = msg.AppRequestClient

	// handle subnet IDs
	if numTrackedSubnets := len(msg.TrackedSubnets); numTrackedSubnets > maxNumTrackedSubnets {
		p.Log.Debug(malformedMessageLog,
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.HandshakeOp),
			zap.String("field", "trackedSubnets"),
			zap.Int("numTrackedSubnets", numTrackedSubnets),
		)
		p.StartClose()
		return
	}

	trackedSubnets, err := parseSubnetIDs(msg.TrackedSubnets)
	if err != nil {
		p.Log.Debug(malformedMessageLog,
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.HandshakeOp),
			zap.String("field", "trackedSubnets"),
			zap.Error(err),
		)
		p.StartClose()
		return
	}

	p.trackedSubnets = trackedSubnets
	p.trackedSubnets.Add(constants.PrimaryNetworkID)

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
		p.Log.Debug(malformedMessageLog,
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.HandshakeOp),
			zap.String("field", "acps"),
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
			p.Log.Debug(malformedMessageLog,
				zap.Stringer("nodeID", p.id),
				zap.Stringer("messageOp", message.HandshakeOp),
				zap.String("field", "knownPeers.filter"),
				zap.Error(err),
			)
			p.StartClose()
			return
		}

		salt = msg.KnownPeers.Salt
		if saltLen := len(salt); saltLen > maxBloomSaltLen {
			p.Log.Debug(malformedMessageLog,
				zap.Stringer("nodeID", p.id),
				zap.Stringer("messageOp", message.HandshakeOp),
				zap.String("field", "knownPeers.salt"),
				zap.Int("saltLen", saltLen),
			)
			p.StartClose()
			return
		}
	}

	addr, ok := ips.AddrFromSlice(msg.IpAddr)
	if !ok {
		p.Log.Debug(malformedMessageLog,
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.HandshakeOp),
			zap.String("field", "ip"),
			zap.Int("ipLen", len(msg.IpAddr)),
		)
		p.StartClose()
		return
	}

	port := uint16(msg.IpPort)
	if msg.IpPort == 0 {
		p.Log.Debug(malformedMessageLog,
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.HandshakeOp),
			zap.String("field", "port"),
			zap.Uint16("port", port),
		)
		p.StartClose()
		return
	}

	p.ip = &SignedIP{
		UnsignedIP: UnsignedIP{
			AddrPort: netip.AddrPortFrom(
				addr,
				port,
			),
			Timestamp: msg.IpSigningTime,
		},
		TLSSignature: msg.IpNodeIdSig,
	}
	maxTimestamp := localTime.Add(p.MaxClockDifference)
	if err := p.ip.Verify(p.cert, maxTimestamp); err != nil {
		log := p.Log.Debug
		if _, ok := p.Beacons.GetValidator(constants.PrimaryNetworkID, p.id); ok {
			log = p.Log.Warn
		}
		log(malformedMessageLog,
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.HandshakeOp),
			zap.String("field", "tlsSignature"),
			zap.Uint64("peerTime", msg.MyTime),
			zap.Uint64("localTime", localUnixTime),
			zap.Error(err),
		)

		p.StartClose()
		return
	}

	signature, err := bls.SignatureFromBytes(msg.IpBlsSig)
	if err != nil {
		p.Log.Debug(malformedMessageLog,
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.HandshakeOp),
			zap.String("field", "blsSignature"),
			zap.Error(err),
		)
		p.StartClose()
		return
	}

	p.ip.BLSSignature = signature
	p.ip.BLSSignatureBytes = msg.IpBlsSig

	p.isAppRequestClient = msg.AppRequestClient

	// If the peer is running an incompatible version or has an invalid BLS
	// signature, disconnect from them prior to marking the handshake as
	// completed.
	if p.shouldDisconnect() {
		p.StartClose()
		return
	}

	p.gotHandshake.Set(true)

	var requestedSubnets set.Set[ids.ID]
	if p.isAppRequestClient {
		requestedSubnets, err = parseSubnetIDs(msg.RequestedSubnets)
		if err != nil {
			p.Log.Debug(malformedMessageLog,
				zap.Stringer("nodeID", p.id),
				zap.Stringer("messageOp", message.HandshakeOp),
				zap.String("field", "requestableSubnets"),
				zap.Error(err),
			)
			p.StartClose()
			return
		}
	} else {
		requestedSubnets = p.trackedSubnets
	}

	peerIPs := p.Network.Peers(p.id, requestedSubnets, msg.AllSubnets, knownPeers, salt)

	// We bypass throttling here to ensure that the handshake message is
	// acknowledged correctly.
	peerListMsg, err := p.Config.MessageCreator.PeerList(peerIPs, true /*=bypassThrottling*/)
	if err != nil {
		p.Log.Error(failedToCreateMessageLog,
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.PeerListOp),
			zap.Error(err),
		)
		p.StartClose()
		return
	}

	if !p.Send(p.onClosingCtx, peerListMsg) {
		// Because throttling was marked to be bypassed with this message,
		// sending should only fail if the peer has started closing.
		p.Log.Debug("failed to send reliable message",
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.PeerListOp),
			zap.Error(p.onClosingCtx.Err()),
		)
		p.StartClose()
	}
}

func (p *peer) handleGetPeerList(msg *p2p.GetPeerList) {
	if !p.finishedHandshake.Get() {
		p.Log.Debug(malformedMessageLog,
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.GetPeerListOp),
			zap.String("reason", "not finished handshake"),
		)
		return
	}

	knownPeersMsg := msg.GetKnownPeers()
	filter, err := bloom.Parse(knownPeersMsg.GetFilter())
	if err != nil {
		p.Log.Debug(malformedMessageLog,
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.GetPeerListOp),
			zap.String("field", "knownPeers.filter"),
			zap.Error(err),
		)
		p.StartClose()
		return
	}

	salt := knownPeersMsg.GetSalt()
	if saltLen := len(salt); saltLen > maxBloomSaltLen {
		p.Log.Debug(malformedMessageLog,
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.GetPeerListOp),
			zap.String("field", "knownPeers.salt"),
			zap.Int("saltLen", saltLen),
		)
		p.StartClose()
		return
	}

	var requestedSubnets set.Set[ids.ID]
	if p.isAppRequestClient {
		requestedSubnets, err = parseSubnetIDs(msg.RequestedSubnets)
		if err != nil {
			p.Log.Debug(malformedMessageLog,
				zap.Stringer("nodeID", p.id),
				zap.Stringer("messageOp", message.GetPeerListOp),
				zap.String("field", "requestableSubnets"),
				zap.Error(err),
			)
			p.StartClose()
			return
		}
	} else {
		requestedSubnets = p.trackedSubnets
	}

	peerIPs := p.Network.Peers(p.id, requestedSubnets, msg.AllSubnets, filter, salt)

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
		p.Log.Error(failedToCreateMessageLog,
			zap.Stringer("nodeID", p.id),
			zap.Stringer("messageOp", message.PeerListOp),
			zap.Error(err),
		)
		return
	}

	p.Send(p.onClosingCtx, peerListMsg)
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

	discoveredIPs := make([]*ips.ClaimedIPPort, len(msg.ClaimedIpPorts)) // the peers this peer told us about
	for i, claimedIPPort := range msg.ClaimedIpPorts {
		tlsCert, err := staking.ParseCertificate(claimedIPPort.X509Certificate)
		if err != nil {
			p.Log.Debug(malformedMessageLog,
				zap.Stringer("nodeID", p.id),
				zap.Stringer("messageOp", message.PeerListOp),
				zap.String("field", "cert"),
				zap.Error(err),
			)
			p.StartClose()
			return
		}

		addr, ok := ips.AddrFromSlice(claimedIPPort.IpAddr)
		if !ok {
			p.Log.Debug(malformedMessageLog,
				zap.Stringer("nodeID", p.id),
				zap.Stringer("messageOp", message.PeerListOp),
				zap.String("field", "ip"),
				zap.Int("ipLen", len(claimedIPPort.IpAddr)),
			)
			p.StartClose()
			return
		}

		port := uint16(claimedIPPort.IpPort)
		if port == 0 {
			p.Log.Debug(malformedMessageLog,
				zap.Stringer("nodeID", p.id),
				zap.Stringer("messageOp", message.PeerListOp),
				zap.String("field", "port"),
				zap.Uint16("port", port),
			)
			p.StartClose()
			return
		}

		discoveredIPs[i] = ips.NewClaimedIPPort(
			tlsCert,
			netip.AddrPortFrom(
				addr,
				port,
			),
			claimedIPPort.Timestamp,
			claimedIPPort.Signature,
		)
	}

	if err := p.Network.Track(discoveredIPs); err != nil {
		p.Log.Debug(malformedMessageLog,
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

func parseSubnetIDs(subnetIDBytes [][]byte) (set.Set[ids.ID], error) {
	subnetIDs := set.NewSet[ids.ID](len(subnetIDBytes))
	for _, subnetIDBytes := range subnetIDBytes {
		subnetID, err := ids.ToID(subnetIDBytes)
		if err != nil {
			return nil, err
		}
		subnetIDs.Add(subnetID)
	}
	return subnetIDs, nil
}
