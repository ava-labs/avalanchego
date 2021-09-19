// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"bufio"
	"bytes"
	"crypto/x509"
	"encoding/binary"
	"io"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/message"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
)

// The signature of a peer's certificate on the byte representation
// of the peer's IP and time, and the time, in Unix seconds.
type signedPeerIP struct {
	ip        utils.IPDesc
	time      uint64
	signature []byte
}

// alias is a secondary IP address where a peer
// was reached
type alias struct {
	// ip where peer was reached
	ip utils.IPDesc

	// expiry is network time when the ip should be released
	expiry time.Time
}

type peer struct {
	net *network // network this peer is part of

	// True if this peer has sent us a valid Version message and
	// is running a compatible version.
	// Only modified on the connection's reader routine.
	gotVersion utils.AtomicBool

	// True if this peer has sent us a valid PeerList message.
	// Only modified on the connection's reader routine.
	gotPeerList utils.AtomicBool

	// only send the version to this peer on handling a getVersion message if
	// a version hasn't already been sent.
	versionSent            utils.AtomicBool
	versionWithSubnetsSent utils.AtomicBool

	// only send the peerlist to this peer on handling a getPeerlist message if
	// a peerlist hasn't already been sent.
	peerListSent utils.AtomicBool

	// True if the peer:
	// * Has sent us a Version message
	// * Has sent us a PeerList message
	// * Is a compatible version
	// Only modified on the connection's reader routine.
	finishedHandshake utils.AtomicBool

	// only close the peer once
	once sync.Once

	// if the close function has been called.
	closed utils.AtomicBool

	// queue of messages to be sent to this peer
	sendQueue [][]byte

	// Signalled when a message is added to [sendQueue],
	// and when [p.closed] is set to true.
	// [sendQueueCond.L] must be held when using [sendQueue].
	sendQueueCond *sync.Cond

	// ip may or may not be set when the peer is first started. is only modified
	// on the connection's reader routine.
	ip utils.IPDesc

	// ipLock must be held when accessing [ip].
	ipLock sync.RWMutex

	// aliases is a list of IPs other than [ip] that we have connected to
	// this peer at.
	aliases []alias

	// aliasTimer triggers the release of expired records from [aliases].
	aliasTimer *timer.Timer

	// aliasLock must be held when accessing [aliases] or [aliasTimer].
	aliasLock sync.Mutex

	// node ID of this peer.
	nodeID ids.ShortID

	// the connection object that is used to read/write messages from
	conn net.Conn

	// Version that this peer reported during the handshake.
	// Set when we process the Version message from this peer.
	versionStruct, versionStr utils.AtomicInterface

	// Unix time of the last message sent and received respectively
	// Must only be accessed atomically
	lastSent, lastReceived int64

	tickerCloser chan struct{}

	// ticker processes
	tickerOnce sync.Once

	// [cert] is this peer's certificate (specifically the leaf of the certificate chain they provided)
	cert *x509.Certificate

	// sigAndTime contains a struct of type sigAndTime.
	// The signature is [cert]'s signature on the peer's IP, concatenated with
	// the peer's local time when it sent a Version.
	// The time in [sigAndTime] is the one mentioned above.
	sigAndTime utils.AtomicInterface

	// Used in [handleAcceptedFrontier], [handleAccepted],
	// [handleGetAccepted], [handleChits].
	// We use this one ids.Set rather than allocating one per method call.
	// Should be cleared before use.
	// Should only be used in peer's reader goroutine.
	idSet ids.Set

	// trackedSubnets hold subnetIDs that this peer is interested in.
	trackedSubnets ids.Set
}

// newPeer returns a properly initialized *peer.
func newPeer(net *network, conn net.Conn, ip utils.IPDesc) *peer {
	p := &peer{
		sendQueueCond: sync.NewCond(&sync.Mutex{}),
		net:           net,
		conn:          conn,
		ip:            ip,
		tickerCloser:  make(chan struct{}),
	}
	p.aliasTimer = timer.NewTimer(p.releaseExpiredAliases)
	p.trackedSubnets.Add(constants.PrimaryNetworkID)
	return p
}

// assume the [stateLock] is held
func (p *peer) Start() {
	go func() {
		// Make sure that the version is the first message sent
		p.sendVersionWithSubnets()
		p.sendVersion()

		go p.ReadMessages()
		go p.WriteMessages()
	}()
}

func (p *peer) StartTicker() {
	go p.requestFinishHandshake()
	go p.sendPings()
	go p.monitorAliases()
}

func (p *peer) sendPings() {
	sendPingsTicker := time.NewTicker(p.net.pingFrequency)
	defer sendPingsTicker.Stop()

	for {
		select {
		case <-sendPingsTicker.C:
			closed := p.closed.GetValue()

			if closed {
				return
			}

			p.sendPing()
		case <-p.tickerCloser:
			return
		}
	}
}

// request missing handshake messages from the peer
func (p *peer) requestFinishHandshake() {
	finishHandshakeTicker := time.NewTicker(p.net.getVersionTimeout)
	defer finishHandshakeTicker.Stop()

	for {
		select {
		case <-finishHandshakeTicker.C:
			if p.finishedHandshake.GetValue() {
				return
			}
			if p.closed.GetValue() {
				return
			}
			if !p.gotVersion.GetValue() {
				p.sendGetVersion()
			}
			if !p.gotPeerList.GetValue() {
				p.sendGetPeerList()
			}
		case <-p.tickerCloser:
			return
		}
	}
}

// monitorAliases periodically attempts
// to release timed out alias IPs of the
// peer.
//
// monitorAliases will acquire [stateLock]
// when an alias is released.
func (p *peer) monitorAliases() {
	go func() {
		<-p.tickerCloser
		p.aliasTimer.Stop()
	}()

	p.aliasTimer.Dispatch()
}

// Read and handle messages from this peer.
// When this method returns, the connection is closed.
func (p *peer) ReadMessages() {
	defer p.Close()

	// Continuously read and handle messages from this peer.
	reader := bufio.NewReader(p.conn)
	msgLenBytes := make([]byte, wrappers.IntLen)
	for {
		// Time out and close connection if we can't read message length
		if err := p.conn.SetReadDeadline(p.nextTimeout()); err != nil {
			p.net.log.Verbo("error setting the connection read timeout on %s%s at %s %s", constants.NodeIDPrefix, p.nodeID, p.getIP(), err)
			return
		}

		// Read the message length
		if _, err := io.ReadFull(reader, msgLenBytes); err != nil {
			p.net.log.Verbo("error reading from %s%s at %s: %s", constants.NodeIDPrefix, p.nodeID, p.getIP(), err)
			return
		}

		// Parse the message length
		msgLen := binary.BigEndian.Uint32(msgLenBytes)

		// Make sure the message length is valid.
		if int64(msgLen) > p.net.maxMessageSize {
			p.net.log.Verbo("too large message length %d from %s%s at %s", msgLen, constants.NodeIDPrefix, p.nodeID, p.getIP())
			return
		}

		// Wait until the throttler says we can proceed to read the message.
		// Note that when we are done handling this message, or give up
		// trying to read it, we must call [p.net.msgThrottler.Release]
		// to give back the bytes used by this message.
		p.net.inboundMsgThrottler.Acquire(uint64(msgLen), p.nodeID)

		// Invariant: When done processing this message, onFinishedHandling() is called.
		// If this is not honored, the message throttler will leak until no new messages can be read.
		// You can look at message throttler metrics to verify that there is no leak.
		onFinishedHandling := func() { p.net.inboundMsgThrottler.Release(uint64(msgLen), p.nodeID) }

		// Time out and close connection if we can't read message
		if err := p.conn.SetReadDeadline(p.nextTimeout()); err != nil {
			p.net.log.Verbo("error setting the connection read timeout on %s%s at %s %s", constants.NodeIDPrefix, p.nodeID, p.getIP(), err)
			onFinishedHandling()
			return
		}

		// Read the message
		msgBytes := make([]byte, msgLen)
		if _, err := io.ReadFull(reader, msgBytes); err != nil {
			p.net.log.Verbo("error reading from %s%s at %s: %s", constants.NodeIDPrefix, p.nodeID, p.getIP(), err)
			onFinishedHandling()
			return
		}

		p.net.log.Verbo("parsing message from %s%s at %s:\n%s", constants.NodeIDPrefix, p.nodeID, p.getIP(), formatting.DumpBytes{Bytes: msgBytes})

		// Parse the message
		msg, err := p.net.c.Parse(msgBytes)
		if err != nil {
			p.net.log.Verbo("failed to parse message from %s%s at %s:\n%s\n%s", constants.NodeIDPrefix, p.nodeID, p.getIP(), formatting.DumpBytes{Bytes: msgBytes}, err)
			// Couldn't parse the message. Read the next one.
			onFinishedHandling()
			p.net.metrics.failedToParse.Inc()
			continue
		}

		// Handle the message. Note that when we are done handling
		// this message, we must call [p.net.msgThrottler.Release]
		// to release the bytes used by this message. See MsgThrottler.
		p.handle(msg, onFinishedHandling)
	}
}

// attempt to write messages to the peer
func (p *peer) WriteMessages() {
	defer p.Close()

	var reader bytes.Reader
	writer := bufio.NewWriter(p.conn)
	for { // When this loop exits, p.sendQueueCond.L is unlocked
		p.sendQueueCond.L.Lock()
		for {
			if p.closed.GetValue() {
				p.sendQueueCond.L.Unlock()
				return
			}
			if len(p.sendQueue) > 0 {
				// There is a message to send
				break
			}
			// Wait until there is a message to send
			p.sendQueueCond.Wait()
		}
		msg := p.sendQueue[0]
		p.sendQueue = p.sendQueue[1:]
		p.sendQueueCond.L.Unlock()

		msgLen := uint32(len(msg))
		p.net.outboundMsgThrottler.Release(uint64(msgLen), p.nodeID)
		p.net.log.Verbo("sending message to %s%s at %s:\n%s", constants.NodeIDPrefix, p.nodeID, p.getIP(), formatting.DumpBytes{Bytes: msg})
		msgb := [wrappers.IntLen]byte{}
		binary.BigEndian.PutUint32(msgb[:], msgLen)
		for _, byteSlice := range [2][]byte{msgb[:], msg} {
			reader.Reset(byteSlice)
			if err := p.conn.SetWriteDeadline(p.nextTimeout()); err != nil {
				p.net.log.Verbo("error setting write deadline to %s%s at %s due to: %s", constants.NodeIDPrefix, p.nodeID, p.getIP(), err)
				return
			}
			if _, err := io.CopyN(writer, &reader, int64(len((byteSlice)))); err != nil {
				p.net.log.Verbo("error writing to %s%s at %s due to: %s", constants.NodeIDPrefix, p.nodeID, p.getIP(), err)
				return
			}
			p.tickerOnce.Do(p.StartTicker)
		}
		// Make sure the peer got the entire message
		if err := writer.Flush(); err != nil {
			p.net.log.Verbo("couldn't flush writer to %s%s at %s: %s", constants.NodeIDPrefix, p.nodeID, p.getIP(), err)
			return
		}

		now := p.net.clock.Time().Unix()
		atomic.StoreInt64(&p.lastSent, now)
		atomic.StoreInt64(&p.net.lastMsgSentTime, now)

		p.net.byteSlicePool.Put(msg)
	}
}

// send assumes that the [stateLock] is not held.
// If [canModifyMsg], [msg] may be modified by this method.
// If ![canModifyMsg], [msg] will not be modified by this method.
// [canModifyMsg] should be false if [msg] is sent in a loop, for example/.
func (p *peer) Send(msg message.Message, canModifyMsg bool) bool {
	msgBytes := msg.Bytes()
	msgLen := int64(len(msgBytes))

	// Acquire space on the outbound message queue, or drop [msg] if we can't
	dropMsg := !p.net.outboundMsgThrottler.Acquire(uint64(msgLen), p.nodeID)
	if dropMsg {
		p.net.log.Debug("dropping %s message to %s%s at %s due to rate-limiting", msg.Op(), constants.NodeIDPrefix, p.nodeID, p.getIP())
		return false
	}
	// Invariant: must call p.net.outboundMsgThrottler.Release(uint64(msgLen), p.nodeID)
	// when done sending [msg] or when we give up sending [msg]

	p.sendQueueCond.L.Lock()
	defer p.sendQueueCond.L.Unlock()

	if p.closed.GetValue() {
		p.net.log.Debug("dropping message to %s%s at %s due to a closed connection", constants.NodeIDPrefix, p.nodeID, p.getIP())
		p.net.outboundMsgThrottler.Release(uint64(msgLen), p.nodeID)
		return false
	}

	// If the flag says to not modify [msgBytes], copy it so that the copy,
	// not [msgBytes], will be put back into the []byte pool after it's written.
	toSend := msgBytes
	if !canModifyMsg {
		toSend = make([]byte, msgLen)
		copy(toSend, msgBytes)
	}

	p.sendQueue = append(p.sendQueue, toSend)
	p.sendQueueCond.Signal()
	return true
}

// assumes the [stateLock] is not held
func (p *peer) handle(msg message.Message, onFinishedHandling func()) {
	now := p.net.clock.Time()
	atomic.StoreInt64(&p.lastReceived, now.Unix())
	atomic.StoreInt64(&p.net.lastMsgReceivedTime, now.Unix())
	msgLen := uint64(len(msg.Bytes()))

	op := msg.Op()
	msgMetrics := p.net.message(op)
	if msgMetrics == nil {
		p.net.log.Error("dropping an unknown message from %s%s at %s with op %s", constants.NodeIDPrefix, p.nodeID, p.getIP(), op)
		onFinishedHandling()
		return
	}
	msgMetrics.numReceived.Inc()
	msgMetrics.receivedBytes.Add(float64(msgLen))
	// assume that if [saved] == 0, [msg] wasn't compressed
	if saved := msg.BytesSavedCompression(); saved != 0 {
		msgMetrics.savedReceivedBytes.Observe(float64(saved))
	}

	switch op { // Network-related message types
	case message.Version:
		p.handleVersion(msg)
		onFinishedHandling()
		return
	case message.VersionWithSubnets:
		p.handleVersionWithSubnets(msg)
		onFinishedHandling()
		return
	case message.GetVersion:
		p.handleGetVersion(msg)
		onFinishedHandling()
		return
	case message.Ping:
		p.handlePing(msg)
		onFinishedHandling()
		return
	case message.Pong:
		p.handlePong(msg)
		onFinishedHandling()
		return
	case message.GetPeerList:
		p.handleGetPeerList(msg)
		onFinishedHandling()
		return
	case message.PeerList:
		p.handlePeerList(msg)
		onFinishedHandling()
		return
	}
	if !p.finishedHandshake.GetValue() {
		p.net.log.Debug("dropping %s from %s%s at %s because handshake isn't finished", op, constants.NodeIDPrefix, p.nodeID, p.getIP())

		// attempt to finish the handshake
		if !p.gotVersion.GetValue() {
			p.sendGetVersion()
		}
		if !p.gotPeerList.GetValue() {
			p.sendGetPeerList()
		}
		onFinishedHandling()
		return
	}

	switch op { // Consensus and app-level messages
	case message.GetAcceptedFrontier:
		p.handleGetAcceptedFrontier(msg, onFinishedHandling)
	case message.AcceptedFrontier:
		p.handleAcceptedFrontier(msg, onFinishedHandling)
	case message.GetAccepted:
		p.handleGetAccepted(msg, onFinishedHandling)
	case message.Accepted:
		p.handleAccepted(msg, onFinishedHandling)
	case message.Get:
		p.handleGet(msg, onFinishedHandling)
	case message.GetAncestors:
		p.handleGetAncestors(msg, onFinishedHandling)
	case message.Put:
		p.handlePut(msg, onFinishedHandling)
	case message.MultiPut:
		p.handleMultiPut(msg, onFinishedHandling)
	case message.PushQuery:
		p.handlePushQuery(msg, onFinishedHandling)
	case message.PullQuery:
		p.handlePullQuery(msg, onFinishedHandling)
	case message.Chits:
		p.handleChits(msg, onFinishedHandling)
	case message.AppRequest:
		p.handleAppRequest(msg, onFinishedHandling)
	case message.AppResponse:
		p.handleAppResponse(msg, onFinishedHandling)
	case message.AppGossip:
		p.handleAppGossip(msg, onFinishedHandling)
	default:
		p.net.log.Debug("dropping an unknown message from %s%s at %s with op %s", constants.NodeIDPrefix, p.nodeID, p.getIP(), op)
		onFinishedHandling()
	}
}

// assumes the [stateLock] is not held
func (p *peer) Close() { p.once.Do(p.close) }

// assumes only [peer.Close] calls this.
// By the time this message returns, [p] has been removed from [p.net.peers]
func (p *peer) close() {
	// If the connection is closing, we can immediately cancel the ticker
	// goroutines.
	close(p.tickerCloser)

	p.closed.SetValue(true)

	if err := p.conn.Close(); err != nil {
		p.net.log.Debug("closing connection to %s%s at %s resulted in an error: %s", constants.NodeIDPrefix, p.nodeID, p.getIP(), err)
	}

	p.sendQueueCond.L.Lock()
	// Release the bytes of the unsent messages to the outbound message throttler
	for i := 0; i < len(p.sendQueue); i++ {
		p.net.outboundMsgThrottler.Release(uint64(len(p.sendQueue[i])), p.nodeID)
	}
	p.sendQueue = nil
	p.sendQueueCond.L.Unlock()
	// Per [p.sendQueueCond]'s spec, it is signalled when [p.closed] is set to true
	// so that we exit the WriteMessages goroutine.
	// Since [p.closed] is now true, nothing else will be put on [p.sendQueue]
	p.sendQueueCond.Signal()
	p.net.disconnected(p)
}

// assumes the [stateLock] is not held
func (p *peer) sendGetVersion() {
	msg, err := p.net.b.GetVersion()
	p.net.log.AssertNoError(err)
	lenMsg := len(msg.Bytes())
	sent := p.Send(msg, true)
	if sent {
		p.net.metrics.getVersion.numSent.Inc()
		p.net.metrics.getVersion.sentBytes.Add(float64(lenMsg))
		// assume that if [saved] == 0, [msg] wasn't compressed
		if saved := msg.BytesSavedCompression(); saved != 0 {
			p.net.metrics.getVersion.savedSentBytes.Observe(float64(saved))
		}
		p.net.sendFailRateCalculator.Observe(0, p.net.clock.Time())
	} else {
		p.net.metrics.getVersion.numFailed.Inc()
		p.net.sendFailRateCalculator.Observe(1, p.net.clock.Time())
	}
}

// assumes the [stateLock] is not held
func (p *peer) sendVersion() {
	p.net.stateLock.RLock()
	myIP := p.net.ip.IP()
	myVersionTime, myVersionSig, err := p.net.getVersion(myIP)
	if err != nil {
		p.net.stateLock.RUnlock()
		return
	}
	msg, err := p.net.b.Version(
		p.net.networkID,
		p.net.nodeID,
		p.net.clock.Unix(),
		myIP,
		p.net.versionCompatibility.Version().String(),
		myVersionTime,
		myVersionSig,
	)
	p.net.stateLock.RUnlock()
	p.net.log.AssertNoError(err)

	lenMsg := len(msg.Bytes())
	sent := p.Send(msg, true)
	if sent {
		p.net.metrics.version.numSent.Inc()
		p.net.metrics.version.sentBytes.Add(float64(lenMsg))
		// assume that if [saved] == 0, [msg] wasn't compressed
		if saved := msg.BytesSavedCompression(); saved != 0 {
			p.net.metrics.version.savedSentBytes.Observe(float64(saved))
		}
		p.net.sendFailRateCalculator.Observe(0, p.net.clock.Time())
		p.versionSent.SetValue(true)
	} else {
		p.net.metrics.version.numFailed.Inc()
		p.net.sendFailRateCalculator.Observe(1, p.net.clock.Time())
	}
}

// assumes the [stateLock] is not held
func (p *peer) sendVersionWithSubnets() {
	p.net.stateLock.RLock()
	myIP := p.net.ip.IP()
	myVersionTime, myVersionSig, err := p.net.getVersion(myIP)
	if err != nil {
		p.net.stateLock.RUnlock()
		return
	}
	whitelistedSubnets := p.net.whitelistedSubnets
	msg, err := p.net.b.VersionWithSubnets(
		p.net.networkID,
		p.net.nodeID,
		p.net.clock.Unix(),
		myIP,
		p.net.versionCompatibility.Version().String(),
		myVersionTime,
		myVersionSig,
		whitelistedSubnets.List(),
	)
	p.net.stateLock.RUnlock()
	p.net.log.AssertNoError(err)

	lenMsg := len(msg.Bytes())
	sent := p.Send(msg, true)
	if sent {
		p.net.metrics.versionWithSubnets.numSent.Inc()
		p.net.metrics.versionWithSubnets.sentBytes.Add(float64(lenMsg))
		// assume that if [saved] == 0, [msg] wasn't compressed
		if saved := msg.BytesSavedCompression(); saved != 0 {
			p.net.metrics.versionWithSubnets.savedSentBytes.Observe(float64(saved))
		}
		p.net.sendFailRateCalculator.Observe(0, p.net.clock.Time())
		p.versionWithSubnetsSent.SetValue(true)
	} else {
		p.net.metrics.versionWithSubnets.numFailed.Inc()
		p.net.sendFailRateCalculator.Observe(1, p.net.clock.Time())
	}
}

// assumes the [stateLock] is not held
func (p *peer) sendGetPeerList() {
	msg, err := p.net.b.GetPeerList()
	p.net.log.AssertNoError(err)

	lenMsg := len(msg.Bytes())
	sent := p.Send(msg, true)
	if sent {
		p.net.getPeerlist.numSent.Inc()
		p.net.getPeerlist.sentBytes.Add(float64(lenMsg))
		// assume that if [saved] == 0, [msg] wasn't compressed
		if saved := msg.BytesSavedCompression(); saved != 0 {
			p.net.metrics.getPeerlist.savedSentBytes.Observe(float64(saved))
		}
		p.net.sendFailRateCalculator.Observe(0, p.net.clock.Time())
	} else {
		p.net.getPeerlist.numFailed.Inc()
		p.net.sendFailRateCalculator.Observe(1, p.net.clock.Time())
	}
}

// assumes the stateLock is not held
func (p *peer) sendPeerList() {
	peers, err := p.net.validatorIPs()
	if err != nil {
		return
	}

	// Compress this message only if the peer can handle compressed
	// messages and we have compression enabled
	msg, err := p.net.b.PeerList(peers, p.net.compressionEnabled)
	if err != nil {
		p.net.log.Warn("failed to send PeerList to %s%s at %s: %s", constants.NodeIDPrefix, p.nodeID, p.getIP(), err)
		return
	}

	lenMsg := len(msg.Bytes())
	sent := p.Send(msg, true)
	if sent {
		p.net.peerList.numSent.Inc()
		p.net.peerList.sentBytes.Add(float64(lenMsg))
		// assume that if [saved] == 0, [msg] wasn't compressed
		if saved := msg.BytesSavedCompression(); saved != 0 {
			p.net.metrics.peerList.savedSentBytes.Observe(float64(saved))
		}
		p.net.sendFailRateCalculator.Observe(0, p.net.clock.Time())
		p.peerListSent.SetValue(true)
	} else {
		p.net.peerList.numFailed.Inc()
		p.net.sendFailRateCalculator.Observe(1, p.net.clock.Time())
	}
}

// assumes the [stateLock] is not held
func (p *peer) sendPing() {
	msg, err := p.net.b.Ping()
	p.net.log.AssertNoError(err)
	lenMsg := len(msg.Bytes())
	sent := p.Send(msg, true)
	if sent {
		p.net.ping.numSent.Inc()
		p.net.ping.sentBytes.Add(float64(lenMsg))
		// assume that if [saved] == 0, [msg] wasn't compressed
		if saved := msg.BytesSavedCompression(); saved != 0 {
			p.net.metrics.ping.savedSentBytes.Observe(float64(saved))
		}
		p.net.sendFailRateCalculator.Observe(0, p.net.clock.Time())
	} else {
		p.net.ping.numFailed.Inc()
		p.net.sendFailRateCalculator.Observe(1, p.net.clock.Time())
	}
}

// assumes the [stateLock] is not held
func (p *peer) sendPong() {
	msg, err := p.net.b.Pong()
	p.net.log.AssertNoError(err)
	lenMsg := len(msg.Bytes())
	sent := p.Send(msg, true)
	if sent {
		p.net.pong.numSent.Inc()
		p.net.pong.sentBytes.Add(float64(lenMsg))
		// assume that if [saved] == 0, [msg] wasn't compressed
		if saved := msg.BytesSavedCompression(); saved != 0 {
			p.net.metrics.pong.savedSentBytes.Observe(float64(saved))
		}
		p.net.sendFailRateCalculator.Observe(0, p.net.clock.Time())
	} else {
		p.net.pong.numFailed.Inc()
		p.net.sendFailRateCalculator.Observe(1, p.net.clock.Time())
	}
}

// assumes the [stateLock] is not held
func (p *peer) handleGetVersion(_ message.Message) {
	if !p.versionWithSubnetsSent.GetValue() {
		p.sendVersionWithSubnets()
	}
	if !p.versionSent.GetValue() {
		p.sendVersion()
	}
}

// assumes the [stateLock] is not held
func (p *peer) handleVersion(msg message.Message) {
	p.versionCheck(msg, false)
}

// assumes the [stateLock] is not held
func (p *peer) handleVersionWithSubnets(msg message.Message) {
	p.versionCheck(msg, true)
}

// assumes the [stateLock] is not held
func (p *peer) versionCheck(msg message.Message, isVersionWithSubnets bool) {
	switch {
	case p.gotVersion.GetValue():
		p.net.log.Verbo("dropping duplicated version message from %s%s at %s", constants.NodeIDPrefix, p.nodeID, p.getIP())
		return
	case msg.Get(message.NodeID).(uint32) == p.net.nodeID:
		p.net.log.Debug("peer at %s has same node ID as me", p.getIP())
		p.discardMyIP()
		return
	case msg.Get(message.NetworkID).(uint32) != p.net.networkID:
		p.net.log.Debug(
			"network ID of %s%s at %s (%d) doesn't match our's (%d)",
			constants.NodeIDPrefix, p.nodeID, p.getIP(), msg.Get(message.NetworkID).(uint32), p.net.networkID,
		)
		p.discardIP()
		return
	case p.closed.GetValue():
		return
	}
	myTime := float64(p.net.clock.Unix())
	peerTime := float64(msg.Get(message.MyTime).(uint64))
	if math.Abs(peerTime-myTime) > p.net.maxClockDifference.Seconds() {
		if p.net.beacons.Contains(p.nodeID) {
			p.net.log.Warn(
				"beacon %s%s at %s reports time (%d) that is too far out of sync with our's (%d)",
				constants.NodeIDPrefix, p.nodeID, p.getIP(), uint64(peerTime), uint64(myTime),
			)
		} else {
			p.net.log.Debug(
				"peer %s%s at %s reports time (%d) that is too far out of sync with our's (%d)",
				constants.NodeIDPrefix, p.nodeID, p.getIP(), uint64(peerTime), uint64(myTime),
			)
		}
		p.discardIP()
		return
	}

	peerVersionStr := msg.Get(message.VersionStr).(string)
	peerVersion, err := p.net.parser.Parse(peerVersionStr)
	if err != nil {
		p.net.log.Debug("version of %s%s at %s could not be parsed: %s", constants.NodeIDPrefix, p.nodeID, p.getIP(), err)
		p.discardIP()
		p.net.metrics.failedToParse.Inc()
		return
	}

	if p.net.versionCompatibility.Version().Before(peerVersion) {
		if p.net.beacons.Contains(p.nodeID) {
			p.net.log.Info(
				"beacon %s%s at %s attempting to connect with newer version %s. You may want to update your client",
				constants.NodeIDPrefix, p.nodeID, p.getIP(), peerVersion,
			)
		} else {
			p.net.log.Debug(
				"peer %s%s at %s attempting to connect with newer version %s. You may want to update your client",
				constants.NodeIDPrefix, p.nodeID, p.getIP(), peerVersion,
			)
		}
	}

	if err := p.net.versionCompatibility.Compatible(peerVersion); err != nil {
		p.net.log.Verbo("peer %s%s at %s version (%s) not compatible: %s", constants.NodeIDPrefix, p.nodeID, p.getIP(), peerVersion, err)
		p.discardIP()
		return
	}

	peerIP := msg.Get(message.IP).(utils.IPDesc)

	versionTime := msg.Get(message.VersionTime).(uint64)
	p.net.stateLock.RLock()
	latestPeerIP := p.net.latestPeerIP[p.nodeID]
	p.net.stateLock.RUnlock()
	if latestPeerIP.time > versionTime {
		p.discardIP()
		return
	}
	if float64(versionTime)-myTime > p.net.maxClockDifference.Seconds() {
		p.net.log.Debug(
			"peer %s%s at %s attempting to connect with version timestamp (%d) too far in the future",
			constants.NodeIDPrefix, p.nodeID, p.getIP(), latestPeerIP.time,
		)
		p.discardIP()
		return
	}

	if isVersionWithSubnets {
		subnetIDsBytes := msg.Get(message.TrackedSubnets).([][]byte)
		for _, subnetIDBytes := range subnetIDsBytes {
			subnetID, err := ids.ToID(subnetIDBytes)
			if err != nil {
				p.net.log.Debug("tracked subnet of %s%s at %s could not be parsed: %s", constants.NodeIDPrefix, p.nodeID, err)
				p.discardIP()
				return
			}
			// add only if we also track this subnet
			if p.net.whitelistedSubnets.Contains(subnetID) {
				p.trackedSubnets.Add(subnetID)
			}
		}
	} else {
		// this peer has old Version, we don't know what its interested in.
		// so assume that it tracks all available subnets
		p.trackedSubnets.Add(p.net.whitelistedSubnets.List()...)
	}

	sig := msg.Get(message.SigBytes).([]byte)
	signed := ipAndTimeBytes(peerIP, versionTime)
	if err := p.cert.CheckSignature(p.cert.SignatureAlgorithm, signed, sig); err != nil {
		p.net.log.Debug("signature verification failed for %s%s at %s: %s", constants.NodeIDPrefix, p.nodeID, p.getIP(), err)
		p.discardIP()
		return
	}

	signedPeerIP := signedPeerIP{
		ip:        peerIP,
		time:      versionTime,
		signature: sig,
	}

	p.net.stateLock.Lock()
	p.net.latestPeerIP[p.nodeID] = signedPeerIP
	p.net.stateLock.Unlock()

	p.sigAndTime.SetValue(signedPeerIP)

	if ip := p.getIP(); ip.IsZero() {
		addr := p.conn.RemoteAddr()
		localPeerIP, err := utils.ToIPDesc(addr.String())
		if err == nil {
			// If we have no clue what the peer's IP is, we can't perform any
			// verification
			if peerIP.IP.Equal(localPeerIP.IP) {
				// if the IPs match, add this ip:port pair to be tracked
				p.setIP(peerIP)
			}
		}
	}

	p.sendPeerList()

	p.versionStruct.SetValue(peerVersion)
	p.versionStr.SetValue(peerVersion.String())
	p.gotVersion.SetValue(true)

	p.tryMarkFinishedHandshake()
}

// assumes the [stateLock] is not held
func (p *peer) handleGetPeerList(_ message.Message) {
	if p.gotVersion.GetValue() && !p.peerListSent.GetValue() {
		p.sendPeerList()
	}
}

func (p *peer) trackSignedPeer(peer utils.IPCertDesc) {
	p.net.stateLock.Lock()
	defer p.net.stateLock.Unlock()

	switch {
	case peer.IPDesc.Equal(p.net.ip.IP()):
		return
	case peer.IPDesc.IsZero():
		return
	case !p.net.allowPrivateIPs && peer.IPDesc.IsPrivate():
		return
	}

	if float64(peer.Time)-float64(p.net.clock.Unix()) > p.net.maxClockDifference.Seconds() {
		p.net.log.Debug("ignoring gossiped peer with version timestamp (%d) too far in the future", peer.Time)
		return
	}

	nodeID := certToID(peer.Cert)
	if !p.net.vdrs.Contains(nodeID) && !p.net.beacons.Contains(nodeID) {
		p.net.log.Verbo(
			"not peering to %s at %s because they are not a validator or beacon",
			nodeID.PrefixedString(constants.NodeIDPrefix), peer.IPDesc,
		)
		return
	}

	// Am I already peered to them? (safe because [p.net.stateLock] is held)

	if foundPeer, ok := p.net.peers.getByID(nodeID); ok && !foundPeer.closed.GetValue() {
		p.net.log.Verbo(
			"not peering to %s because we are already connected to %s",
			peer.IPDesc, nodeID.PrefixedString(constants.NodeIDPrefix),
		)
		return
	}

	if p.net.latestPeerIP[nodeID].time > peer.Time {
		p.net.log.Verbo(
			"not peering to %s at %s: the given timestamp (%d) < latest (%d)",
			nodeID.PrefixedString(constants.NodeIDPrefix), peer.IPDesc, peer.Time, p.net.latestPeerIP[nodeID].time,
		)
		return
	}

	signed := ipAndTimeBytes(peer.IPDesc, peer.Time)
	err := peer.Cert.CheckSignature(peer.Cert.SignatureAlgorithm, signed, peer.Signature)
	if err != nil {
		p.net.log.Debug(
			"signature verification failed for %s at %s: %s",
			nodeID.PrefixedString(constants.NodeIDPrefix), peer.IPDesc, err,
		)
		return
	}
	p.net.latestPeerIP[nodeID] = signedPeerIP{
		ip:   peer.IPDesc,
		time: peer.Time,
	}

	p.net.track(peer.IPDesc, nodeID)
}

// assumes the [stateLock] is not held
func (p *peer) handlePeerList(msg message.Message) {
	p.gotPeerList.SetValue(true)
	p.tryMarkFinishedHandshake()

	ips := msg.Get(message.SignedPeers).([]utils.IPCertDesc)
	for _, ip := range ips {
		p.trackSignedPeer(ip)
	}
}

// assumes the [stateLock] is not held
func (p *peer) handlePing(_ message.Message) {
	p.sendPong()
}

// assumes the [stateLock] is not held
func (p *peer) handlePong(_ message.Message) {
	if !p.finishedHandshake.GetValue() {
		// If the handshake isn't finished - do nothing
		return
	}

	peerVersion := p.versionStruct.GetValue().(version.Application)
	if err := p.net.versionCompatibility.Compatible(peerVersion); err != nil {
		p.net.log.Debug("disconnecting from peer %s%s at %s version (%s) not compatible: %s", constants.NodeIDPrefix, p.nodeID, p.getIP(), peerVersion, err)
		p.discardIP()
	}
}

// assumes the [stateLock] is not held
func (p *peer) handleGetAcceptedFrontier(msg message.Message, onFinishedHandling func()) {
	chainID, err := ids.ToID(msg.Get(message.ChainID).([]byte))
	p.net.log.AssertNoError(err)
	requestID := msg.Get(message.RequestID).(uint32)
	deadline := p.net.clock.Time().Add(time.Duration(msg.Get(message.Deadline).(uint64)))

	p.net.router.GetAcceptedFrontier(
		p.nodeID,
		chainID,
		requestID,
		deadline,
		onFinishedHandling,
	)
}

// assumes the [stateLock] is not held
func (p *peer) handleAcceptedFrontier(msg message.Message, onFinishedHandling func()) {
	chainID, err := ids.ToID(msg.Get(message.ChainID).([]byte))
	p.net.log.AssertNoError(err)
	requestID := msg.Get(message.RequestID).(uint32)

	containerIDsBytes := msg.Get(message.ContainerIDs).([][]byte)
	containerIDs := make([]ids.ID, len(containerIDsBytes))
	p.idSet.Clear()
	for i, containerIDBytes := range containerIDsBytes {
		containerID, err := ids.ToID(containerIDBytes)
		if err != nil {
			p.net.log.Debug(
				"error parsing ContainerID from %s%s at %s. ID: 0x%x. Error: %s",
				constants.NodeIDPrefix, p.nodeID, p.getIP(), containerIDBytes, err,
			)
			onFinishedHandling()
			p.net.metrics.failedToParse.Inc()
			return
		}
		if p.idSet.Contains(containerID) {
			p.net.log.Debug(
				"message from %s%s at %s contains duplicate of container ID %s",
				constants.NodeIDPrefix, p.nodeID, p.getIP(), containerID,
			)
			onFinishedHandling()
			p.net.metrics.failedToParse.Inc()
			return
		}
		containerIDs[i] = containerID
		p.idSet.Add(containerID)
	}

	p.net.router.AcceptedFrontier(
		p.nodeID,
		chainID,
		requestID,
		containerIDs,
		onFinishedHandling,
	)
}

// assumes the [stateLock] is not held
func (p *peer) handleGetAccepted(msg message.Message, onFinishedHandling func()) {
	chainID, err := ids.ToID(msg.Get(message.ChainID).([]byte))
	p.net.log.AssertNoError(err)
	requestID := msg.Get(message.RequestID).(uint32)
	deadline := p.net.clock.Time().Add(time.Duration(msg.Get(message.Deadline).(uint64)))

	containerIDsBytes := msg.Get(message.ContainerIDs).([][]byte)
	containerIDs := make([]ids.ID, len(containerIDsBytes))
	p.idSet.Clear()
	for i, containerIDBytes := range containerIDsBytes {
		containerID, err := ids.ToID(containerIDBytes)
		if err != nil {
			p.net.log.Debug(
				"error parsing ContainerID from %s%s at %s. ID: 0x%x. Error: %s",
				constants.NodeIDPrefix, p.nodeID, p.getIP(), containerIDBytes, err,
			)
			onFinishedHandling()
			p.net.metrics.failedToParse.Inc()
			return
		}
		if p.idSet.Contains(containerID) {
			p.net.log.Debug(
				"message from %s%s at %s contains duplicate of container ID %s",
				constants.NodeIDPrefix, p.nodeID, p.getIP(), containerID,
			)
			onFinishedHandling()
			p.net.metrics.failedToParse.Inc()
			return
		}
		containerIDs[i] = containerID
		p.idSet.Add(containerID)
	}

	p.net.router.GetAccepted(
		p.nodeID,
		chainID,
		requestID,
		deadline,
		containerIDs,
		onFinishedHandling,
	)
}

// assumes the [stateLock] is not held
func (p *peer) handleAccepted(msg message.Message, onFinishedHandling func()) {
	chainID, err := ids.ToID(msg.Get(message.ChainID).([]byte))
	p.net.log.AssertNoError(err)
	requestID := msg.Get(message.RequestID).(uint32)

	containerIDsBytes := msg.Get(message.ContainerIDs).([][]byte)
	containerIDs := make([]ids.ID, len(containerIDsBytes))
	p.idSet.Clear()
	for i, containerIDBytes := range containerIDsBytes {
		containerID, err := ids.ToID(containerIDBytes)
		if err != nil {
			p.net.log.Debug(
				"error parsing ContainerID from %s%s at %s. ID: 0x%x. Error: %s",
				constants.NodeIDPrefix, p.nodeID, p.getIP(), containerIDBytes, err,
			)
			onFinishedHandling()
			p.net.metrics.failedToParse.Inc()
			return
		}
		if p.idSet.Contains(containerID) {
			p.net.log.Debug(
				"message from %s%s at %s contains duplicate of container ID %s",
				constants.NodeIDPrefix, p.nodeID, p.getIP(), containerID,
			)
			onFinishedHandling()
			p.net.metrics.failedToParse.Inc()
			return
		}
		containerIDs[i] = containerID
		p.idSet.Add(containerID)
	}

	p.net.router.Accepted(
		p.nodeID,
		chainID,
		requestID,
		containerIDs,
		onFinishedHandling,
	)
}

// assumes the [stateLock] is not held
func (p *peer) handleGet(msg message.Message, onFinishedHandling func()) {
	chainID, err := ids.ToID(msg.Get(message.ChainID).([]byte))
	p.net.log.AssertNoError(err)
	requestID := msg.Get(message.RequestID).(uint32)
	deadline := p.net.clock.Time().Add(time.Duration(msg.Get(message.Deadline).(uint64)))
	containerID, err := ids.ToID(msg.Get(message.ContainerID).([]byte))
	p.net.log.AssertNoError(err)

	p.net.router.Get(
		p.nodeID,
		chainID,
		requestID,
		deadline,
		containerID,
		onFinishedHandling,
	)
}

func (p *peer) handleGetAncestors(msg message.Message, onFinishedHandling func()) {
	chainID, err := ids.ToID(msg.Get(message.ChainID).([]byte))
	p.net.log.AssertNoError(err)
	requestID := msg.Get(message.RequestID).(uint32)
	deadline := p.net.clock.Time().Add(time.Duration(msg.Get(message.Deadline).(uint64)))
	containerID, err := ids.ToID(msg.Get(message.ContainerID).([]byte))
	p.net.log.AssertNoError(err)

	p.net.router.GetAncestors(
		p.nodeID,
		chainID,
		requestID,
		deadline,
		containerID,
		onFinishedHandling,
	)
}

// assumes the [stateLock] is not held
func (p *peer) handlePut(msg message.Message, onFinishedHandling func()) {
	chainID, err := ids.ToID(msg.Get(message.ChainID).([]byte))
	p.net.log.AssertNoError(err)
	requestID := msg.Get(message.RequestID).(uint32)
	containerID, err := ids.ToID(msg.Get(message.ContainerID).([]byte))
	p.net.log.AssertNoError(err)
	container := msg.Get(message.ContainerBytes).([]byte)

	p.net.router.Put(
		p.nodeID,
		chainID,
		requestID,
		containerID,
		container,
		onFinishedHandling,
	)
}

// assumes the [stateLock] is not held
func (p *peer) handleMultiPut(msg message.Message, onFinishedHandling func()) {
	chainID, err := ids.ToID(msg.Get(message.ChainID).([]byte))
	p.net.log.AssertNoError(err)
	requestID := msg.Get(message.RequestID).(uint32)
	containers := msg.Get(message.MultiContainerBytes).([][]byte)

	p.net.router.MultiPut(
		p.nodeID,
		chainID,
		requestID,
		containers,
		onFinishedHandling,
	)
}

func (p *peer) handlePushQuery(msg message.Message, onFinishedHandling func()) {
	chainID, err := ids.ToID(msg.Get(message.ChainID).([]byte))
	p.net.log.AssertNoError(err)
	requestID := msg.Get(message.RequestID).(uint32)
	deadline := p.net.clock.Time().Add(time.Duration(msg.Get(message.Deadline).(uint64)))
	containerID, err := ids.ToID(msg.Get(message.ContainerID).([]byte))
	p.net.log.AssertNoError(err)
	container := msg.Get(message.ContainerBytes).([]byte)

	p.net.router.PushQuery(
		p.nodeID,
		chainID,
		requestID,
		deadline,
		containerID,
		container,
		onFinishedHandling,
	)
}

// assumes the [stateLock] is not held
func (p *peer) handlePullQuery(msg message.Message, onFinishedHandling func()) {
	chainID, err := ids.ToID(msg.Get(message.ChainID).([]byte))
	p.net.log.AssertNoError(err)
	requestID := msg.Get(message.RequestID).(uint32)
	deadline := p.net.clock.Time().Add(time.Duration(msg.Get(message.Deadline).(uint64)))
	containerID, err := ids.ToID(msg.Get(message.ContainerID).([]byte))
	p.net.log.AssertNoError(err)

	p.net.router.PullQuery(
		p.nodeID,
		chainID,
		requestID,
		deadline,
		containerID,
		onFinishedHandling,
	)
}

// assumes the [stateLock] is not held
func (p *peer) handleChits(msg message.Message, onFinishedHandling func()) {
	chainID, err := ids.ToID(msg.Get(message.ChainID).([]byte))
	p.net.log.AssertNoError(err)
	requestID := msg.Get(message.RequestID).(uint32)

	containerIDsBytes := msg.Get(message.ContainerIDs).([][]byte)
	containerIDs := make([]ids.ID, len(containerIDsBytes))
	p.idSet.Clear()
	for i, containerIDBytes := range containerIDsBytes {
		containerID, err := ids.ToID(containerIDBytes)
		if err != nil {
			p.net.log.Debug(
				"error parsing ContainerID from %s%s at %s 0x%x: %s",
				constants.NodeIDPrefix, p.nodeID, p.getIP(), containerIDBytes, err,
			)
			onFinishedHandling()
			p.net.metrics.failedToParse.Inc()
			return
		}
		if p.idSet.Contains(containerID) {
			p.net.log.Debug(
				"message from %s%s at %s contains duplicate of container ID %s",
				constants.NodeIDPrefix, p.nodeID, p.getIP(), containerID,
			)
			onFinishedHandling()
			p.net.metrics.failedToParse.Inc()
			return
		}
		containerIDs[i] = containerID
		p.idSet.Add(containerID)
	}

	p.net.router.Chits(
		p.nodeID,
		chainID,
		requestID,
		containerIDs,
		onFinishedHandling,
	)
}

// assumes the [stateLock] is not held
func (p *peer) handleAppRequest(msg message.Message, onFinishedHandling func()) {
	chainID, err := ids.ToID(msg.Get(message.ChainID).([]byte))
	p.net.log.AssertNoError(err)
	requestID := msg.Get(message.RequestID).(uint32)
	deadline := p.net.clock.Time().Add(time.Duration(msg.Get(message.Deadline).(uint64)))
	request := msg.Get(message.AppRequestBytes).([]byte)

	p.net.router.AppRequest(
		p.nodeID,
		chainID,
		requestID,
		deadline,
		request,
		onFinishedHandling,
	)
}

// assumes the [stateLock] is not held
func (p *peer) handleAppResponse(msg message.Message, onFinishedHandling func()) {
	chainID, err := ids.ToID(msg.Get(message.ChainID).([]byte))
	p.net.log.AssertNoError(err)
	requestID := msg.Get(message.RequestID).(uint32)
	response := msg.Get(message.AppResponseBytes).([]byte)

	p.net.router.AppResponse(
		p.nodeID,
		chainID,
		requestID,
		response,
		onFinishedHandling,
	)
}

// assumes the [stateLock] is not held
func (p *peer) handleAppGossip(msg message.Message, onFinishedHandling func()) {
	chainID, err := ids.ToID(msg.Get(message.ChainID).([]byte))
	p.net.log.AssertNoError(err)
	gossipedMsg := msg.Get(message.AppGossipBytes).([]byte)

	p.net.router.AppGossip(
		p.nodeID,
		chainID,
		gossipedMsg,
		onFinishedHandling,
	)
}

// assumes the [stateLock] is held
func (p *peer) tryMarkFinishedHandshake() {
	if !p.finishedHandshake.GetValue() && // not already marked as finished with handshake
		p.gotVersion.GetValue() && // not waiting for Version
		p.gotPeerList.GetValue() && // not waiting for PeerList
		!p.closed.GetValue() { // not already disconnected
		p.net.connected(p)
	}
}

func (p *peer) discardIP() {
	// By clearing the IP, we will not attempt to reconnect to this peer
	if ip := p.getIP(); !ip.IsZero() {
		p.setIP(utils.IPDesc{})

		p.net.stateLock.Lock()
		delete(p.net.disconnectedIPs, ip.String())
		p.net.stateLock.Unlock()
	}
	p.Close()
}

func (p *peer) discardMyIP() {
	// By clearing the IP, we will not attempt to reconnect to this peer
	if ip := p.getIP(); !ip.IsZero() {
		p.setIP(utils.IPDesc{})

		str := ip.String()

		p.net.stateLock.Lock()
		p.net.myIPs[str] = struct{}{}
		delete(p.net.disconnectedIPs, str)
		p.net.stateLock.Unlock()
	}
	p.Close()
}

func (p *peer) setIP(ip utils.IPDesc) {
	p.ipLock.Lock()
	defer p.ipLock.Unlock()
	p.ip = ip
}

func (p *peer) getIP() utils.IPDesc {
	p.ipLock.RLock()
	defer p.ipLock.RUnlock()
	return p.ip
}

// addAlias marks that we have found another
// IP that we can connect to this peer at.
//
// assumes [stateLock] is held
func (p *peer) addAlias(ip utils.IPDesc) {
	p.aliasLock.Lock()
	defer p.aliasLock.Unlock()

	p.net.peerAliasIPs[ip.String()] = struct{}{}
	p.aliases = append(p.aliases, alias{
		ip:     ip,
		expiry: p.net.clock.Time().Add(p.net.peerAliasTimeout),
	})

	// Set the [aliasTimer] if this ip is the first alias we put
	// in [aliases].
	if len(p.aliases) == 1 {
		p.aliasTimer.SetTimeoutIn(p.net.peerAliasTimeout)
	}
}

// releaseNextAlias returns the next released alias or nil if none was released.
// If none was released, then this will schedule the next time to remove an
// alias.
//
// assumes [stateLock] is held
func (p *peer) releaseNextAlias(now time.Time) *alias {
	p.aliasLock.Lock()
	defer p.aliasLock.Unlock()

	if len(p.aliases) == 0 {
		return nil
	}

	next := p.aliases[0]
	if timeUntilExpiry := next.expiry.Sub(now); timeUntilExpiry > 0 {
		p.aliasTimer.SetTimeoutIn(timeUntilExpiry)
		return nil
	}
	p.aliases = p.aliases[1:]

	p.net.log.Verbo("released alias %s for peer %s%s", next.ip, constants.NodeIDPrefix, p.nodeID)
	return &next
}

// releaseExpiredAliases frees expired IP aliases. If there is an IP pending
// expiration, then the expiration is scheduled.
//
// assumes [stateLock] is not held
func (p *peer) releaseExpiredAliases() {
	currentTime := p.net.clock.Time()
	for {
		next := p.releaseNextAlias(currentTime)
		if next == nil {
			return
		}

		// We should always release [aliasLock] before attempting
		// to acquire the [stateLock] to avoid deadlocking on addAlias.
		p.net.stateLock.Lock()
		delete(p.net.peerAliasIPs, next.ip.String())
		p.net.stateLock.Unlock()
	}
}

// releaseAllAliases frees all alias IPs.
//
// assumes [stateLock] is held and that [aliasTimer]
// has been stopped
func (p *peer) releaseAllAliases() {
	p.aliasLock.Lock()
	defer p.aliasLock.Unlock()

	for _, alias := range p.aliases {
		delete(p.net.peerAliasIPs, alias.ip.String())

		p.net.log.Verbo("released alias %s for peer %s%s", alias.ip, constants.NodeIDPrefix, p.nodeID)
	}
	p.aliases = nil
}

func (p *peer) nextTimeout() time.Time {
	return p.net.clock.Time().Add(p.net.pingPongTimeout)
}

func ipAndTimeBytes(ip utils.IPDesc, timestamp uint64) []byte {
	p := wrappers.Packer{
		Bytes: make([]byte, wrappers.IPLen+wrappers.LongLen),
	}
	p.PackIP(ip)
	p.PackLong(timestamp)
	return p.Bytes
}

func ipAndTimeHash(ip utils.IPDesc, timestamp uint64) []byte {
	return hashing.ComputeHash256(ipAndTimeBytes(ip, timestamp))
}
