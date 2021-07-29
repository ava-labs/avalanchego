// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/prometheus/client_golang/prometheus"
)

// Sender is a wrapper around an ExternalSender.
// Messages to this node are put directly into [router] rather than
// being sent over the network via the wrapped ExternalSender.
// Sender registers outbound requests with [router] so that [router]
// fires a timeout if we don't get a response to the request.
type Sender struct {
	ctx      *snow.Context
	sender   ExternalSender // Actually does the sending over the network
	router   router.Router
	timeouts *timeout.Manager

	// Request message type --> Counts how many of that request
	// have failed because the node was benched
	failedDueToBench map[constants.MsgType]prometheus.Counter
}

// Initialize this sender
func (s *Sender) Initialize(
	ctx *snow.Context,
	sender ExternalSender,
	router router.Router,
	timeouts *timeout.Manager,
	metricsNamespace string,
	metricsRegisterer prometheus.Registerer,
) error {
	s.ctx = ctx
	s.sender = sender
	s.router = router
	s.timeouts = timeouts

	// Register metrics
	// Message type --> String representation for metrics
	requestTypes := map[constants.MsgType]string{
		constants.GetMsg:                 "get",
		constants.GetAcceptedMsg:         "get_accepted",
		constants.GetAcceptedFrontierMsg: "get_accepted_frontier",
		constants.GetAncestorsMsg:        "get_ancestors",
		constants.PullQueryMsg:           "pull_query",
		constants.PushQueryMsg:           "push_query",
		constants.AppRequestMsg:          "app_request",
	}

	s.failedDueToBench = make(map[constants.MsgType]prometheus.Counter, len(requestTypes))

	for msgType, asStr := range requestTypes {
		counter := prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: metricsNamespace,
				Name:      fmt.Sprintf("%s_failed_benched", asStr),
				Help:      fmt.Sprintf("# of times a %s request was not sent because the node was benched", asStr),
			},
		)
		if err := metricsRegisterer.Register(counter); err != nil {
			return fmt.Errorf("couldn't register metric for %s: %w", msgType, err)
		}
		s.failedDueToBench[msgType] = counter
	}
	return nil
}

// Context of this sender
func (s *Sender) Context() *snow.Context { return s.ctx }

func (s *Sender) SendGetAcceptedFrontier(nodeIDs ids.ShortSet, requestID uint32) {
	// Sending a message to myself. No need to send it over the network.
	// Just put it right into the router. Asynchronously to avoid deadlock.
	if nodeIDs.Contains(s.ctx.NodeID) {
		nodeIDs.Remove(s.ctx.NodeID)
		// Note that this timeout duration won't exactly match the one that gets registered. That's OK.
		timeoutDuration := s.timeouts.TimeoutDuration()
		// Tell the router to expect a reply message from this node
		s.router.RegisterRequest(s.ctx.NodeID, s.ctx.ChainID, requestID, constants.GetAcceptedFrontierMsg)
		go s.router.GetAcceptedFrontier(s.ctx.NodeID, s.ctx.ChainID, requestID, time.Now().Add(timeoutDuration), nil)
	}

	// Some of the nodes in [nodeIDs] may be benched. That is, they've been unresponsive
	// so we don't even bother sending messages to them. We just have them immediately fail.
	for nodeID := range nodeIDs {
		if s.timeouts.IsBenched(nodeID, s.ctx.ChainID) {
			s.failedDueToBench[constants.GetAcceptedFrontierMsg].Inc() // update metric
			nodeIDs.Remove(nodeID)
			s.timeouts.RegisterRequestToUnreachableValidator()
			// Immediately register a failure. Do so asynchronously to avoid deadlock.
			go s.router.GetAcceptedFrontierFailed(nodeID, s.ctx.ChainID, requestID)
		}
	}

	// Try to send the messages over the network.
	// [sentTo] are the IDs of nodes who may receive the message.
	// Note that this timeout duration won't exactly match the one that gets registered. That's OK.
	timeoutDuration := s.timeouts.TimeoutDuration()
	sentTo := s.sender.SendGetAcceptedFrontier(nodeIDs, s.ctx.ChainID, requestID, timeoutDuration)

	// Tell the router to expect a reply message from these nodes
	for _, nodeID := range sentTo {
		nID := nodeID // Prevent overwrite in next loop iteration
		s.router.RegisterRequest(nID, s.ctx.ChainID, requestID, constants.GetAcceptedFrontierMsg)
		nodeIDs.Remove(nID)
	}

	// Register failures for nodes we didn't even send a request to.
	for nodeID := range nodeIDs {
		// Note: The call to RegisterRequestToUnreachableValidator is not strictly necessary.
		// This call causes the reported network latency look larger than it actually is.
		s.timeouts.RegisterRequestToUnreachableValidator()
		go s.router.GetAcceptedFrontierFailed(nodeID, s.ctx.ChainID, requestID)
	}
}

func (s *Sender) SendAcceptedFrontier(nodeID ids.ShortID, requestID uint32, containerIDs []ids.ID) {
	if nodeID == s.ctx.NodeID {
		go s.router.AcceptedFrontier(nodeID, s.ctx.ChainID, requestID, containerIDs, nil)
	} else {
		s.sender.SendAcceptedFrontier(nodeID, s.ctx.ChainID, requestID, containerIDs)
	}
}

func (s *Sender) SendGetAccepted(nodeIDs ids.ShortSet, requestID uint32, containerIDs []ids.ID) {
	// Sending a message to myself. No need to send it over the network.
	// Just put it right into the router. Asynchronously to avoid deadlock.
	if nodeIDs.Contains(s.ctx.NodeID) {
		nodeIDs.Remove(s.ctx.NodeID)
		// Note that this timeout duration won't exactly match the one that gets registered. That's OK.
		timeoutDuration := s.timeouts.TimeoutDuration()
		// Tell the router to expect a reply message from this node
		s.router.RegisterRequest(s.ctx.NodeID, s.ctx.ChainID, requestID, constants.GetAcceptedMsg)
		go s.router.GetAccepted(s.ctx.NodeID, s.ctx.ChainID, requestID, time.Now().Add(timeoutDuration), containerIDs, nil)
	}

	// Some of the nodes in [nodeIDs] may be benched. That is, they've been unresponsive
	// so we don't even bother sending messages to them. We just have them immediately fail.
	for nodeID := range nodeIDs {
		if s.timeouts.IsBenched(nodeID, s.ctx.ChainID) {
			s.failedDueToBench[constants.GetAcceptedMsg].Inc() // update metric
			nodeIDs.Remove(nodeID)
			s.timeouts.RegisterRequestToUnreachableValidator()
			// Immediately register a failure. Do so asynchronously to avoid deadlock.
			go s.router.GetAcceptedFailed(nodeID, s.ctx.ChainID, requestID)
		}
	}

	// Try to send the messages over the network.
	// [sentTo] are the IDs of nodes who may receive the message.
	// Note that this timeout duration won't exactly match the one that gets registered. That's OK.
	timeoutDuration := s.timeouts.TimeoutDuration()
	sentTo := s.sender.SendGetAccepted(nodeIDs, s.ctx.ChainID, requestID, timeoutDuration, containerIDs)

	// Tell the router to expect a reply message from these nodes
	for _, nodeID := range sentTo {
		nID := nodeID // Prevent overwrite in next loop iteration
		s.router.RegisterRequest(nID, s.ctx.ChainID, requestID, constants.GetAcceptedMsg)
		nodeIDs.Remove(nID)
	}

	// Register failures for nodes we didn't even send a request to.
	for nodeID := range nodeIDs {
		s.timeouts.RegisterRequestToUnreachableValidator()
		go s.router.GetAcceptedFailed(nodeID, s.ctx.ChainID, requestID)
	}
}

func (s *Sender) SendAccepted(nodeID ids.ShortID, requestID uint32, containerIDs []ids.ID) {
	if nodeID == s.ctx.NodeID {
		go s.router.Accepted(nodeID, s.ctx.ChainID, requestID, containerIDs, nil)
	} else {
		s.sender.SendAccepted(nodeID, s.ctx.ChainID, requestID, containerIDs)
	}
}

func (s *Sender) SendGetAncestors(nodeID ids.ShortID, requestID uint32, containerID ids.ID) {
	s.ctx.Log.Verbo("Sending GetAncestors to node %s. RequestID: %d. ContainerID: %s", nodeID.PrefixedString(constants.NodeIDPrefix), requestID, containerID)
	// Sending a GetAncestors to myself will always fail
	if nodeID == s.ctx.NodeID {
		go s.router.GetAncestorsFailed(nodeID, s.ctx.ChainID, requestID)
		return
	}

	// [nodeID] may be benched. That is, they've been unresponsive
	// so we don't even bother sending requests to them. We just have them immediately fail.
	if s.timeouts.IsBenched(nodeID, s.ctx.ChainID) {
		s.failedDueToBench[constants.GetAncestorsMsg].Inc() // update metric
		s.timeouts.RegisterRequestToUnreachableValidator()
		go s.router.GetAncestorsFailed(nodeID, s.ctx.ChainID, requestID)
		return
	}

	// Note that this timeout duration won't exactly match the one that gets registered. That's OK.
	timeoutDuration := s.timeouts.TimeoutDuration()
	sent := s.sender.SendGetAncestors(nodeID, s.ctx.ChainID, requestID, timeoutDuration, containerID)

	if sent {
		// Tell the router to expect a reply message from this node
		s.router.RegisterRequest(nodeID, s.ctx.ChainID, requestID, constants.GetAncestorsMsg)
		return
	}
	s.timeouts.RegisterRequestToUnreachableValidator()
	go s.router.GetAncestorsFailed(nodeID, s.ctx.ChainID, requestID)
}

// SendMultiPut sends a MultiPut message to the consensus engine running on the specified chain
// on the specified node.
// The MultiPut message gives the recipient the contents of several containers.
func (s *Sender) SendMultiPut(nodeID ids.ShortID, requestID uint32, containers [][]byte) {
	s.ctx.Log.Verbo("Sending MultiPut to node %s. RequestID: %d. NumContainers: %d", nodeID, requestID, len(containers))
	s.sender.SendMultiPut(nodeID, s.ctx.ChainID, requestID, containers)
}

// SendGet sends a Get message to the consensus engine running on the specified
// chain to the specified node. The Get message signifies that this
// consensus engine would like the recipient to send this consensus engine the
// specified container.
func (s *Sender) SendGet(nodeID ids.ShortID, requestID uint32, containerID ids.ID) {
	s.ctx.Log.Verbo("Sending Get to node %s. RequestID: %d. ContainerID: %s", nodeID.PrefixedString(constants.NodeIDPrefix), requestID, containerID)

	// Sending a Get to myself will always fail
	if nodeID == s.ctx.NodeID {
		go s.router.GetFailed(nodeID, s.ctx.ChainID, requestID)
		return
	}

	// [nodeID] may be benched. That is, they've been unresponsive
	// so we don't even bother sending requests to them. We just have them immediately fail.
	if s.timeouts.IsBenched(nodeID, s.ctx.ChainID) {
		s.failedDueToBench[constants.GetMsg].Inc() // update metric
		s.timeouts.RegisterRequestToUnreachableValidator()
		go s.router.GetFailed(nodeID, s.ctx.ChainID, requestID)
		return
	}

	// Note that this timeout duration won't exactly match the one that gets registered. That's OK.
	timeoutDuration := s.timeouts.TimeoutDuration()
	sent := s.sender.SendGet(nodeID, s.ctx.ChainID, requestID, timeoutDuration, containerID)

	if sent {
		// Tell the router to expect a reply message from this node
		s.router.RegisterRequest(nodeID, s.ctx.ChainID, requestID, constants.GetMsg)
		return
	}
	s.timeouts.RegisterRequestToUnreachableValidator()
	go s.router.GetFailed(nodeID, s.ctx.ChainID, requestID)
}

// SendPut sends a Put message to the consensus engine running on the specified chain
// on the specified node.
// The Put message signifies that this consensus engine is giving to the recipient
// the contents of the specified container.
func (s *Sender) SendPut(nodeID ids.ShortID, requestID uint32, containerID ids.ID, container []byte) {
	s.ctx.Log.Verbo("Sending Put to node %s. RequestID: %d. ContainerID: %s", nodeID.PrefixedString(constants.NodeIDPrefix), requestID, containerID)
	s.sender.SendPut(nodeID, s.ctx.ChainID, requestID, containerID, container)
}

// SendPushQuery sends a PushQuery message to the consensus engines running on the specified chains
// on the specified nodes.
// The PushQuery message signifies that this consensus engine would like each node to send
// their preferred frontier given the existence of the specified container.
func (s *Sender) SendPushQuery(nodeIDs ids.ShortSet, requestID uint32, containerID ids.ID, container []byte) {
	s.ctx.Log.Verbo("Sending PushQuery to nodes %v. RequestID: %d. ContainerID: %s", nodeIDs, requestID, containerID)

	// Note that this timeout duration won't exactly match the one that gets registered. That's OK.
	timeoutDuration := s.timeouts.TimeoutDuration()

	// Sending a message to myself. No need to send it over the network.
	// Just put it right into the router. Do so asynchronously to avoid deadlock.
	if nodeIDs.Contains(s.ctx.NodeID) {
		nodeIDs.Remove(s.ctx.NodeID)
		// Tell the router to expect a reply message from this node
		s.router.RegisterRequest(s.ctx.NodeID, s.ctx.ChainID, requestID, constants.PushQueryMsg)
		go s.router.PushQuery(
			s.ctx.NodeID,
			s.ctx.ChainID,
			requestID,
			time.Now().Add(timeoutDuration),
			containerID,
			container,
			nil,
		)
	}

	// Some of [nodeIDs] may be benched. That is, they've been unresponsive
	// so we don't even bother sending messages to them. We just have them immediately fail.
	for nodeID := range nodeIDs {
		if s.timeouts.IsBenched(nodeID, s.ctx.ChainID) {
			s.failedDueToBench[constants.PushQueryMsg].Inc() // update metric
			nodeIDs.Remove(nodeID)
			s.timeouts.RegisterRequestToUnreachableValidator()
			// Immediately register a failure. Do so asynchronously to avoid deadlock.
			go s.router.QueryFailed(nodeID, s.ctx.ChainID, requestID)
		}
	}

	// Try to send the messages over the network.
	// [sentTo] are the IDs of nodes who may receive the message.
	sentTo := s.sender.SendPushQuery(nodeIDs, s.ctx.ChainID, requestID, timeoutDuration, containerID, container)

	// Set timeouts so that if we don't hear back from these nodes, we register a failure.
	for _, nodeID := range sentTo {
		nID := nodeID // Prevent overwrite in next loop iteration
		// Tell the router to expect a reply message from this node
		s.router.RegisterRequest(nID, s.ctx.ChainID, requestID, constants.PushQueryMsg)
		nodeIDs.Remove(nID)
	}

	// Register failures for nodes we didn't even send a request to.
	for nodeID := range nodeIDs {
		s.timeouts.RegisterRequestToUnreachableValidator()
		go s.router.QueryFailed(nodeID, s.ctx.ChainID, requestID)
	}
}

// SendPullQuery sends a PullQuery message to the consensus engines running on the specified chains
// on the specified nodes.
// The PullQuery message signifies that this consensus engine would like each node to send
// their preferred frontier.
func (s *Sender) SendPullQuery(nodeIDs ids.ShortSet, requestID uint32, containerID ids.ID) {
	s.ctx.Log.Verbo("Sending PullQuery. RequestID: %d. ContainerID: %s", requestID, containerID)

	// Note that this timeout duration won't exactly match the one that gets registered. That's OK.
	timeoutDuration := s.timeouts.TimeoutDuration()

	// Sending a message to myself. No need to send it over the network.
	// Just put it right into the router. Do so asynchronously to avoid deadlock.
	if nodeIDs.Contains(s.ctx.NodeID) {
		nodeIDs.Remove(s.ctx.NodeID)
		// Register a timeout in case I don't respond to myself
		s.router.RegisterRequest(s.ctx.NodeID, s.ctx.ChainID, requestID, constants.PullQueryMsg)
		go s.router.PullQuery(
			s.ctx.NodeID,
			s.ctx.ChainID,
			requestID,
			time.Now().Add(timeoutDuration),
			containerID,
			nil,
		)
	}

	// Some of the nodes in [nodeIDs] may be benched. That is, they've been unresponsive
	// so we don't even bother sending messages to them. We just have them immediately fail.
	for nodeID := range nodeIDs {
		if s.timeouts.IsBenched(nodeID, s.ctx.ChainID) {
			s.failedDueToBench[constants.PullQueryMsg].Inc() // update metric
			nodeIDs.Remove(nodeID)
			s.timeouts.RegisterRequestToUnreachableValidator()
			// Immediately register a failure. Do so asynchronously to avoid deadlock.
			go s.router.QueryFailed(nodeID, s.ctx.ChainID, requestID)
		}
	}

	// Try to send the messages over the network.
	// [sentTo] are the IDs of nodes who may receive the message.
	sentTo := s.sender.SendPullQuery(nodeIDs, s.ctx.ChainID, requestID, timeoutDuration, containerID)

	// Set timeouts so that if we don't hear back from these nodes, we register a failure.
	for _, nodeID := range sentTo {
		nID := nodeID // Prevent overwrite in next loop iteration
		s.router.RegisterRequest(nID, s.ctx.ChainID, requestID, constants.PullQueryMsg)
		nodeIDs.Remove(nID)
	}

	// Register failures for nodes we didn't even send a request to.
	for nodeID := range nodeIDs {
		s.timeouts.RegisterRequestToUnreachableValidator()
		go s.router.QueryFailed(nodeID, s.ctx.ChainID, requestID)
	}
}

// SendAppRequest sends an application-level request to the given nodes.
// The meaning of this request, and how it should be handled, is defined by the VM.
func (s *Sender) SendAppRequest(nodeIDs ids.ShortSet, requestID uint32, appRequestBytes []byte) {
	s.ctx.Log.Verbo("Sending AppRequest. RequestID: %d. Message: %s", requestID, formatting.DumpBytes{Bytes: appRequestBytes})

	// Note that this timeout duration won't exactly match the one that gets registered. That's OK.
	timeoutDuration := s.timeouts.TimeoutDuration()

	// Sending a message to myself. No need to send it over the network.
	// Just put it right into the router. Do so asynchronously to avoid deadlock.
	if nodeIDs.Contains(s.ctx.NodeID) {
		nodeIDs.Remove(s.ctx.NodeID)
		// Register a timeout in case I don't respond to myself
		s.router.RegisterRequest(s.ctx.NodeID, s.ctx.ChainID, requestID, constants.AppRequestMsg)
		go s.router.AppRequest(s.ctx.NodeID, s.ctx.ChainID, requestID, time.Now().Add(timeoutDuration), appRequestBytes, nil)
	}

	// Some of the nodes in [nodeIDs] may be benched. That is, they've been unresponsive
	// so we don't even bother sending messages to them. We just have them immediately fail.
	for nodeID := range nodeIDs {
		if s.timeouts.IsBenched(nodeID, s.ctx.ChainID) {
			s.failedDueToBench[constants.AppRequestMsg].Inc() // update metric
			nodeIDs.Remove(nodeID)
			s.timeouts.RegisterRequestToUnreachableValidator()
			// Immediately register a failure. Do so asynchronously to avoid deadlock.
			go s.router.AppRequestFailed(nodeID, s.ctx.ChainID, requestID)
		}
	}

	// Try to send the messages over the network.
	// [sentTo] are the IDs of nodes who may receive the message.
	sentTo := s.sender.SendAppRequest(nodeIDs, s.ctx.ChainID, requestID, timeoutDuration, appRequestBytes)

	// Set timeouts so that if we don't hear back from these nodes, we register a failure.
	for _, nodeID := range sentTo {
		nID := nodeID // Prevent overwrite in next loop iteration
		s.router.RegisterRequest(nID, s.ctx.ChainID, requestID, constants.AppRequestMsg)
		nodeIDs.Remove(nID)
	}

	// Register failures for nodes we didn't even send a request to.
	for nodeID := range nodeIDs {
		s.timeouts.RegisterRequestToUnreachableValidator()
		go s.router.AppRequestFailed(nodeID, s.ctx.ChainID, requestID)
	}
}

// Sends a response to an application-level request from the given node
func (s *Sender) SendAppResponse(nodeID ids.ShortID, requestID uint32, appResponseBytes []byte) {
	if nodeID == s.ctx.NodeID {
		go s.router.AppResponse(nodeID, s.ctx.ChainID, requestID, appResponseBytes, nil)
	} else {
		s.sender.SendAppResponse(nodeID, s.ctx.ChainID, requestID, appResponseBytes)
	}
}

// Sends a application-level gossip message the given nodes. The node doesn't need to respond to
func (s *Sender) SendAppGossip(appResponseBytes []byte) {
	s.sender.SendAppGossip(s.ctx.ChainID, appResponseBytes)
}

// Chits sends chits
func (s *Sender) SendChits(nodeID ids.ShortID, requestID uint32, votes []ids.ID) {
	s.ctx.Log.Verbo("Sending Chits to node %s. RequestID: %d. Votes: %s", nodeID.PrefixedString(constants.NodeIDPrefix), requestID, votes)
	// If [nodeID] is myself, send this message directly
	// to my own router rather than sending it over the network
	if nodeID == s.ctx.NodeID {
		go s.router.Chits(nodeID, s.ctx.ChainID, requestID, votes, nil)
	} else {
		s.sender.SendChits(nodeID, s.ctx.ChainID, requestID, votes)
	}
}

// Gossip the provided container
func (s *Sender) SendGossip(containerID ids.ID, container []byte) {
	s.ctx.Log.Verbo("Gossiping %s", containerID)
	s.sender.SendGossip(s.ctx.ChainID, containerID, container)
}
