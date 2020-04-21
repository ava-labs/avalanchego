// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package networking

// #include "salticidae/network.h"
// bool connHandler(msgnetwork_conn_t *, bool, void *);
// void unknownPeerHandler(netaddr_t *, x509_t *, void *);
// void peerHandler(peernetwork_conn_t *, bool, void *);
// void ping(msg_t *, msgnetwork_conn_t *, void *);
// void pong(msg_t *, msgnetwork_conn_t *, void *);
// void getVersion(msg_t *, msgnetwork_conn_t *, void *);
// void version(msg_t *, msgnetwork_conn_t *, void *);
// void getPeerList(msg_t *, msgnetwork_conn_t *, void *);
// void peerList(msg_t *, msgnetwork_conn_t *, void *);
import "C"

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"
	"unsafe"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/salticidae-go"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/networking"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/utils/random"
	"github.com/ava-labs/gecko/utils/timer"
)

/*
Receive a new connection.
 - Send version message.
Receive version message.
 - Validate data
 - Send peer list
 - Mark this node as being connected
*/

/*
Periodically gossip peerlists.
 - Only connected stakers should be gossiped.
 - Gossip to a caped number of peers.
 - The peers to gossip to should be at least half full of stakers (or all the
	stakers should be in the set).
*/

/*
Attempt reconnections
 - If a non-staker disconnects, delete the connection
 - If a staker disconnects, attempt to reconnect to the node for awhile. If the
    node isn't connected to after awhile delete the connection.
*/

const (
	// CurrentVersion this avalanche instance is executing.
	CurrentVersion = "avalanche/0.0.1"
	// MaxClockDifference allowed between connected nodes.
	MaxClockDifference = time.Minute
	// PeerListGossipSpacing is the amount of time to wait between pushing this
	// node's peer list to other nodes.
	PeerListGossipSpacing = time.Minute
	// PeerListGossipSize is the number of peers to gossip each period.
	PeerListGossipSize = 100
	// PeerListStakerGossipFraction calculates the fraction of stakers that are
	// gossiped to. If set to 1, then only stakers will be gossiped to.
	PeerListStakerGossipFraction = 2

	// ConnectTimeout is the amount of time to wait before attempt to connect to
	// an unknown peer
	ConnectTimeout = time.Second
	// GetVersionTimeout is the amount of time to wait before sending a
	// getVersion message to a partially connected peer
	GetVersionTimeout = 2 * time.Second
	// ReconnectTimeout is the amount of time to wait to reconnect to a staker
	// before giving up
	ReconnectTimeout = 10 * time.Minute
)

// Manager is the struct that will be accessed on event calls
var (
	HandshakeNet = Handshake{}
)

var (
	errDSValidators = errors.New("couldn't get validator set of default subnet")
)

// Handshake handles the authentication of new peers. Only valid stakers
// will appear connected.
type Handshake struct {
	handshakeMetrics

	networkID uint32 // ID of the network I'm running, used to prevent connecting to the wrong network

	log           logging.Logger
	vdrs          validators.Set         // set of current validators in the AVAnet
	myAddr        salticidae.NetAddr     // IP I communicate to peers
	myID          ids.ShortID            // ID that identifies myself as a staker or not
	net           salticidae.PeerNetwork // C messaging network
	enableStaking bool                   // Should only be false for local tests

	clock timer.Clock

	// Connections that I have added by IP, but haven't gotten an ID from
	requestedLock    sync.Mutex
	requested        map[string]struct{}
	requestedTimeout timer.TimeoutManager

	// Connections that I have added as a peer, but haven't gotten a version
	// message from
	pending        Connections
	versionTimeout timer.TimeoutManager

	// Connections that I have gotten a valid version message from
	connections      Connections
	reconnectTimeout timer.TimeoutManager

	// IPs of nodes I'm connected to will be repeatedly gossiped throughout the network
	peerListGossiper *timer.Repeater

	// If any chain is blocked on connecting to peers, track these blockers here
	awaitingLock sync.Mutex
	awaiting     []*networking.AwaitingConnections
}

// Initialize to the c networking library. This should only be done once during
// node setup.
func (nm *Handshake) Initialize(
	log logging.Logger,
	vdrs validators.Set,
	myAddr salticidae.NetAddr,
	myID ids.ShortID,
	peerNet salticidae.PeerNetwork,
	registerer prometheus.Registerer,
	enableStaking bool,
	networkID uint32,
) {
	log.AssertTrue(nm.net == nil, "Should only register network handlers once")

	nm.handshakeMetrics.Initialize(log, registerer)

	nm.networkID = networkID

	nm.log = log
	nm.vdrs = vdrs
	nm.myAddr = myAddr
	nm.myID = myID
	nm.net = peerNet
	nm.enableStaking = enableStaking

	nm.requested = make(map[string]struct{})
	nm.requestedTimeout.Initialize(ConnectTimeout)
	go nm.log.RecoverAndPanic(nm.requestedTimeout.Dispatch)

	nm.pending = NewConnections()
	nm.versionTimeout.Initialize(GetVersionTimeout)
	go nm.log.RecoverAndPanic(nm.versionTimeout.Dispatch)

	nm.connections = NewConnections()
	nm.reconnectTimeout.Initialize(ReconnectTimeout)
	go nm.log.RecoverAndPanic(nm.reconnectTimeout.Dispatch)

	nm.peerListGossiper = timer.NewRepeater(nm.gossipPeerList, PeerListGossipSpacing)
	go nm.log.RecoverAndPanic(nm.peerListGossiper.Dispatch)

	// register c message callbacks
	net := peerNet.AsMsgNetwork()

	net.RegConnHandler(salticidae.MsgNetworkConnCallback(C.connHandler), nil)
	peerNet.RegPeerHandler(salticidae.PeerNetworkPeerCallback(C.peerHandler), nil)
	peerNet.RegUnknownPeerHandler(salticidae.PeerNetworkUnknownPeerCallback(C.unknownPeerHandler), nil)
	net.RegHandler(Ping, salticidae.MsgNetworkMsgCallback(C.ping), nil)
	net.RegHandler(Pong, salticidae.MsgNetworkMsgCallback(C.pong), nil)
	net.RegHandler(GetVersion, salticidae.MsgNetworkMsgCallback(C.getVersion), nil)
	net.RegHandler(Version, salticidae.MsgNetworkMsgCallback(C.version), nil)
	net.RegHandler(GetPeerList, salticidae.MsgNetworkMsgCallback(C.getPeerList), nil)
	net.RegHandler(PeerList, salticidae.MsgNetworkMsgCallback(C.peerList), nil)
}

// ConnectTo add the peer as a connection and connects to them. Will free peer
func (nm *Handshake) ConnectTo(peer salticidae.PeerID, addr salticidae.NetAddr) {
	if !nm.pending.ContainsPeerID(peer) && !nm.connections.ContainsPeerID(peer) {
		HandshakeNet.net.AddPeer(peer)
		HandshakeNet.net.SetPeerAddr(peer, addr)
		HandshakeNet.net.ConnPeer(peer, 600, 1)

		// TODO: Should add the peer to the pending set, register a timeout to
		// remove the peer
	}

	peer.Free()
}

// Connect ...
func (nm *Handshake) Connect(addr salticidae.NetAddr) {
	ip := toIPDesc(addr)
	ipStr := ip.String()
	if nm.pending.ContainsIP(ip) || nm.connections.ContainsIP(ip) {
		return
	}

	nm.log.Debug("Adding peer %s", ip)

	//TODO:

	if !nm.enableStaking {
		peer := salticidae.NewPeerIDFromNetAddr(addr, false)
		nm.ConnectTo(peer, addr)
	} else {
		nm.requestedConnections[ipStr] = struct{}{}

		nm.log.Verbo("Attempting to discover peer at %s", ipStr)

		msgNet := nm.net.AsMsgNetwork()
		msgNet.Connect(addr)
	}
}

// AwaitConnections ...
func (nm *Handshake) AwaitConnections(awaiting *networking.AwaitingConnections) {
	nm.awaitingLock.Lock()
	defer nm.awaitingLock.Unlock()

	awaiting.Add(nm.myID)
	for _, cert := range nm.connections.IDs().List() {
		awaiting.Add(cert)
	}
	if awaiting.Ready() {
		go awaiting.Finish()
	} else {
		nm.awaiting = append(nm.awaiting, awaiting)
	}
}

func (nm *Handshake) gossipPeerList() {
	stakers := []ids.ShortID{}
	nonStakers := []ids.ShortID{}
	for _, id := range nm.connections.IDs().List() {
		if nm.vdrs.Contains(id) {
			stakers = append(stakers, id)
		} else {
			nonStakers = append(nonStakers, id)
		}
	}

	numStakersToSend := (PeerListGossipSize + PeerListStakerGossipFraction - 1) / PeerListStakerGossipFraction
	if len(stakers) < numStakersToSend {
		numStakersToSend = len(stakers)
	}
	numNonStakersToSend := PeerListGossipSize - numStakersToSend
	if len(nonStakers) < numNonStakersToSend {
		numNonStakersToSend = len(nonStakers)
	}

	idsToSend := []ids.ShortID{}
	sampler := random.Uniform{N: len(stakers)}
	for i := 0; i < numStakersToSend; i++ {
		idsToSend = append(idsToSend, stakers[sampler.Sample()])
	}
	sampler.N = len(nonStakers)
	sampler.Replace()
	for i := 0; i < numNonStakersToSend; i++ {
		idsToSend = append(idsToSend, nonStakers[sampler.Sample()])
	}

	peers := []salticidae.PeerID{}
	for _, id := range idsToSend {
		if peer, exists := nm.connections.GetPeerID(id); exists {
			peers = append(peers, peer)
		}
	}

	nm.SendPeerList(peers...)
}

// Connections returns the object that tracks the nodes that are currently
// connected to this node.
func (nm *Handshake) Connections() Connections { return nm.connections }

// Shutdown the network
func (nm *Handshake) Shutdown() {
	nm.versionTimeout.Stop()
	nm.peerListGossiper.Stop()
}

// SendGetVersion to the requested peer
func (nm *Handshake) SendGetVersion(peer salticidae.PeerID) {
	build := Builder{}
	gv, err := build.GetVersion()
	nm.log.AssertNoError(err)
	nm.send(gv, peer)

	nm.numGetVersionSent.Inc()
}

// SendVersion to the requested peer
func (nm *Handshake) SendVersion(peer salticidae.PeerID) error {
	build := Builder{}
	v, err := build.Version(nm.networkID, nm.clock.Unix(), toIPDesc(nm.myAddr), CurrentVersion)
	if err != nil {
		return fmt.Errorf("packing Version failed due to %s", err)
	}
	nm.send(v, peer)
	nm.numVersionSent.Inc()
	return nil
}

// SendPeerList to the requested peer
func (nm *Handshake) SendPeerList(peers ...salticidae.PeerID) error {
	if len(peers) == 0 {
		return nil
	}

	_, ids, ips := nm.connections.Conns()
	ipsToSend := []utils.IPDesc(nil)
	for i, id := range ids {
		ip := ips[i]
		if !ip.IsZero() && nm.vdrs.Contains(id) {
			ipsToSend = append(ipsToSend, ip)
		}
	}

	if len(ipsToSend) == 0 {
		nm.log.Debug("No IPs to send to %d peer(s)", len(peers))
		return nil
	}

	nm.log.Verbo("Sending %d ips to %d peer(s)", len(ipsToSend), len(peers))

	build := Builder{}
	pl, err := build.PeerList(ipsToSend)
	if err != nil {
		return fmt.Errorf("Packing Peerlist failed due to %w", err)
	}
	nm.send(pl, peers...)
	nm.numPeerlistSent.Add(float64(len(peers)))
	return nil
}

func (nm *Handshake) send(msg Msg, peers ...salticidae.PeerID) {
	ds := msg.DataStream()
	defer ds.Free()
	ba := salticidae.NewByteArrayMovedFromDataStream(ds, false)
	defer ba.Free()
	cMsg := salticidae.NewMsgMovedFromByteArray(msg.Op(), ba, false)
	defer cMsg.Free()

	switch len(peers) {
	case 0:
	case 1:
		nm.net.SendMsg(cMsg, peers[0])
	default:
		nm.net.MulticastMsgByMove(cMsg, peers)
	}
}

// connHandler notifies of a new inbound connection
//export connHandler
func connHandler(_conn *C.struct_msgnetwork_conn_t, connected C.bool, _ unsafe.Pointer) C.bool {
	if !HandshakeNet.enableStaking || !bool(connected) {
		return connected
	}

	conn := salticidae.MsgNetworkConnFromC(salticidae.CMsgNetworkConn(_conn))
	addr := conn.GetAddr()
	ip := toIPDesc(addr)

	ipStr := ip.String()
	if _, exists := HandshakeNet.requestedConnections[ipStr]; !exists {
		HandshakeNet.log.Debug("connHandler called with %s", ip)
		return true
	}
	delete(HandshakeNet.requestedConnections, ipStr)

	cert := conn.GetPeerCert()
	peer := salticidae.NewPeerIDFromX509(cert, false)
	HandshakeNet.ConnectTo(peer, addr)
	return true
}

func (nm *Handshake) connectedToPeer(conn *C.struct_peernetwork_conn_t, peer salticidae.PeerID) {
	peerBytes := toID(peer)
	peerID := ids.NewID(peerBytes)

	// If we're enforcing staking, use a peer's certificate to uniquely identify them
	// Otherwise, use a hash of their ip to identify them
	cert := ids.ShortID{}
	if nm.enableStaking {
		cert = getPeerCert(conn)
	} else {
		key := [20]byte{}
		copy(key[:], peerID.Bytes())
		cert = ids.NewShortID(key)
	}

	nm.log.Debug("Connected to %s", cert)

	nm.reconnectTimeout.Remove(peerID)

	nm.pending.Add(peer, cert, utils.IPDesc{})

	handler := new(func())
	*handler = func() {
		if nm.pending.ContainsPeerID(peer) {
			nm.SendGetVersion(peer)
			nm.versionTimeout.Put(peerID, *handler)
		}
	}
	(*handler)()
}

func (nm *Handshake) disconnectedFromPeer(peer salticidae.PeerID) {
	cert := ids.ShortID{}
	if pendingCert, exists := nm.pending.GetID(peer); exists {
		cert = pendingCert
		nm.log.Info("Disconnected from pending peer %s", cert)
	} else if connectedCert, exists := nm.connections.GetID(peer); exists {
		cert = connectedCert
		nm.log.Info("Disconnected from peer %s", cert)
	} else {
		return
	}

	peerBytes := toID(peer)
	peerID := ids.NewID(peerBytes)

	if nm.vdrs.Contains(cert) {
		nm.reconnectTimeout.Put(peerID, func() {
			nm.net.DelPeer(peer)
		})
	} else {
		nm.net.DelPeer(peer)
	}
	nm.versionTimeout.Remove(peerID)

	if !nm.enableStaking {
		nm.vdrs.Remove(cert)
	}

	nm.pending.RemovePeerID(peer)
	nm.connections.RemovePeerID(peer)
	nm.numPeers.Set(float64(nm.connections.Len()))

	nm.awaitingLock.Lock()
	defer nm.awaitingLock.Unlock()
	for _, awaiting := range HandshakeNet.awaiting {
		awaiting.Remove(cert)
	}
}

// peerHandler notifies a change to the set of connected peers
// connected is true if a new peer is connected
// connected is false if a formerly connected peer has disconnected
//export peerHandler
func peerHandler(_conn *C.struct_peernetwork_conn_t, connected C.bool, _ unsafe.Pointer) {
	HandshakeNet.log.Debug("peerHandler called")

	pConn := salticidae.PeerNetworkConnFromC(salticidae.CPeerNetworkConn(_conn))
	peer := pConn.GetPeerID(true)

	if connected {
		HandshakeNet.connectedToPeer(_conn, peer)
	} else {
		HandshakeNet.disconnectedFromPeer(peer)
	}
}

// unknownPeerHandler notifies of an unknown peer connection attempt
//export unknownPeerHandler
func unknownPeerHandler(_addr *C.netaddr_t, _cert *C.x509_t, _ unsafe.Pointer) {
	HandshakeNet.log.Debug("unknownPeerHandler called")

	addr := salticidae.NetAddrFromC(salticidae.CNetAddr(_addr)).Copy(true)
	ip := toIPDesc(addr)

	HandshakeNet.log.Info("Adding peer %s", ip)

	var peer salticidae.PeerID
	if HandshakeNet.enableStaking {
		cert := salticidae.X509FromC(salticidae.CX509(_cert))
		peer = salticidae.NewPeerIDFromX509(cert, true)
	} else {
		peer = salticidae.NewPeerIDFromNetAddr(addr, true)
	}

	peerBytes := toID(peer)
	peerID := ids.NewID(peerBytes)

	HandshakeNet.reconnectTimeout.Put(peerID, func() {
		HandshakeNet.net.DelPeer(peer)
	})
	HandshakeNet.net.AddPeer(peer)
}

// ping handles the recept of a ping message
//export ping
func ping(_ *C.struct_msg_t, _conn *C.struct_msgnetwork_conn_t, _ unsafe.Pointer) {
	conn := salticidae.PeerNetworkConnFromC(salticidae.CPeerNetworkConn(_conn))
	peer := conn.GetPeerID(false)
	defer peer.Free()

	build := Builder{}
	pong, err := build.Pong()
	HandshakeNet.log.AssertNoError(err)

	HandshakeNet.send(pong, peer)
}

// pong handles the recept of a pong message
//export pong
func pong(*C.struct_msg_t, *C.struct_msgnetwork_conn_t, unsafe.Pointer) {}

// getVersion handles the recept of a getVersion message
//export getVersion
func getVersion(_msg *C.struct_msg_t, _conn *C.struct_msgnetwork_conn_t, _ unsafe.Pointer) {
	HandshakeNet.numGetVersionReceived.Inc()

	conn := salticidae.PeerNetworkConnFromC(salticidae.CPeerNetworkConn(_conn))
	peer := conn.GetPeerID(false)
	defer peer.Free()

	HandshakeNet.SendVersion(peer)
}

// version handles the recept of a version message
//export version
func version(_msg *C.struct_msg_t, _conn *C.struct_msgnetwork_conn_t, _ unsafe.Pointer) {
	HandshakeNet.numVersionReceived.Inc()

	msg := salticidae.MsgFromC(salticidae.CMsg(_msg))
	conn := salticidae.PeerNetworkConnFromC(salticidae.CPeerNetworkConn(_conn))
	peer := conn.GetPeerID(true)

	id, exists := HandshakeNet.pending.GetID(peer)
	if !exists {
		return
	}

	defer HandshakeNet.pending.Remove(peer, id)

	build := Builder{}
	pMsg, err := build.Parse(Version, msg.GetPayloadByMove())
	if err != nil {
		HandshakeNet.log.Warn("Failed to parse Version message")

		HandshakeNet.net.DelPeer(peer)
		return
	}

	if networkID := pMsg.Get(NetworkID).(uint32); networkID != HandshakeNet.networkID {
		HandshakeNet.log.Warn("Peer's network ID doesn't match our networkID: Peer's = %d ; Ours = %d", networkID, HandshakeNet.networkID)

		HandshakeNet.net.DelPeer(peer)
		return
	}

	myTime := float64(HandshakeNet.clock.Unix())
	if peerTime := float64(pMsg.Get(MyTime).(uint64)); math.Abs(peerTime-myTime) > MaxClockDifference.Seconds() {
		HandshakeNet.log.Warn("Peer's clock is too far out of sync with mine. His = %d, Mine = %d (seconds)", uint64(peerTime), uint64(myTime))

		HandshakeNet.net.DelPeer(peer)
		return
	}

	if peerVersion := pMsg.Get(VersionStr).(string); !checkCompatibility(CurrentVersion, peerVersion) {
		HandshakeNet.log.Warn("Bad version")

		HandshakeNet.net.DelPeer(peer)
		return
	}

	ip := pMsg.Get(IP).(utils.IPDesc)

	HandshakeNet.log.Debug("Finishing handshake with %s", ip)

	HandshakeNet.SendPeerList(peer)
	HandshakeNet.connections.Add(peer, id, ip)

	peerBytes := toID(peer)
	peerID := ids.NewID(peerBytes)

	HandshakeNet.versionTimeout.Remove(peerID)

	if !HandshakeNet.enableStaking {
		HandshakeNet.vdrs.Add(validators.NewValidator(id, 1))
	}

	HandshakeNet.numPeers.Set(float64(HandshakeNet.connections.Len()))

	HandshakeNet.awaitingLock.Lock()
	defer HandshakeNet.awaitingLock.Unlock()

	for i := 0; i < len(HandshakeNet.awaiting); i++ {
		awaiting := HandshakeNet.awaiting[i]
		awaiting.Add(id)
		if !awaiting.Ready() {
			continue
		}

		newLen := len(HandshakeNet.awaiting) - 1
		HandshakeNet.awaiting[i] = HandshakeNet.awaiting[newLen]
		HandshakeNet.awaiting = HandshakeNet.awaiting[:newLen]

		i--

		go awaiting.Finish()
	}
}

// getPeerList handles the recept of a getPeerList message
//export getPeerList
func getPeerList(_ *C.struct_msg_t, _conn *C.struct_msgnetwork_conn_t, _ unsafe.Pointer) {
	HandshakeNet.numGetPeerlistReceived.Inc()

	conn := salticidae.PeerNetworkConnFromC(salticidae.CPeerNetworkConn(_conn))
	peer := conn.GetPeerID(false)
	defer peer.Free()

	HandshakeNet.SendPeerList(peer)
}

// peerList handles the recept of a peerList message
//export peerList
func peerList(_msg *C.struct_msg_t, _conn *C.struct_msgnetwork_conn_t, _ unsafe.Pointer) {
	HandshakeNet.numPeerlistReceived.Inc()

	msg := salticidae.MsgFromC(salticidae.CMsg(_msg))
	build := Builder{}
	pMsg, err := build.Parse(PeerList, msg.GetPayloadByMove())
	if err != nil {
		HandshakeNet.log.Warn("Failed to parse PeerList message due to %s", err)
		// TODO: What should we do here?
		return
	}

	ips := pMsg.Get(Peers).([]utils.IPDesc)
	cErr := salticidae.NewError()
	for _, ip := range ips {
		addr := salticidae.NewNetAddrFromIPPortString(ip.String(), true, &cErr)

		if cErr.GetCode() != 0 || HandshakeNet.myAddr.IsEq(addr) {
			// Make sure not to connect to myself
			continue
		}

		HandshakeNet.Connect(addr)
	}
}

func getPeerCert(_conn *C.struct_peernetwork_conn_t) ids.ShortID {
	conn := salticidae.MsgNetworkConnFromC(salticidae.CMsgNetworkConn(_conn))
	return getCert(conn.GetPeerCert())
}

func getCert(cert salticidae.X509) ids.ShortID {
	der := cert.GetDer(false)
	certDS := salticidae.NewDataStreamMovedFromByteArray(der, false)
	certBytes := certDS.GetDataInPlace(certDS.Size()).Get()
	certID, err := ids.ToShortID(hashing.PubkeyBytesToAddress(certBytes))

	certDS.Free()
	der.Free()
	HandshakeNet.log.AssertNoError(err)
	return certID
}

// checkCompatibility Check to make sure that the peer and I speak the same language.
func checkCompatibility(myVersion string, peerVersion string) bool {
	// At the moment, we are all compatible.
	return true
}
