// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
)

const (
	defaultCPUInterval = 15 * time.Second
)

var (
	_ Router = &ChainRouter{}
)

// ChainRouter routes incoming messages from the validator network
// to the consensus engines that the messages are intended for.
// Note that consensus engines are uniquely identified by the ID of the chain
// that they are working on.
type ChainRouter struct {
	log              logging.Logger
	lock             sync.RWMutex
	chains           map[ids.ID]*Handler
	timeouts         *timeout.Manager
	gossiper         *timer.Repeater
	intervalNotifier *timer.Repeater
	closeTimeout     time.Duration
	peers            ids.ShortSet
	criticalChains   ids.Set
	onFatal          func()
}

// Initialize the router.
//
// When this router receives an incoming message, it cancels the timeout in
// [timeouts] associated with the request that caused the incoming message, if
// applicable.
//
// This router also fires a gossip event every [gossipFrequency] to the engine,
// notifying the engine it should gossip it's accepted set.
func (cr *ChainRouter) Initialize(
	nodeID ids.ShortID,
	log logging.Logger,
	timeouts *timeout.Manager,
	gossipFrequency time.Duration,
	closeTimeout time.Duration,
	criticalChains ids.Set,
	onFatal func(),
) {
	cr.log = log
	cr.chains = make(map[ids.ID]*Handler)
	cr.timeouts = timeouts
	cr.gossiper = timer.NewRepeater(cr.Gossip, gossipFrequency)
	cr.intervalNotifier = timer.NewRepeater(cr.EndInterval, defaultCPUInterval)
	cr.closeTimeout = closeTimeout
	cr.criticalChains = criticalChains
	cr.onFatal = onFatal

	cr.peers.Add(nodeID)

	go log.RecoverAndPanic(cr.gossiper.Dispatch)
	go log.RecoverAndPanic(cr.intervalNotifier.Dispatch)
}

// Shutdown shuts down this router
func (cr *ChainRouter) Shutdown() {
	cr.log.Info("shutting down chain router")
	cr.lock.Lock()
	prevChains := cr.chains
	cr.chains = map[ids.ID]*Handler{}
	cr.lock.Unlock()

	cr.gossiper.Stop()
	cr.intervalNotifier.Stop()

	for _, chain := range prevChains {
		chain.Shutdown()
	}

	ticker := time.NewTicker(cr.closeTimeout)
	timedout := false
	for _, chain := range prevChains {
		select {
		case <-chain.closed:
		case <-ticker.C:
			timedout = true
		}
	}
	if timedout {
		cr.log.Warn("timed out while shutting down the chains")
	}
	ticker.Stop()
}

// AddChain registers the specified chain so that incoming
// messages can be routed to it
func (cr *ChainRouter) AddChain(chain *Handler) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	chainID := chain.Context().ChainID
	cr.log.Debug("registering chain %s with chain router", chainID)
	chain.toClose = func() { cr.RemoveChain(chainID) }
	cr.chains[chainID] = chain

	for validatorID := range cr.peers {
		chain.Connected(validatorID)
	}
}

// RemoveChain removes the specified chain so that incoming
// messages can't be routed to it
func (cr *ChainRouter) RemoveChain(chainID ids.ID) {
	cr.lock.Lock()
	chain, exists := cr.chains[chainID]
	if !exists {
		cr.log.Debug("can't remove unknown chain %s", chainID)
		cr.lock.Unlock()
		return
	}
	delete(cr.chains, chainID)
	cr.lock.Unlock()

	chain.Shutdown()

	ticker := time.NewTicker(cr.closeTimeout)
	select {
	case <-chain.closed:
	case <-ticker.C:
		chain.Context().Log.Warn("timed out while shutting down")
	}
	ticker.Stop()

	if cr.onFatal != nil && cr.criticalChains.Contains(chainID) {
		go cr.onFatal()
	}
}

// GetAcceptedFrontier routes an incoming GetAcceptedFrontier request from the
// validator with ID [validatorID]  to the consensus engine working on the
// chain with ID [chainID]
func (cr *ChainRouter) GetAcceptedFrontier(validatorID ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Time) {
	cr.lock.RLock()
	defer cr.lock.RUnlock()

	if chain, exists := cr.chains[chainID]; exists {
		chain.GetAcceptedFrontier(validatorID, requestID, deadline)
	} else {
		cr.log.Debug("GetAcceptedFrontier(%s, %s, %d) dropped due to unknown chain", validatorID, chainID, requestID)
	}
}

// AcceptedFrontier routes an incoming AcceptedFrontier request from the
// validator with ID [validatorID]  to the consensus engine working on the
// chain with ID [chainID]
func (cr *ChainRouter) AcceptedFrontier(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs []ids.ID) {
	cr.lock.RLock()
	defer cr.lock.RUnlock()

	if chain, exists := cr.chains[chainID]; exists {
		// Tell the timeout manager we got a response
		cr.timeouts.RegisterResponse(validatorID, chainID, requestID)
		if !chain.AcceptedFrontier(validatorID, requestID, containerIDs) {
			// We weren't able to pass the response to the chain
			chain.GetAcceptedFrontierFailed(validatorID, requestID)
		}
	} else {
		cr.log.Debug("AcceptedFrontier(%s, %s, %d, %s) dropped due to unknown chain", validatorID, chainID, requestID, containerIDs)
	}
}

// GetAcceptedFrontierFailed routes an incoming GetAcceptedFrontierFailed
// request from the validator with ID [validatorID]  to the consensus engine
// working on the chain with ID [chainID]
func (cr *ChainRouter) GetAcceptedFrontierFailed(validatorID ids.ShortID, chainID ids.ID, requestID uint32) {
	cr.lock.RLock()
	defer cr.lock.RUnlock()

	if chain, exists := cr.chains[chainID]; exists {
		chain.GetAcceptedFrontierFailed(validatorID, requestID)
	} else {
		cr.log.Debug("GetAcceptedFrontierFailed(%s, %s, %d) dropped due to unknown chain", validatorID, chainID, requestID)
	}
}

// GetAccepted routes an incoming GetAccepted request from the
// validator with ID [validatorID]  to the consensus engine working on the
// chain with ID [chainID]
func (cr *ChainRouter) GetAccepted(validatorID ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Time, containerIDs []ids.ID) {
	cr.lock.RLock()
	defer cr.lock.RUnlock()

	if chain, exists := cr.chains[chainID]; exists {
		chain.GetAccepted(validatorID, requestID, deadline, containerIDs)
	} else {
		cr.log.Debug("GetAccepted(%s, %s, %d, %s) dropped due to unknown chain", validatorID, chainID, requestID, containerIDs)
	}
}

// Accepted routes an incoming Accepted request from the validator with ID
// [validatorID] to the consensus engine working on the chain with ID
// [chainID]
func (cr *ChainRouter) Accepted(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs []ids.ID) {
	cr.lock.RLock()
	defer cr.lock.RUnlock()

	if chain, exists := cr.chains[chainID]; exists {
		// Tell the timeout manager we got a response
		cr.timeouts.RegisterResponse(validatorID, chainID, requestID)
		if !chain.Accepted(validatorID, requestID, containerIDs) {
			// We weren't able to pass the response to the chain
			chain.GetAcceptedFailed(validatorID, requestID)
		}
	} else {
		cr.log.Debug("Accepted(%s, %s, %d, %s) dropped due to unknown chain", validatorID, chainID, requestID, containerIDs)
	}
}

// GetAcceptedFailed routes an incoming GetAcceptedFailed request from the
// validator with ID [validatorID]  to the consensus engine working on the
// chain with ID [chainID]
func (cr *ChainRouter) GetAcceptedFailed(validatorID ids.ShortID, chainID ids.ID, requestID uint32) {
	cr.lock.RLock()
	defer cr.lock.RUnlock()

	if chain, exists := cr.chains[chainID]; exists {
		chain.GetAcceptedFailed(validatorID, requestID)
	} else {
		cr.log.Debug("GetAcceptedFailed(%s, %s, %d) dropped due to unknown chain", validatorID, chainID, requestID)
	}
}

// GetAncestors routes an incoming GetAncestors message from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
// The maximum number of ancestors to respond with is define in snow/engine/commong/bootstrapper.go
func (cr *ChainRouter) GetAncestors(validatorID ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Time, containerID ids.ID) {
	cr.lock.RLock()
	defer cr.lock.RUnlock()

	if chain, exists := cr.chains[chainID]; exists {
		chain.GetAncestors(validatorID, requestID, deadline, containerID)
	} else {
		cr.log.Debug("GetAncestors(%s, %s, %d) dropped due to unknown chain", validatorID, chainID, requestID)
	}
}

// MultiPut routes an incoming MultiPut message from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (cr *ChainRouter) MultiPut(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containers [][]byte) {
	cr.lock.RLock()
	defer cr.lock.RUnlock()

	// This message came in response to a GetAncestors message from this node, and when we sent that
	// message we set a timeout. Since we got a response, cancel the timeout.
	if chain, exists := cr.chains[chainID]; exists {
		// Tell the timeout manager we got a response
		cr.timeouts.RegisterResponse(validatorID, chainID, requestID)
		if !chain.MultiPut(validatorID, requestID, containers) {
			// We weren't able to pass the response to the chain
			chain.GetAncestorsFailed(validatorID, requestID)
		}
	} else {
		cr.log.Debug("MultiPut(%s, %s, %d, %d) dropped due to unknown chain", validatorID, chainID, requestID, len(containers))
	}
}

// GetAncestorsFailed routes an incoming GetAncestorsFailed message from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (cr *ChainRouter) GetAncestorsFailed(validatorID ids.ShortID, chainID ids.ID, requestID uint32) {
	cr.lock.RLock()
	defer cr.lock.RUnlock()

	if chain, exists := cr.chains[chainID]; exists {
		chain.GetAncestorsFailed(validatorID, requestID)
	} else {
		cr.log.Debug("GetAncestorsFailed(%s, %s, %d) dropped due to unknown chain", validatorID, chainID, requestID)
	}
}

// Get routes an incoming Get request from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (cr *ChainRouter) Get(validatorID ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Time, containerID ids.ID) {
	cr.lock.RLock()
	defer cr.lock.RUnlock()

	if chain, exists := cr.chains[chainID]; exists {
		chain.Get(validatorID, requestID, deadline, containerID)
	} else {
		cr.log.Debug("Get(%s, %s, %d, %s) dropped due to unknown chain", validatorID, chainID, requestID, containerID)
	}
}

// Put routes an incoming Put request from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (cr *ChainRouter) Put(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerID ids.ID, container []byte) {
	cr.lock.RLock()
	defer cr.lock.RUnlock()

	// This message came in response to a Get message from this node, and when we sent that Get
	// message we set a timeout. Since we got a response, cancel the timeout.
	chain, exists := cr.chains[chainID]
	switch {
	case exists:
		// Tell the timeout manager we got a response
		cr.timeouts.RegisterResponse(validatorID, chainID, requestID)
		if !chain.Put(validatorID, requestID, containerID, container) {
			// We weren't able to pass the response to the chain
			chain.GetFailed(validatorID, requestID)
		}
	case requestID == constants.GossipMsgRequestID:
		cr.log.Verbo("Gossiped Put(%s, %s, %d, %s) dropped due to unknown chain. Container:",
			validatorID, chainID, requestID, containerID, formatting.DumpBytes{Bytes: container},
		)
	default:
		cr.log.Debug("Put(%s, %s, %d, %s) dropped due to unknown chain", validatorID, chainID, requestID, containerID)
		cr.log.Verbo("container:\n%s", formatting.DumpBytes{Bytes: container})
	}
}

// GetFailed routes an incoming GetFailed message from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (cr *ChainRouter) GetFailed(validatorID ids.ShortID, chainID ids.ID, requestID uint32) {
	cr.lock.RLock()
	defer cr.lock.RUnlock()

	if chain, exists := cr.chains[chainID]; exists {
		chain.GetFailed(validatorID, requestID)
	} else {
		cr.log.Debug("GetFailed(%s, %s, %d) dropped due to unknown chain", validatorID, chainID, requestID)
	}
}

// PushQuery routes an incoming PushQuery request from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (cr *ChainRouter) PushQuery(validatorID ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Time, containerID ids.ID, container []byte) {
	cr.lock.RLock()
	defer cr.lock.RUnlock()

	if chain, exists := cr.chains[chainID]; exists {
		chain.PushQuery(validatorID, requestID, deadline, containerID, container)
	} else {
		cr.log.Debug("PushQuery(%s, %s, %d, %s) dropped due to unknown chain", validatorID, chainID, requestID, containerID)
		cr.log.Verbo("container:\n%s", formatting.DumpBytes{Bytes: container})
	}
}

// PullQuery routes an incoming PullQuery request from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (cr *ChainRouter) PullQuery(validatorID ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Time, containerID ids.ID) {
	cr.lock.RLock()
	defer cr.lock.RUnlock()

	if chain, exists := cr.chains[chainID]; exists {
		chain.PullQuery(validatorID, requestID, deadline, containerID)
	} else {
		cr.log.Debug("PullQuery(%s, %s, %d, %s) dropped due to unknown chain", validatorID, chainID, requestID, containerID)
	}
}

// Chits routes an incoming Chits message from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (cr *ChainRouter) Chits(validatorID ids.ShortID, chainID ids.ID, requestID uint32, votes []ids.ID) {
	cr.lock.RLock()
	defer cr.lock.RUnlock()

	// Cancel timeout we set when sent the message asking for these Chits
	if chain, exists := cr.chains[chainID]; exists {
		// Tell the timeout manager we got a response
		cr.timeouts.RegisterResponse(validatorID, chainID, requestID)
		if !chain.Chits(validatorID, requestID, votes) {
			// We weren't able to pass the response to the chain
			chain.QueryFailed(validatorID, requestID)
		}
	} else {
		cr.log.Debug("Chits(%s, %s, %d, %s) dropped due to unknown chain", validatorID, chainID, requestID, votes)
	}
}

// QueryFailed routes an incoming QueryFailed message from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (cr *ChainRouter) QueryFailed(validatorID ids.ShortID, chainID ids.ID, requestID uint32) {
	cr.lock.RLock()
	defer cr.lock.RUnlock()

	if chain, exists := cr.chains[chainID]; exists {
		chain.QueryFailed(validatorID, requestID)
	} else {
		cr.log.Debug("QueryFailed(%s, %s, %d) dropped due to unknown chain", validatorID, chainID, requestID)
	}
}

// Connected routes an incoming notification that a validator was just connected
func (cr *ChainRouter) Connected(validatorID ids.ShortID) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	cr.peers.Add(validatorID)
	for _, chain := range cr.chains {
		chain.Connected(validatorID)
	}
}

// Disconnected routes an incoming notification that a validator was connected
func (cr *ChainRouter) Disconnected(validatorID ids.ShortID) {
	cr.lock.Lock()
	defer cr.lock.Unlock()

	cr.peers.Remove(validatorID)
	for _, chain := range cr.chains {
		chain.Disconnected(validatorID)
	}
}

// Gossip accepted containers
func (cr *ChainRouter) Gossip() {
	cr.lock.RLock()
	defer cr.lock.RUnlock()

	for _, chain := range cr.chains {
		chain.Gossip()
	}
}

// EndInterval notifies the chains that the current CPU interval has ended
func (cr *ChainRouter) EndInterval() {
	cr.lock.RLock()
	defer cr.lock.RUnlock()

	for _, chain := range cr.chains {
		chain.endInterval()
	}
}
