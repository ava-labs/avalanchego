// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/utils/constants"
)

// Sender sends consensus messages to other validators
type Sender struct {
	ctx      *snow.Context
	sender   ExternalSender // Actually does the sending over the network
	router   router.Router
	timeouts *timeout.Manager
}

// Initialize this sender
func (s *Sender) Initialize(ctx *snow.Context, sender ExternalSender, router router.Router, timeouts *timeout.Manager) {
	s.ctx = ctx
	s.sender = sender
	s.router = router
	s.timeouts = timeouts
}

// Context of this sender
func (s *Sender) Context() *snow.Context { return s.ctx }

// GetAcceptedFrontier ...
func (s *Sender) GetAcceptedFrontier(validatorIDs ids.ShortSet, requestID uint32) {
	// TODO this timeout duration won't exactly match the one that gets registered.
	// I think that's OK, but double check.
	timeoutDuration := s.timeouts.TimeoutDuration()
	deadline := time.Now().Add(timeoutDuration)

	// A GetAcceptedFrontier to myself will always fail.
	if validatorIDs.Contains(s.ctx.NodeID) {
		validatorIDs.Remove(s.ctx.NodeID)
		go s.router.GetAcceptedFrontierFailed(s.ctx.NodeID, s.ctx.ChainID, requestID)
	}

	// Some of the validators in [validatorIDs] may be benched. That is, they've been unresponsive
	// so we don't even bother sending messages to them. We just have them immediately fail.
	for validatorID := range validatorIDs {
		if s.timeouts.IsBenched(validatorID, s.ctx.ChainID) {
			validatorIDs.Remove(validatorID)
			// Immediately register a failure. Do so asynchronously to avoid deadlock.
			go s.router.GetAcceptedFrontierFailed(s.ctx.NodeID, s.ctx.ChainID, requestID)
		}
	}

	// Try to send the messages over the network.
	// [sentTo] are the IDs of validators who may receive the message.
	sentTo := s.sender.GetAcceptedFrontier(validatorIDs, s.ctx.ChainID, requestID, deadline)

	// Set timeouts so that if we don't hear back from these validators, we register a failure.
	for _, validatorID := range sentTo {
		vID := validatorID // Prevent overwrite in next loop iteration
		// TODO do we need the return values here?
		s.timeouts.RegisterRequest(vID, s.ctx.ChainID, requestID, true, constants.GetAcceptedFrontierMsg, func() {
			s.router.GetAcceptedFrontierFailed(vID, s.ctx.ChainID, requestID)
		})
		validatorIDs.Remove(vID)
	}

	for validatorID := range validatorIDs {
		vID := validatorID // Prevent overwrite in next loop iteration
		go s.router.GetAcceptedFrontierFailed(vID, s.ctx.ChainID, requestID)
	}
}

// AcceptedFrontier ...
func (s *Sender) AcceptedFrontier(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) {
	if validatorID == s.ctx.NodeID {
		go s.router.AcceptedFrontier(validatorID, s.ctx.ChainID, requestID, containerIDs)
	} else {
		s.sender.AcceptedFrontier(validatorID, s.ctx.ChainID, requestID, containerIDs)
	}
}

// GetAccepted ...
func (s *Sender) GetAccepted(validatorIDs ids.ShortSet, requestID uint32, containerIDs []ids.ID) {
	// TODO this timeout duration won't exactly match the one that gets registered.
	// I think that's OK, but double check.
	timeoutDuration := s.timeouts.TimeoutDuration()
	deadline := time.Now().Add(timeoutDuration)

	// A GetAccepted to myself will always fail.
	if validatorIDs.Contains(s.ctx.NodeID) {
		validatorIDs.Remove(s.ctx.NodeID)
		go s.router.GetAcceptedFailed(s.ctx.NodeID, s.ctx.ChainID, requestID)
	}

	// Some of the validators in [validatorIDs] may be benched. That is, they've been unresponsive
	// so we don't even bother sending messages to them. We just have them immediately fail.
	for validatorID := range validatorIDs {
		if s.timeouts.IsBenched(validatorID, s.ctx.ChainID) {
			validatorIDs.Remove(validatorID)
			// Immediately register a failure. Do so asynchronously to avoid deadlock.
			go s.router.GetAcceptedFailed(s.ctx.NodeID, s.ctx.ChainID, requestID)
		}
	}

	// Try to send the messages over the network.
	// [sentTo] are the IDs of validators who may receive the message.
	sentTo := s.sender.GetAccepted(validatorIDs, s.ctx.ChainID, requestID, deadline, containerIDs)

	// Set timeouts so that if we don't hear back from these validators, we register a failure.
	for _, validatorID := range sentTo {
		vID := validatorID // Prevent overwrite in next loop iteration
		// TODO do we need the return values here?
		s.timeouts.RegisterRequest(vID, s.ctx.ChainID, requestID, true, constants.GetAcceptedMsg, func() {
			s.router.GetAcceptedFailed(vID, s.ctx.ChainID, requestID)
		})
		validatorIDs.Remove(vID)
	}

	for validatorID := range validatorIDs {
		vID := validatorID // Prevent overwrite in next loop iteration
		go s.router.GetAcceptedFailed(vID, s.ctx.ChainID, requestID)
	}
}

// Accepted ...
func (s *Sender) Accepted(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) {
	if validatorID == s.ctx.NodeID {
		go s.router.Accepted(validatorID, s.ctx.ChainID, requestID, containerIDs)
	} else {
		s.sender.Accepted(validatorID, s.ctx.ChainID, requestID, containerIDs)
	}
}

// GetAncestors sends a GetAncestors message
func (s *Sender) GetAncestors(validatorID ids.ShortID, requestID uint32, containerID ids.ID) {
	s.ctx.Log.Verbo("Sending GetAncestors to validator %s. RequestID: %d. ContainerID: %s", validatorID, requestID, containerID)
	// Sending a GetAncestors to myself will always fail
	if validatorID == s.ctx.NodeID {
		go s.router.GetAncestorsFailed(validatorID, s.ctx.ChainID, requestID)
		return
	}

	timeoutDuration := s.timeouts.TimeoutDuration()
	deadline := time.Now().Add(timeoutDuration)
	sent := s.sender.GetAncestors(validatorID, s.ctx.ChainID, requestID, deadline, containerID)

	if sent {
		s.timeouts.RegisterRequest(validatorID, s.ctx.ChainID, requestID, false, constants.GetAncestorsMsg, func() {
			s.router.GetAncestorsFailed(validatorID, s.ctx.ChainID, requestID)
		})
		return
	}
	go s.router.GetAncestorsFailed(validatorID, s.ctx.ChainID, requestID)
}

// MultiPut sends a MultiPut message to the consensus engine running on the specified chain
// on the specified validator.
// The MultiPut message gives the recipient the contents of several containers.
func (s *Sender) MultiPut(validatorID ids.ShortID, requestID uint32, containers [][]byte) {
	s.ctx.Log.Verbo("Sending MultiPut to validator %s. RequestID: %d. NumContainers: %d", validatorID, requestID, len(containers))
	s.sender.MultiPut(validatorID, s.ctx.ChainID, requestID, containers)
}

// Get sends a Get message to the consensus engine running on the specified
// chain to the specified validator. The Get message signifies that this
// consensus engine would like the recipient to send this consensus engine the
// specified container.
func (s *Sender) Get(validatorID ids.ShortID, requestID uint32, containerID ids.ID) {
	s.ctx.Log.Verbo("Sending Get to validator %s. RequestID: %d. ContainerID: %s", validatorID, requestID, containerID)

	// Sending a Get to myself will always fail
	if validatorID == s.ctx.NodeID {
		go s.router.GetFailed(validatorID, s.ctx.ChainID, requestID)
		return
	}

	timeoutDuration := s.timeouts.TimeoutDuration()
	deadline := time.Now().Add(timeoutDuration)
	sent := s.sender.Get(validatorID, s.ctx.ChainID, requestID, deadline, containerID)

	if sent {
		// Add a timeout -- if we don't get a response before the timeout expires,
		// send this consensus engine a GetFailed message
		s.timeouts.RegisterRequest(validatorID, s.ctx.ChainID, requestID, true, constants.GetMsg, func() {
			s.router.GetFailed(validatorID, s.ctx.ChainID, requestID)
		})
		return
	}
	go s.router.GetFailed(validatorID, s.ctx.ChainID, requestID)
}

// Put sends a Put message to the consensus engine running on the specified chain
// on the specified validator.
// The Put message signifies that this consensus engine is giving to the recipient
// the contents of the specified container.
func (s *Sender) Put(validatorID ids.ShortID, requestID uint32, containerID ids.ID, container []byte) {
	s.ctx.Log.Verbo("Sending Put to validator %s. RequestID: %d. ContainerID: %s", validatorID, requestID, containerID)
	s.sender.Put(validatorID, s.ctx.ChainID, requestID, containerID, container)
}

// PushQuery sends a PushQuery message to the consensus engines running on the specified chains
// on the specified validators.
// The PushQuery message signifies that this consensus engine would like each validator to send
// their preferred frontier given the existence of the specified container.
func (s *Sender) PushQuery(validatorIDs ids.ShortSet, requestID uint32, containerID ids.ID, container []byte) {
	s.ctx.Log.Verbo("Sending PushQuery to validators %v. RequestID: %d. ContainerID: %s", validatorIDs, requestID, containerID)

	// TODO this timeout duration won't exactly match the one that gets registered.
	// I think that's OK, but double check.
	timeoutDuration := s.timeouts.TimeoutDuration()
	deadline := time.Now().Add(timeoutDuration)

	// Sending a message to myself. No need to send it over the network.
	// Just put it right into the router. Do so asynchronously to avoid deadlock.
	if validatorIDs.Contains(s.ctx.NodeID) {
		validatorIDs.Remove(s.ctx.NodeID)
		// Register a timeout in case I don't respond to myself
		s.timeouts.RegisterRequest(s.ctx.NodeID, s.ctx.ChainID, requestID, true, constants.PushQueryMsg, func() {
			s.router.QueryFailed(s.ctx.NodeID, s.ctx.ChainID, requestID)
		})
		go s.router.PushQuery(s.ctx.NodeID, s.ctx.ChainID, requestID, deadline, containerID, container)
	}

	// Some of the validators in [validatorIDs] may be benched. That is, they've been unresponsive
	// so we don't even bother sending messages to them. We just have them immediately fail.
	for validatorID := range validatorIDs {
		if s.timeouts.IsBenched(validatorID, s.ctx.ChainID) {
			validatorIDs.Remove(validatorID)
			// Immediately register a failure. Do so asynchronously to avoid deadlock.
			go s.router.QueryFailed(s.ctx.NodeID, s.ctx.ChainID, requestID)
		}
	}

	// Try to send the messages over the network.
	// [sentTo] are the IDs of validators who may receive the message.
	sentTo := s.sender.PushQuery(validatorIDs, s.ctx.ChainID, requestID, deadline, containerID, container)

	// Set timeouts so that if we don't hear back from these validators, we register a failure.
	for _, validatorID := range sentTo {
		vID := validatorID // Prevent overwrite in next loop iteration
		// TODO do we need the return values here?
		s.timeouts.RegisterRequest(vID, s.ctx.ChainID, requestID, true, constants.PushQueryMsg, func() {
			s.router.QueryFailed(vID, s.ctx.ChainID, requestID)
		})
		validatorIDs.Remove(vID)
	}

	for validatorID := range validatorIDs {
		vID := validatorID // Prevent overwrite in next loop iteration
		go s.router.QueryFailed(vID, s.ctx.ChainID, requestID)
	}
}

// PullQuery sends a PullQuery message to the consensus engines running on the specified chains
// on the specified validators.
// The PullQuery message signifies that this consensus engine would like each validator to send
// their preferred frontier.
func (s *Sender) PullQuery(validatorIDs ids.ShortSet, requestID uint32, containerID ids.ID) {
	s.ctx.Log.Verbo("Sending PullQuery. RequestID: %d. ContainerID: %s", requestID, containerID)

	// TODO this timeout duration won't exactly match the one that gets registered.
	// I think that's OK, but double check.
	timeoutDuration := s.timeouts.TimeoutDuration()
	deadline := time.Now().Add(timeoutDuration)

	// Sending a message to myself. No need to send it over the network.
	// Just put it right into the router. Do so asynchronously to avoid deadlock.
	if validatorIDs.Contains(s.ctx.NodeID) {
		validatorIDs.Remove(s.ctx.NodeID)
		// Register a timeout in case I don't respond to myself
		s.timeouts.RegisterRequest(s.ctx.NodeID, s.ctx.ChainID, requestID, true, constants.PullQueryMsg, func() {
			s.router.QueryFailed(s.ctx.NodeID, s.ctx.ChainID, requestID)
		})
		go s.router.PullQuery(s.ctx.NodeID, s.ctx.ChainID, requestID, deadline, containerID)
	}

	// Some of the validators in [validatorIDs] may be benched. That is, they've been unresponsive
	// so we don't even bother sending messages to them. We just have them immediately fail.
	for validatorID := range validatorIDs {
		if s.timeouts.IsBenched(validatorID, s.ctx.ChainID) {
			validatorIDs.Remove(validatorID)
			// Immediately register a failure. Do so asynchronously to avoid deadlock.
			go s.router.QueryFailed(s.ctx.NodeID, s.ctx.ChainID, requestID)
		}
	}

	// Try to send the messages over the network.
	// [sentTo] are the IDs of validators who may receive the message.
	sentTo := s.sender.PullQuery(validatorIDs, s.ctx.ChainID, requestID, deadline, containerID)

	// Set timeouts so that if we don't hear back from these validators, we register a failure.
	for _, validatorID := range sentTo {
		vID := validatorID // Prevent overwrite in next loop iteration
		// TODO do we need the return values here?
		s.timeouts.RegisterRequest(vID, s.ctx.ChainID, requestID, true, constants.PullQueryMsg, func() {
			s.router.QueryFailed(vID, s.ctx.ChainID, requestID)
		})
		validatorIDs.Remove(vID)
	}

	for validatorID := range validatorIDs {
		vID := validatorID // Prevent overwrite in next loop iteration
		go s.router.QueryFailed(vID, s.ctx.ChainID, requestID)
	}
}

// Chits sends chits
func (s *Sender) Chits(validatorID ids.ShortID, requestID uint32, votes []ids.ID) {
	s.ctx.Log.Verbo("Sending Chits to validator %s. RequestID: %d. Votes: %s", validatorID, requestID, votes)
	// If [validatorID] is myself, send this message directly
	// to my own router rather than sending it over the network
	if validatorID == s.ctx.NodeID {
		go s.router.Chits(validatorID, s.ctx.ChainID, requestID, votes)
	} else {
		s.sender.Chits(validatorID, s.ctx.ChainID, requestID, votes)
	}
}

// Gossip the provided container
func (s *Sender) Gossip(containerID ids.ID, container []byte) {
	s.ctx.Log.Verbo("Gossiping %s", containerID)
	s.sender.Gossip(s.ctx.ChainID, containerID, container)
}
