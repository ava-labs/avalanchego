// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"bytes"
	"math"
	"net"
	"sync"
	"time"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/wrappers"
)

type peer struct {
	net *network

	connected bool
	closed    bool
	sender    chan []byte

	ip   utils.IPDesc
	id   ids.ShortID
	conn net.Conn

	once sync.Once

	b Builder
	c Codec
}

func (p *peer) Start() {
	go p.ReadMessages()
	go p.WriteMessages()

	// Initially send the version to the peer
	p.Version()
}

func (p *peer) requestVersion() {
	t := time.NewTicker(p.net.getVersionTimeout)
	defer t.Stop()

	for range t.C {
		p.net.stateLock.Lock()
		connected := p.connected
		closed := p.closed
		p.net.stateLock.Unlock()

		if connected || closed {
			return
		}
		p.GetVersion()
	}
}

func (p *peer) ReadMessages() {
	defer p.Close()

	pendingBuffer := wrappers.Packer{}
	readBuffer := make([]byte, 1<<10)
	for {
		read, err := p.conn.Read(readBuffer)
		if err != nil {
			p.net.log.Verbo("error on connection read to %s %s", p.id, err)
			return
		}

		pendingBuffer.Bytes = append(pendingBuffer.Bytes, readBuffer[:read]...)

		msgBytes := pendingBuffer.UnpackBytes()
		if pendingBuffer.Errored() {
			// if reading the bytes errored, then we haven't read the full
			// message yet
			pendingBuffer.Offset = 0
			pendingBuffer.Err = nil

			if uint32(len(pendingBuffer.Bytes)) > p.net.maxMessageSize+wrappers.IntLen {
				// we have read more bytes than the max message size allows for,
				// so we should terminate this connection

				p.net.log.Verbo("error reading too many bytes on %s %s", p.id, err)
				return
			}

			// we should try to read more bytes to finish the message
			continue
		}

		// we read the full message bytes

		// set the pending bytes to any extra bytes that were read
		pendingBuffer.Bytes = pendingBuffer.Bytes[pendingBuffer.Offset:]
		// set the offset back to the start of the next message
		pendingBuffer.Offset = 0

		if uint32(len(msgBytes)) > p.net.maxMessageSize {
			// if this message is longer than the max message length, then we
			// should terminate this connection

			p.net.log.Verbo("error reading too many bytes on %s %s", p.id, err)
			return
		}

		p.net.log.Verbo("parsing new message from %s:\n%s",
			p.id,
			formatting.DumpBytes{Bytes: msgBytes})

		msg, err := p.c.Parse(msgBytes)
		if err != nil {
			p.net.log.Debug("failed to parse new message from %s:\n%s\n%s",
				p.id,
				formatting.DumpBytes{Bytes: msgBytes},
				err)
			return
		}

		p.handle(msg)
	}
}

func (p *peer) WriteMessages() {
	defer p.Close()

	for msg := range p.sender {
		p.net.log.Verbo("sending new message to %s:\n%s",
			p.id,
			formatting.DumpBytes{Bytes: msg})

		packer := wrappers.Packer{Bytes: make([]byte, len(msg)+wrappers.IntLen)}
		packer.PackBytes(msg)
		msg = packer.Bytes
		for len(msg) > 0 {
			written, err := p.conn.Write(msg)
			if err != nil {
				// TODO: log error
				return
			}
			msg = msg[written:]
		}
	}
}

func (p *peer) Send(msg Msg) bool {
	select {
	case p.sender <- msg.Bytes():
		return true
	default:
		p.net.log.Debug("dropping message to %s due to a full send queue", p.id)
		return false
	}
}

func (p *peer) handle(msg Msg) {
	op := msg.Op()
	switch op {
	case Version:
		p.version(msg)
		return
	case GetVersion:
		p.getVersion(msg)
		return
	}
	if !p.connected {
		p.net.log.Debug("dropping message from %s because the connection hasn't been established yet", p.id)
		return
	}
	switch msg.Op() {
	case GetPeerList:
		p.getPeerList(msg)
	case PeerList:
		p.peerList(msg)
	case GetAcceptedFrontier:
		p.getAcceptedFrontier(msg)
	case AcceptedFrontier:
		p.acceptedFrontier(msg)
	case GetAccepted:
		p.getAccepted(msg)
	case Accepted:
		p.accepted(msg)
	case Get:
		p.get(msg)
	case Put:
		p.put(msg)
	case PushQuery:
		p.pushQuery(msg)
	case PullQuery:
		p.pullQuery(msg)
	case Chits:
		p.chits(msg)
	default:
		p.net.log.Debug("dropping an unknown message from %s with op %d", p.id, op)
	}
}

func (p *peer) Close() { p.once.Do(p.close) }

func (p *peer) close() {
	p.net.stateLock.Lock()
	defer p.net.stateLock.Unlock()

	p.closed = true
	p.conn.Close()
	close(p.sender)
	p.net.disconnected(p)
}

func (p *peer) GetVersion() {
	msg, err := p.b.GetVersion()
	p.net.log.AssertNoError(err)
	p.Send(msg)
}
func (p *peer) Version() {
	msg, err := p.b.Version(
		p.net.networkID,
		p.net.clock.Unix(),
		p.net.ip,
		p.net.version.String(),
	)
	p.net.log.AssertNoError(err)
	p.Send(msg)
}
func (p *peer) GetPeerList() {
	msg, err := p.b.GetPeerList()
	p.net.log.AssertNoError(err)
	p.Send(msg)
}
func (p *peer) PeerList(peers []utils.IPDesc) {
	msg, err := p.b.PeerList(peers)
	if err != nil {
		p.net.log.Warn("failed to send PeerList message due to %s", err)
		return
	}
	p.Send(msg)
	return
}

func (p *peer) getVersion(_ Msg) { p.Version() }
func (p *peer) version(msg Msg) {
	if p.connected {
		p.net.log.Verbo("dropping duplicated version message from %s", p.id)
		return
	}

	if networkID := msg.Get(NetworkID).(uint32); networkID != p.net.networkID {
		p.net.log.Debug("peer's network ID doesn't match our networkID: Peer's = %d ; Ours = %d",
			networkID,
			p.net.networkID)

		// By clearing the IP, we will not attempt to reconnect to this peer
		p.ip = utils.IPDesc{}
		p.Close()
		return
	}

	myTime := float64(p.net.clock.Unix())
	if peerTime := float64(msg.Get(MyTime).(uint64)); math.Abs(peerTime-myTime) > p.net.maxClockDifference.Seconds() {
		p.net.log.Debug("peer's clock is too far out of sync with mine. Peer's = %d, Ours = %d (seconds)",
			uint64(peerTime),
			uint64(myTime))

		// By clearing the IP, we will not attempt to reconnect to this peer
		p.ip = utils.IPDesc{}
		p.Close()
		return
	}

	peerVersionStr := msg.Get(VersionStr).(string)
	peerVersion, err := p.net.parser.Parse(peerVersionStr)
	if err != nil {
		p.net.log.Debug("peer version could not be parsed due to %s", err)

		// By clearing the IP, we will not attempt to reconnect to this peer
		p.ip = utils.IPDesc{}
		p.Close()
		return
	}

	if p.net.version.Before(peerVersion) {
		p.net.log.Info("peer attempting to connect with newer version %s. You may want to update your client",
			peerVersion)
	}

	if err := p.net.version.Compatible(peerVersion); err != nil {
		p.net.log.Debug("peer version not compatible due to %s", err)

		// By clearing the IP, we will not attempt to reconnect to this peer
		p.ip = utils.IPDesc{}
		p.Close()
		return
	}

	if p.ip.IsZero() {
		// we only care about the claimed IP if we don't know the IP yet
		peerIP := msg.Get(IP).(utils.IPDesc)

		addr := p.conn.RemoteAddr()
		localPeerIP, err := utils.ToIPDesc(addr.String())
		if err == nil {
			// If we have no clue what the peer's IP is, we can't perform any
			// verification
			if bytes.Equal(peerIP.IP, localPeerIP.IP) {
				// if the IPs match, add this ip:port pair to be tracked
				p.ip = peerIP
			}
		}
	}

	p.net.stateLock.Lock()
	defer p.net.stateLock.Unlock()

	if p.closed {
		return
	}

	p.connected = true
	p.net.connected(p)
}
func (p *peer) getPeerList(_ Msg) {
	ips := p.net.validatorIPs()
	reply, err := p.b.PeerList(ips)
	if err != nil {
		p.net.log.Warn("failed to send PeerList message due to %s", err)
		return
	}
	p.Send(reply)
}
func (p *peer) peerList(msg Msg) {
	ips := msg.Get(Peers).([]utils.IPDesc)

	for _, ip := range ips {
		if !ip.Equal(p.net.ip) &&
			!ip.IsZero() &&
			(p.net.allowPrivateIPs || !ip.IsPrivate()) {
			// TODO: this is a vulnerability, perhaps only try to connect once?
			p.net.Track(ip)
		}
	}
}
func (p *peer) getAcceptedFrontier(msg Msg) {
	chainID, err := ids.ToID(msg.Get(ChainID).([]byte))
	p.net.log.AssertNoError(err)
	requestID := msg.Get(RequestID).(uint32)

	p.net.router.GetAcceptedFrontier(p.id, chainID, requestID)
}
func (p *peer) acceptedFrontier(msg Msg) {
	chainID, err := ids.ToID(msg.Get(ChainID).([]byte))
	p.net.log.AssertNoError(err)
	requestID := msg.Get(RequestID).(uint32)

	containerIDs := ids.Set{}
	for _, containerIDBytes := range msg.Get(ContainerIDs).([][]byte) {
		containerID, err := ids.ToID(containerIDBytes)
		if err != nil {
			p.net.log.Debug("error parsing ContainerID 0x%x: %s", containerIDBytes, err)
			return
		}
		containerIDs.Add(containerID)
	}

	p.net.router.AcceptedFrontier(p.id, chainID, requestID, containerIDs)
}
func (p *peer) getAccepted(msg Msg) {
	chainID, err := ids.ToID(msg.Get(ChainID).([]byte))
	p.net.log.AssertNoError(err)
	requestID := msg.Get(RequestID).(uint32)

	containerIDs := ids.Set{}
	for _, containerIDBytes := range msg.Get(ContainerIDs).([][]byte) {
		containerID, err := ids.ToID(containerIDBytes)
		if err != nil {
			p.net.log.Debug("error parsing ContainerID 0x%x: %s", containerIDBytes, err)
			return
		}
		containerIDs.Add(containerID)
	}

	p.net.router.GetAccepted(p.id, chainID, requestID, containerIDs)
}
func (p *peer) accepted(msg Msg) {
	chainID, err := ids.ToID(msg.Get(ChainID).([]byte))
	p.net.log.AssertNoError(err)
	requestID := msg.Get(RequestID).(uint32)

	containerIDs := ids.Set{}
	for _, containerIDBytes := range msg.Get(ContainerIDs).([][]byte) {
		containerID, err := ids.ToID(containerIDBytes)
		if err != nil {
			p.net.log.Debug("error parsing ContainerID 0x%x: %s", containerIDBytes, err)
			return
		}
		containerIDs.Add(containerID)
	}

	p.net.router.Accepted(p.id, chainID, requestID, containerIDs)
}
func (p *peer) get(msg Msg) {
	chainID, err := ids.ToID(msg.Get(ChainID).([]byte))
	p.net.log.AssertNoError(err)
	requestID := msg.Get(RequestID).(uint32)
	containerID, err := ids.ToID(msg.Get(ContainerID).([]byte))
	p.net.log.AssertNoError(err)

	p.net.router.Get(p.id, chainID, requestID, containerID)
}
func (p *peer) put(msg Msg) {
	chainID, err := ids.ToID(msg.Get(ChainID).([]byte))
	p.net.log.AssertNoError(err)
	requestID := msg.Get(RequestID).(uint32)
	containerID, err := ids.ToID(msg.Get(ContainerID).([]byte))
	p.net.log.AssertNoError(err)
	container := msg.Get(ContainerBytes).([]byte)

	p.net.router.Put(p.id, chainID, requestID, containerID, container)
}
func (p *peer) pushQuery(msg Msg) {
	chainID, err := ids.ToID(msg.Get(ChainID).([]byte))
	p.net.log.AssertNoError(err)
	requestID := msg.Get(RequestID).(uint32)
	containerID, err := ids.ToID(msg.Get(ContainerID).([]byte))
	p.net.log.AssertNoError(err)
	container := msg.Get(ContainerBytes).([]byte)

	p.net.router.PushQuery(p.id, chainID, requestID, containerID, container)
}
func (p *peer) pullQuery(msg Msg) {
	chainID, err := ids.ToID(msg.Get(ChainID).([]byte))
	p.net.log.AssertNoError(err)
	requestID := msg.Get(RequestID).(uint32)
	containerID, err := ids.ToID(msg.Get(ContainerID).([]byte))
	p.net.log.AssertNoError(err)

	p.net.router.PullQuery(p.id, chainID, requestID, containerID)
}
func (p *peer) chits(msg Msg) {
	chainID, err := ids.ToID(msg.Get(ChainID).([]byte))
	p.net.log.AssertNoError(err)
	requestID := msg.Get(RequestID).(uint32)

	containerIDs := ids.Set{}
	for _, containerIDBytes := range msg.Get(ContainerIDs).([][]byte) {
		containerID, err := ids.ToID(containerIDBytes)
		if err != nil {
			p.net.log.Debug("error parsing ContainerID 0x%x: %s", containerIDBytes, err)
			return
		}
		containerIDs.Add(containerID)
	}

	p.net.router.Chits(p.id, chainID, requestID, containerIDs)
}
