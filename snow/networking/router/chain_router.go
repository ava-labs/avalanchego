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
	defaultCPUInterval = 5 * time.Second
)

// ChainRouter routes incoming messages from the validator network
// to the consensus engines that the messages are intended for.
// Note that consensus engines are uniquely identified by the ID of the chain
// that they are working on.
type ChainRouter struct {
	log              logging.Logger
	lock             sync.RWMutex
	chains           map[[32]byte]*Handler
	timeouts         *timeout.Manager
	gossiper         *timer.Repeater
	intervalNotifier *timer.Repeater
	closeTimeout     time.Duration
}

// Initialize the router.
//
// When this router receives an incoming message, it cancels the timeout in
// [timeouts] associated with the request that caused the incoming message, if
// applicable.
//
// This router also fires a gossip event every [gossipFrequency] to the engine,
// notifying the engine it should gossip it's accepted set.
func (sr *ChainRouter) Initialize(
	log logging.Logger,
	timeouts *timeout.Manager,
	gossipFrequency time.Duration,
	closeTimeout time.Duration,
) {
	sr.log = log
	sr.chains = make(map[[32]byte]*Handler)
	sr.timeouts = timeouts
	sr.gossiper = timer.NewRepeater(sr.Gossip, gossipFrequency)
	sr.intervalNotifier = timer.NewRepeater(sr.EndInterval, defaultCPUInterval)
	sr.closeTimeout = closeTimeout

	go log.RecoverAndPanic(sr.gossiper.Dispatch)
	go log.RecoverAndPanic(sr.intervalNotifier.Dispatch)
}

// AddChain registers the specified chain so that incoming
// messages can be routed to it
func (sr *ChainRouter) AddChain(chain *Handler) {
	sr.lock.Lock()
	defer sr.lock.Unlock()

	chainID := chain.Context().ChainID
	sr.log.Debug("registering chain %s with chain router", chainID)
	chain.toClose = func() { sr.RemoveChain(chainID) }
	sr.chains[chainID.Key()] = chain
}

// RemoveChain removes the specified chain so that incoming
// messages can't be routed to it
func (sr *ChainRouter) RemoveChain(chainID ids.ID) {
	sr.lock.Lock()
	chain, exists := sr.chains[chainID.Key()]
	if !exists {
		sr.log.Debug("can't remove unknown chain %s", chainID)
		sr.lock.Unlock()
		return
	}
	delete(sr.chains, chainID.Key())
	sr.lock.Unlock()

	chain.Shutdown()

	ticker := time.NewTicker(sr.closeTimeout)
	select {
	case <-chain.closed:
	case <-ticker.C:
		chain.Context().Log.Warn("timed out while shutting down")
	}
	ticker.Stop()
}

// GetAcceptedFrontier routes an incoming GetAcceptedFrontier request from the
// validator with ID [validatorID]  to the consensus engine working on the
// chain with ID [chainID]
func (sr *ChainRouter) GetAcceptedFrontier(validatorID ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Time) {
	sr.lock.RLock()
	defer sr.lock.RUnlock()

	if chain, exists := sr.chains[chainID.Key()]; exists {
		chain.GetAcceptedFrontier(validatorID, requestID, deadline)
	} else {
		sr.log.Debug("GetAcceptedFrontier(%s, %s, %d) dropped due to unknown chain", validatorID, chainID, requestID)
	}
}

// AcceptedFrontier routes an incoming AcceptedFrontier request from the
// validator with ID [validatorID]  to the consensus engine working on the
// chain with ID [chainID]
func (sr *ChainRouter) AcceptedFrontier(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs ids.Set) {
	sr.lock.RLock()
	defer sr.lock.RUnlock()

	if chain, exists := sr.chains[chainID.Key()]; exists {
		if chain.AcceptedFrontier(validatorID, requestID, containerIDs) {
			sr.timeouts.Cancel(validatorID, chainID, requestID)
		}
	} else {
		sr.log.Debug("AcceptedFrontier(%s, %s, %d, %s) dropped due to unknown chain", validatorID, chainID, requestID, containerIDs)
	}
}

// GetAcceptedFrontierFailed routes an incoming GetAcceptedFrontierFailed
// request from the validator with ID [validatorID]  to the consensus engine
// working on the chain with ID [chainID]
func (sr *ChainRouter) GetAcceptedFrontierFailed(validatorID ids.ShortID, chainID ids.ID, requestID uint32) {
	sr.lock.RLock()
	defer sr.lock.RUnlock()

	sr.timeouts.Cancel(validatorID, chainID, requestID)
	if chain, exists := sr.chains[chainID.Key()]; exists {
		chain.GetAcceptedFrontierFailed(validatorID, requestID)
	} else {
		sr.log.Error("GetAcceptedFrontierFailed(%s, %s, %d) dropped due to unknown chain", validatorID, chainID, requestID)
	}
}

// GetAccepted routes an incoming GetAccepted request from the
// validator with ID [validatorID]  to the consensus engine working on the
// chain with ID [chainID]
func (sr *ChainRouter) GetAccepted(validatorID ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Time, containerIDs ids.Set) {
	sr.lock.RLock()
	defer sr.lock.RUnlock()

	if chain, exists := sr.chains[chainID.Key()]; exists {
		chain.GetAccepted(validatorID, requestID, deadline, containerIDs)
	} else {
		sr.log.Debug("GetAccepted(%s, %s, %d, %s) dropped due to unknown chain", validatorID, chainID, requestID, containerIDs)
	}
}

// Accepted routes an incoming Accepted request from the validator with ID
// [validatorID]  to the consensus engine working on the chain with ID
// [chainID]
func (sr *ChainRouter) Accepted(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs ids.Set) {
	sr.lock.RLock()
	defer sr.lock.RUnlock()

	if chain, exists := sr.chains[chainID.Key()]; exists {
		if chain.Accepted(validatorID, requestID, containerIDs) {
			sr.timeouts.Cancel(validatorID, chainID, requestID)
		}
	} else {
		sr.log.Debug("Accepted(%s, %s, %d, %s) dropped due to unknown chain", validatorID, chainID, requestID, containerIDs)
	}
}

// GetAcceptedFailed routes an incoming GetAcceptedFailed request from the
// validator with ID [validatorID]  to the consensus engine working on the
// chain with ID [chainID]
func (sr *ChainRouter) GetAcceptedFailed(validatorID ids.ShortID, chainID ids.ID, requestID uint32) {
	sr.lock.RLock()
	defer sr.lock.RUnlock()

	sr.timeouts.Cancel(validatorID, chainID, requestID)
	if chain, exists := sr.chains[chainID.Key()]; exists {
		chain.GetAcceptedFailed(validatorID, requestID)
	} else {
		sr.log.Error("GetAcceptedFailed(%s, %s, %d) dropped due to unknown chain", validatorID, chainID, requestID)
	}
}

// GetAncestors routes an incoming GetAncestors message from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
// The maximum number of ancestors to respond with is define in snow/engine/commong/bootstrapper.go
func (sr *ChainRouter) GetAncestors(validatorID ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Time, containerID ids.ID) {
	sr.lock.RLock()
	defer sr.lock.RUnlock()

	if chain, exists := sr.chains[chainID.Key()]; exists {
		chain.GetAncestors(validatorID, requestID, deadline, containerID)
	} else {
		sr.log.Debug("GetAncestors(%s, %s, %d) dropped due to unknown chain", validatorID, chainID, requestID)
	}
}

// MultiPut routes an incoming MultiPut message from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (sr *ChainRouter) MultiPut(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containers [][]byte) {
	sr.lock.RLock()
	defer sr.lock.RUnlock()

	// This message came in response to a GetAncestors message from this node, and when we sent that
	// message we set a timeout. Since we got a response, cancel the timeout.
	if chain, exists := sr.chains[chainID.Key()]; exists {
		if chain.MultiPut(validatorID, requestID, containers) {
			sr.timeouts.Cancel(validatorID, chainID, requestID)
		}
	} else {
		sr.log.Debug("MultiPut(%s, %s, %d, %d) dropped due to unknown chain", validatorID, chainID, requestID, len(containers))
	}
}

// GetAncestorsFailed routes an incoming GetAncestorsFailed message from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (sr *ChainRouter) GetAncestorsFailed(validatorID ids.ShortID, chainID ids.ID, requestID uint32) {
	sr.lock.RLock()
	defer sr.lock.RUnlock()

	sr.timeouts.Cancel(validatorID, chainID, requestID)
	if chain, exists := sr.chains[chainID.Key()]; exists {
		chain.GetAncestorsFailed(validatorID, requestID)
	} else {
		sr.log.Error("GetAncestorsFailed(%s, %s, %d) dropped due to unknown chain", validatorID, chainID, requestID)
	}
}

// Get routes an incoming Get request from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (sr *ChainRouter) Get(validatorID ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Time, containerID ids.ID) {
	sr.lock.RLock()
	defer sr.lock.RUnlock()

	if chain, exists := sr.chains[chainID.Key()]; exists {
		chain.Get(validatorID, requestID, deadline, containerID)
	} else {
		sr.log.Debug("Get(%s, %s, %d, %s) dropped due to unknown chain", validatorID, chainID, requestID, containerID)
	}
}

// Put routes an incoming Put request from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (sr *ChainRouter) Put(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerID ids.ID, container []byte) {
	sr.lock.RLock()
	defer sr.lock.RUnlock()

	// This message came in response to a Get message from this node, and when we sent that Get
	// message we set a timeout. Since we got a response, cancel the timeout.
	if chain, exists := sr.chains[chainID.Key()]; exists {
		if chain.Put(validatorID, requestID, containerID, container) {
			sr.timeouts.Cancel(validatorID, chainID, requestID)
		}
	} else if requestID == constants.GossipMsgRequestID {
		sr.log.Verbo("Gossiped Put(%s, %s, %d, %s) dropped due to unknown chain. Container:",
			validatorID, chainID, requestID, containerID, formatting.DumpBytes{Bytes: container},
		)
	} else {
		sr.log.Debug("Put(%s, %s, %d, %s) dropped due to unknown chain", validatorID, chainID, requestID, containerID)
		sr.log.Verbo("container:\n%s", formatting.DumpBytes{Bytes: container})
	}
}

// GetFailed routes an incoming GetFailed message from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (sr *ChainRouter) GetFailed(validatorID ids.ShortID, chainID ids.ID, requestID uint32) {
	sr.lock.RLock()
	defer sr.lock.RUnlock()

	sr.timeouts.Cancel(validatorID, chainID, requestID)
	if chain, exists := sr.chains[chainID.Key()]; exists {
		chain.GetFailed(validatorID, requestID)
	} else {
		sr.log.Error("GetFailed(%s, %s, %d) dropped due to unknown chain", validatorID, chainID, requestID)
	}
}

// PushQuery routes an incoming PushQuery request from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (sr *ChainRouter) PushQuery(validatorID ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Time, containerID ids.ID, container []byte) {
	sr.lock.RLock()
	defer sr.lock.RUnlock()

	if chain, exists := sr.chains[chainID.Key()]; exists {
		chain.PushQuery(validatorID, requestID, deadline, containerID, container)
	} else {
		sr.log.Debug("PushQuery(%s, %s, %d, %s) dropped due to unknown chain", validatorID, chainID, requestID, containerID)
		sr.log.Verbo("container:\n%s", formatting.DumpBytes{Bytes: container})
	}
}

// PullQuery routes an incoming PullQuery request from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (sr *ChainRouter) PullQuery(validatorID ids.ShortID, chainID ids.ID, requestID uint32, deadline time.Time, containerID ids.ID) {
	sr.lock.RLock()
	defer sr.lock.RUnlock()

	if chain, exists := sr.chains[chainID.Key()]; exists {
		chain.PullQuery(validatorID, requestID, deadline, containerID)
	} else {
		sr.log.Debug("PullQuery(%s, %s, %d, %s) dropped due to unknown chain", validatorID, chainID, requestID, containerID)
	}
}

// Chits routes an incoming Chits message from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (sr *ChainRouter) Chits(validatorID ids.ShortID, chainID ids.ID, requestID uint32, votes ids.Set) {
	sr.lock.RLock()
	defer sr.lock.RUnlock()

	// Cancel timeout we set when sent the message asking for these Chits
	if chain, exists := sr.chains[chainID.Key()]; exists {
		if chain.Chits(validatorID, requestID, votes) {
			sr.timeouts.Cancel(validatorID, chainID, requestID)
		}
	} else {
		sr.log.Debug("Chits(%s, %s, %d, %s) dropped due to unknown chain", validatorID, chainID, requestID, votes)
	}
}

// QueryFailed routes an incoming QueryFailed message from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (sr *ChainRouter) QueryFailed(validatorID ids.ShortID, chainID ids.ID, requestID uint32) {
	sr.lock.RLock()
	defer sr.lock.RUnlock()

	sr.timeouts.Cancel(validatorID, chainID, requestID)
	if chain, exists := sr.chains[chainID.Key()]; exists {
		chain.QueryFailed(validatorID, requestID)
	} else {
		sr.log.Error("QueryFailed(%s, %s, %d) dropped due to unknown chain", validatorID, chainID, requestID)
	}
}

// Shutdown shuts down this router
func (sr *ChainRouter) Shutdown() {
	sr.lock.Lock()
	prevChains := sr.chains
	sr.chains = map[[32]byte]*Handler{}
	sr.lock.Unlock()

	sr.gossiper.Stop()
	sr.intervalNotifier.Stop()

	for _, chain := range prevChains {
		chain.Shutdown()
	}

	ticker := time.NewTicker(sr.closeTimeout)
	timedout := false
	for _, chain := range prevChains {
		select {
		case <-chain.closed:
		case <-ticker.C:
			timedout = true
		}
	}
	if timedout {
		sr.log.Warn("timed out while shutting down the chains")
	}
	ticker.Stop()
}

// Gossip accepted containers
func (sr *ChainRouter) Gossip() {
	sr.lock.Lock()
	defer sr.lock.Unlock()

	for _, chain := range sr.chains {
		chain.Gossip()
	}
}

// EndInterval notifies the chains that the current CPU interval has ended
func (sr *ChainRouter) EndInterval() {
	sr.lock.Lock()
	defer sr.lock.Unlock()

	for _, chain := range sr.chains {
		chain.endInterval()
	}
}
