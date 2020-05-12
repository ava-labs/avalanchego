// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package networking

// #include "salticidae/network.h"
// void getAcceptedFrontier(msg_t *, msgnetwork_conn_t *, void *);
// void acceptedFrontier(msg_t *, msgnetwork_conn_t *, void *);
// void getAccepted(msg_t *, msgnetwork_conn_t *, void *);
// void accepted(msg_t *, msgnetwork_conn_t *, void *);
// void get(msg_t *, msgnetwork_conn_t *, void *);
// void put(msg_t *, msgnetwork_conn_t *, void *);
// void pushQuery(msg_t *, msgnetwork_conn_t *, void *);
// void pullQuery(msg_t *, msgnetwork_conn_t *, void *);
// void chits(msg_t *, msgnetwork_conn_t *, void *);
import "C"

import (
	"errors"
	"fmt"
	"math"
	"unsafe"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/salticidae-go"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/networking/router"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/utils/random"
	"github.com/ava-labs/gecko/utils/timer"
)

// GossipSize is the maximum number of peers to gossip a container to
const (
	GossipSize = 50
)

var (
	// VotingNet implements the SenderExternal interface.
	VotingNet = Voting{}
)

var (
	errConnectionDropped = errors.New("connection dropped before receiving message")
)

// Voting implements the SenderExternal interface with a c++ library.
type Voting struct {
	votingMetrics

	log   logging.Logger
	vdrs  validators.Set
	net   salticidae.PeerNetwork
	conns Connections

	router   router.Router
	executor timer.Executor
}

// Initialize to the c networking library. Should only be called once ever.
func (s *Voting) Initialize(log logging.Logger, vdrs validators.Set, peerNet salticidae.PeerNetwork, conns Connections, router router.Router, registerer prometheus.Registerer) {
	log.AssertTrue(s.net == nil, "Should only register network handlers once")
	log.AssertTrue(s.conns == nil, "Should only set connections once")
	log.AssertTrue(s.router == nil, "Should only set the router once")

	s.log = log
	s.vdrs = vdrs
	s.net = peerNet
	s.conns = conns
	s.router = router

	s.votingMetrics.Initialize(log, registerer)

	net := peerNet.AsMsgNetwork()

	net.RegHandler(GetAcceptedFrontier, salticidae.MsgNetworkMsgCallback(C.getAcceptedFrontier), nil)
	net.RegHandler(AcceptedFrontier, salticidae.MsgNetworkMsgCallback(C.acceptedFrontier), nil)
	net.RegHandler(GetAccepted, salticidae.MsgNetworkMsgCallback(C.getAccepted), nil)
	net.RegHandler(Accepted, salticidae.MsgNetworkMsgCallback(C.accepted), nil)
	net.RegHandler(Get, salticidae.MsgNetworkMsgCallback(C.get), nil)
	net.RegHandler(Put, salticidae.MsgNetworkMsgCallback(C.put), nil)
	net.RegHandler(PushQuery, salticidae.MsgNetworkMsgCallback(C.pushQuery), nil)
	net.RegHandler(PullQuery, salticidae.MsgNetworkMsgCallback(C.pullQuery), nil)
	net.RegHandler(Chits, salticidae.MsgNetworkMsgCallback(C.chits), nil)

	s.executor.Initialize()
	go log.RecoverAndPanic(s.executor.Dispatch)
}

// Shutdown threads
func (s *Voting) Shutdown() { s.executor.Stop() }

// Accept is called after every consensus decision
func (s *Voting) Accept(chainID, containerID ids.ID, container []byte) error {
	return s.gossip(chainID, containerID, container)
}

// GetAcceptedFrontier implements the Sender interface.
func (s *Voting) GetAcceptedFrontier(validatorIDs ids.ShortSet, chainID ids.ID, requestID uint32) {
	peers := []salticidae.PeerID(nil)
	validatorIDList := validatorIDs.List()
	for _, validatorID := range validatorIDList {
		vID := validatorID
		if peer, exists := s.conns.GetPeerID(vID); exists {
			peers = append(peers, peer)
			s.log.Verbo("Sending a GetAcceptedFrontier to %s", vID)
		} else {
			s.log.Debug("Attempted to send a GetAcceptedFrontier message to a disconnected validator: %s", vID)
			s.executor.Add(func() { s.router.GetAcceptedFrontierFailed(vID, chainID, requestID) })
		}
	}

	build := Builder{}
	msg, err := build.GetAcceptedFrontier(chainID, requestID)
	s.log.AssertNoError(err)

	s.log.Verbo("Sending a GetAcceptedFrontier message."+
		"\nNumber of Validators: %d"+
		"\nChain: %s"+
		"\nRequest ID: %d",
		len(peers),
		chainID,
		requestID,
	)
	s.send(msg, peers...)
	s.numGetAcceptedFrontierSent.Add(float64(len(peers)))
}

// AcceptedFrontier implements the Sender interface.
func (s *Voting) AcceptedFrontier(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs ids.Set) {
	peer, exists := s.conns.GetPeerID(validatorID)
	if !exists {
		s.log.Debug("Attempted to send an AcceptedFrontier message to a disconnected validator: %s", validatorID)
		return // Validator is not connected
	}

	build := Builder{}
	msg, err := build.AcceptedFrontier(chainID, requestID, containerIDs)
	if err != nil {
		s.log.Error("Attempted to pack too large of an AcceptedFrontier message.\nNumber of containerIDs: %d", containerIDs.Len())
		return // Packing message failed
	}

	s.log.Verbo("Sending an AcceptedFrontier message."+
		"\nValidator: %s"+
		"\nChain: %s"+
		"\nRequest ID: %d"+
		"\nContainer IDs: %s",
		validatorID,
		chainID,
		requestID,
		containerIDs,
	)
	s.send(msg, peer)
	s.numAcceptedFrontierSent.Inc()
}

// GetAccepted implements the Sender interface.
func (s *Voting) GetAccepted(validatorIDs ids.ShortSet, chainID ids.ID, requestID uint32, containerIDs ids.Set) {
	peers := []salticidae.PeerID(nil)
	validatorIDList := validatorIDs.List()
	for _, validatorID := range validatorIDList {
		vID := validatorID
		if peer, exists := s.conns.GetPeerID(validatorID); exists {
			peers = append(peers, peer)
			s.log.Verbo("Sending a GetAccepted to %s", vID)
		} else {
			s.log.Debug("Attempted to send a GetAccepted message to a disconnected validator: %s", vID)
			s.executor.Add(func() { s.router.GetAcceptedFailed(vID, chainID, requestID) })
		}
	}

	build := Builder{}
	msg, err := build.GetAccepted(chainID, requestID, containerIDs)
	if err != nil {
		for _, peer := range peers {
			if validatorID, exists := s.conns.GetID(peer); exists {
				s.executor.Add(func() { s.router.GetAcceptedFailed(validatorID, chainID, requestID) })
			}
		}
		s.log.Debug("Attempted to pack too large of a GetAccepted message.\nNumber of containerIDs: %d", containerIDs.Len())
		return // Packing message failed
	}

	s.log.Verbo("Sending a GetAccepted message."+
		"\nNumber of Validators: %d"+
		"\nChain: %s"+
		"\nRequest ID: %d"+
		"\nContainer IDs:%s",
		len(peers),
		chainID,
		requestID,
		containerIDs,
	)
	s.send(msg, peers...)
	s.numGetAcceptedSent.Add(float64(len(peers)))
}

// Accepted implements the Sender interface.
func (s *Voting) Accepted(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs ids.Set) {
	peer, exists := s.conns.GetPeerID(validatorID)
	if !exists {
		s.log.Debug("Attempted to send an Accepted message to a disconnected validator: %s", validatorID)
		return // Validator is not connected
	}

	build := Builder{}
	msg, err := build.Accepted(chainID, requestID, containerIDs)
	if err != nil {
		s.log.Error("Attempted to pack too large of an Accepted message.\nNumber of containerIDs: %d", containerIDs.Len())
		return // Packing message failed
	}

	s.log.Verbo("Sending an Accepted message."+
		"\nValidator: %s"+
		"\nChain: %s"+
		"\nRequest ID: %d"+
		"\nContainer IDs: %s",
		validatorID,
		chainID,
		requestID,
		containerIDs,
	)
	s.send(msg, peer)
	s.numAcceptedSent.Inc()
}

// Get implements the Sender interface.
func (s *Voting) Get(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerID ids.ID) {
	peer, exists := s.conns.GetPeerID(validatorID)
	if !exists {
		s.log.Debug("Attempted to send a Get message to a disconnected validator: %s", validatorID)
		s.executor.Add(func() { s.router.GetFailed(validatorID, chainID, requestID, containerID) })
		return // Validator is not connected
	}

	build := Builder{}
	msg, err := build.Get(chainID, requestID, containerID)
	s.log.AssertNoError(err)

	s.log.Verbo("Sending a Get message."+
		"\nValidator: %s"+
		"\nChain: %s"+
		"\nRequest ID: %d"+
		"\nContainer ID: %s",
		validatorID,
		chainID,
		requestID,
		containerID,
	)
	s.send(msg, peer)
	s.numGetSent.Inc()
}

// Put implements the Sender interface.
func (s *Voting) Put(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerID ids.ID, container []byte) {
	peer, exists := s.conns.GetPeerID(validatorID)
	if !exists {
		s.log.Debug("Attempted to send a Container message to a disconnected validator: %s", validatorID)
		return // Validator is not connected
	}

	build := Builder{}
	msg, err := build.Put(chainID, requestID, containerID, container)
	if err != nil {
		s.log.Error("Attempted to pack too large of a Put message.\nContainer length: %d", len(container))
		return // Packing message failed
	}

	s.log.Verbo("Sending a Container message."+
		"\nValidator: %s"+
		"\nChain: %s"+
		"\nRequest ID: %d"+
		"\nContainer ID: %s"+
		"\nContainer:\n%s",
		validatorID,
		chainID,
		requestID,
		containerID,
		formatting.DumpBytes{Bytes: container},
	)
	s.send(msg, peer)
	s.numPutSent.Inc()
}

// PushQuery implements the Sender interface.
func (s *Voting) PushQuery(validatorIDs ids.ShortSet, chainID ids.ID, requestID uint32, containerID ids.ID, container []byte) {
	peers := []salticidae.PeerID(nil)
	validatorIDList := validatorIDs.List()
	for _, validatorID := range validatorIDList {
		vID := validatorID
		if peer, exists := s.conns.GetPeerID(vID); exists {
			peers = append(peers, peer)
			s.log.Verbo("Sending a PushQuery to %s", vID)
		} else {
			s.log.Debug("Attempted to send a PushQuery message to a disconnected validator: %s", vID)
			s.executor.Add(func() { s.router.QueryFailed(vID, chainID, requestID) })
		}
	}

	build := Builder{}
	msg, err := build.PushQuery(chainID, requestID, containerID, container)
	if err != nil {
		for _, peer := range peers {
			if validatorID, exists := s.conns.GetID(peer); exists {
				s.executor.Add(func() { s.router.QueryFailed(validatorID, chainID, requestID) })
			}
		}
		s.log.Error("Attempted to pack too large of a PushQuery message.\nContainer length: %d", len(container))
		return // Packing message failed
	}

	s.log.Verbo("Sending a PushQuery message."+
		"\nNumber of Validators: %d"+
		"\nChain: %s"+
		"\nRequest ID: %d"+
		"\nContainer ID: %s"+
		"\nContainer:\n%s",
		len(peers),
		chainID,
		requestID,
		containerID,
		formatting.DumpBytes{Bytes: container},
	)
	s.send(msg, peers...)
	s.numPushQuerySent.Add(float64(len(peers)))
}

// PullQuery implements the Sender interface.
func (s *Voting) PullQuery(validatorIDs ids.ShortSet, chainID ids.ID, requestID uint32, containerID ids.ID) {
	peers := []salticidae.PeerID(nil)
	validatorIDList := validatorIDs.List()
	for _, validatorID := range validatorIDList {
		vID := validatorID
		if peer, exists := s.conns.GetPeerID(vID); exists {
			peers = append(peers, peer)
			s.log.Verbo("Sending a PullQuery to %s", vID)
		} else {
			s.log.Warn("Attempted to send a PullQuery message to a disconnected validator: %s", vID)
			s.executor.Add(func() { s.router.QueryFailed(vID, chainID, requestID) })
		}
	}

	build := Builder{}
	msg, err := build.PullQuery(chainID, requestID, containerID)
	s.log.AssertNoError(err)

	s.log.Verbo("Sending a PullQuery message."+
		"\nNumber of Validators: %d"+
		"\nChain: %s"+
		"\nRequest ID: %d"+
		"\nContainer ID: %s",
		len(peers),
		chainID,
		requestID,
		containerID,
	)
	s.send(msg, peers...)
	s.numPullQuerySent.Add(float64(len(peers)))
}

// Chits implements the Sender interface.
func (s *Voting) Chits(validatorID ids.ShortID, chainID ids.ID, requestID uint32, votes ids.Set) {
	peer, exists := s.conns.GetPeerID(validatorID)
	if !exists {
		s.log.Debug("Attempted to send a Chits message to a disconnected validator: %s", validatorID)
		return // Validator is not connected
	}

	build := Builder{}
	msg, err := build.Chits(chainID, requestID, votes)
	if err != nil {
		s.log.Error("Attempted to pack too large of a Chits message.\nChits length: %d", votes.Len())
		return // Packing message failed
	}

	s.log.Verbo("Sending a Chits message."+
		"\nValidator: %s"+
		"\nChain: %s"+
		"\nRequest ID: %d"+
		"\nNumber of Chits: %d",
		validatorID,
		chainID,
		requestID,
		votes.Len(),
	)
	s.send(msg, peer)
	s.numChitsSent.Inc()
}

// Gossip attempts to gossip the container to the network
func (s *Voting) Gossip(chainID, containerID ids.ID, container []byte) {
	if err := s.gossip(chainID, containerID, container); err != nil {
		s.log.Error("Error gossiping container %s to %s\n%s", containerID, chainID, err)
	}
}

func (s *Voting) send(msg Msg, peers ...salticidae.PeerID) {
	ds := msg.DataStream()
	defer ds.Free()
	ba := salticidae.NewByteArrayMovedFromDataStream(ds, false)
	defer ba.Free()
	cMsg := salticidae.NewMsgMovedFromByteArray(msg.Op(), ba, false)
	defer cMsg.Free()

	switch len(peers) {
	case 0:
	case 1:
		s.net.SendMsg(cMsg, peers[0])
	default:
		s.net.MulticastMsgByMove(cMsg, peers)
	}
}

func (s *Voting) gossip(chainID, containerID ids.ID, container []byte) error {
	allPeers := s.conns.PeerIDs()

	numToGossip := GossipSize
	if numToGossip > len(allPeers) {
		numToGossip = len(allPeers)
	}
	peers := make([]salticidae.PeerID, numToGossip)

	sampler := random.Uniform{N: len(allPeers)}
	for i := range peers {
		peers[i] = allPeers[sampler.Sample()]
	}

	build := Builder{}
	msg, err := build.Put(chainID, math.MaxUint32, containerID, container)
	if err != nil {
		return fmt.Errorf("Attempted to pack too large of a Put message.\nContainer length: %d: %w", len(container), err)
	}

	s.log.Verbo("Sending a Put message to peers."+
		"\nNumber of Peers: %d"+
		"\nChain: %s"+
		"\nContainer ID: %s"+
		"\nContainer:\n%s",
		len(peers),
		chainID,
		containerID,
		formatting.DumpBytes{Bytes: container},
	)
	s.send(msg, peers...)
	s.numPutSent.Add(float64(len(peers)))
	return nil
}

// getAcceptedFrontier handles the recept of a getAcceptedFrontier container
// message for a chain
//export getAcceptedFrontier
func getAcceptedFrontier(_msg *C.struct_msg_t, _conn *C.struct_msgnetwork_conn_t, _ unsafe.Pointer) {
	VotingNet.numGetAcceptedFrontierReceived.Inc()

	validatorID, chainID, requestID, _, err := VotingNet.sanitize(_msg, _conn, GetAcceptedFrontier)
	if err != nil {
		VotingNet.log.Error("Failed to sanitize message due to: %s", err)
		return
	}

	VotingNet.router.GetAcceptedFrontier(validatorID, chainID, requestID)
}

// acceptedFrontier handles the recept of an acceptedFrontier message
//export acceptedFrontier
func acceptedFrontier(_msg *C.struct_msg_t, _conn *C.struct_msgnetwork_conn_t, _ unsafe.Pointer) {
	VotingNet.numAcceptedFrontierReceived.Inc()

	validatorID, chainID, requestID, msg, err := VotingNet.sanitize(_msg, _conn, AcceptedFrontier)
	if err != nil {
		VotingNet.log.Error("Failed to sanitize message due to: %s", err)
		return
	}

	containerIDs := ids.Set{}
	for _, containerIDBytes := range msg.Get(ContainerIDs).([][]byte) {
		containerID, err := ids.ToID(containerIDBytes)
		if err != nil {
			VotingNet.log.Warn("Error parsing ContainerID: %v", containerIDBytes)
			return
		}
		containerIDs.Add(containerID)
	}

	VotingNet.router.AcceptedFrontier(validatorID, chainID, requestID, containerIDs)
}

// getAccepted handles the recept of a getAccepted message
//export getAccepted
func getAccepted(_msg *C.struct_msg_t, _conn *C.struct_msgnetwork_conn_t, _ unsafe.Pointer) {
	VotingNet.numGetAcceptedReceived.Inc()

	validatorID, chainID, requestID, msg, err := VotingNet.sanitize(_msg, _conn, GetAccepted)
	if err != nil {
		VotingNet.log.Error("Failed to sanitize message due to: %s", err)
		return
	}

	containerIDs := ids.Set{}
	for _, containerIDBytes := range msg.Get(ContainerIDs).([][]byte) {
		containerID, err := ids.ToID(containerIDBytes)
		if err != nil {
			VotingNet.log.Warn("Error parsing ContainerID: %v", containerIDBytes)
			return
		}
		containerIDs.Add(containerID)
	}

	VotingNet.router.GetAccepted(validatorID, chainID, requestID, containerIDs)
}

// accepted handles the recept of an accepted message
//export accepted
func accepted(_msg *C.struct_msg_t, _conn *C.struct_msgnetwork_conn_t, _ unsafe.Pointer) {
	VotingNet.numAcceptedReceived.Inc()

	validatorID, chainID, requestID, msg, err := VotingNet.sanitize(_msg, _conn, Accepted)
	if err != nil {
		VotingNet.log.Error("Failed to sanitize message due to: %s", err)
		return
	}

	containerIDs := ids.Set{}
	for _, containerIDBytes := range msg.Get(ContainerIDs).([][]byte) {
		containerID, err := ids.ToID(containerIDBytes)
		if err != nil {
			VotingNet.log.Warn("Error parsing ContainerID: %v", containerIDBytes)
			return
		}
		containerIDs.Add(containerID)
	}

	VotingNet.router.Accepted(validatorID, chainID, requestID, containerIDs)
}

// get handles the recept of a get container message for a chain
//export get
func get(_msg *C.struct_msg_t, _conn *C.struct_msgnetwork_conn_t, _ unsafe.Pointer) {
	VotingNet.numGetReceived.Inc()

	validatorID, chainID, requestID, msg, err := VotingNet.sanitize(_msg, _conn, Get)
	if err != nil {
		VotingNet.log.Error("Failed to sanitize message due to: %s", err)
		return
	}

	containerID, _ := ids.ToID(msg.Get(ContainerID).([]byte))

	VotingNet.router.Get(validatorID, chainID, requestID, containerID)
}

// put handles the receipt of a container message
//export put
func put(_msg *C.struct_msg_t, _conn *C.struct_msgnetwork_conn_t, _ unsafe.Pointer) {
	VotingNet.numPutReceived.Inc()

	validatorID, chainID, requestID, msg, err := VotingNet.sanitize(_msg, _conn, Put)
	if err != nil {
		VotingNet.log.Error("Failed to sanitize message due to: %s", err)
		return
	}

	containerID, _ := ids.ToID(msg.Get(ContainerID).([]byte))

	containerBytes := msg.Get(ContainerBytes).([]byte)

	VotingNet.router.Put(validatorID, chainID, requestID, containerID, containerBytes)
}

// pushQuery handles the recept of a pull query message
//export pushQuery
func pushQuery(_msg *C.struct_msg_t, _conn *C.struct_msgnetwork_conn_t, _ unsafe.Pointer) {
	VotingNet.numPushQueryReceived.Inc()

	validatorID, chainID, requestID, msg, err := VotingNet.sanitize(_msg, _conn, PushQuery)
	if err != nil {
		VotingNet.log.Error("Failed to sanitize message due to: %s", err)
		return
	}

	containerID, _ := ids.ToID(msg.Get(ContainerID).([]byte))

	containerBytes := msg.Get(ContainerBytes).([]byte)

	VotingNet.router.PushQuery(validatorID, chainID, requestID, containerID, containerBytes)
}

// pullQuery handles the recept of a query message
//export pullQuery
func pullQuery(_msg *C.struct_msg_t, _conn *C.struct_msgnetwork_conn_t, _ unsafe.Pointer) {
	VotingNet.numPullQueryReceived.Inc()

	validatorID, chainID, requestID, msg, err := VotingNet.sanitize(_msg, _conn, PullQuery)
	if err != nil {
		VotingNet.log.Error("Failed to sanitize message due to: %s", err)
		return
	}

	containerID, _ := ids.ToID(msg.Get(ContainerID).([]byte))

	VotingNet.router.PullQuery(validatorID, chainID, requestID, containerID)
}

// chits handles the recept of a chits message
//export chits
func chits(_msg *C.struct_msg_t, _conn *C.struct_msgnetwork_conn_t, _ unsafe.Pointer) {
	VotingNet.numChitsReceived.Inc()

	validatorID, chainID, requestID, msg, err := VotingNet.sanitize(_msg, _conn, Chits)
	if err != nil {
		VotingNet.log.Error("Failed to sanitize message due to: %s", err)
		return
	}

	votes := ids.Set{}
	for _, voteBytes := range msg.Get(ContainerIDs).([][]byte) {
		vote, err := ids.ToID(voteBytes)
		if err != nil {
			VotingNet.log.Warn("Error parsing chit: %v", voteBytes)
			return
		}
		votes.Add(vote)
	}

	VotingNet.router.Chits(validatorID, chainID, requestID, votes)
}

func (s *Voting) sanitize(_msg *C.struct_msg_t, _conn *C.struct_msgnetwork_conn_t, op salticidae.Opcode) (ids.ShortID, ids.ID, uint32, Msg, error) {
	conn := salticidae.PeerNetworkConnFromC(salticidae.CPeerNetworkConn((*C.peernetwork_conn_t)(_conn)))
	peer := conn.GetPeerID(false)
	defer peer.Free()

	validatorID, exists := s.conns.GetID(peer)
	if !exists {
		return ids.ShortID{}, ids.ID{}, 0, nil, fmt.Errorf("message received from an un-registered peer")
	}

	s.log.Verbo("Receiving message from %s", validatorID)

	msg := salticidae.MsgFromC(salticidae.CMsg(_msg))
	codec := Codec{}
	pMsg, err := codec.Parse(op, msg.GetPayloadByMove())
	if err != nil {
		return ids.ShortID{}, ids.ID{}, 0, nil, err // The message couldn't be parsed
	}

	chainID, err := ids.ToID(pMsg.Get(ChainID).([]byte))
	s.log.AssertNoError(err)

	requestID := pMsg.Get(RequestID).(uint32)

	return validatorID, chainID, requestID, pMsg, nil
}
