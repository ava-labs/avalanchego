// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"sync"
	"time"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/networking/timeout"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/utils/timer"
)

// ChainRouter routes incoming messages from the validator network
// to the consensus engines that the messages are intended for.
// Note that consensus engines are uniquely identified by the ID of the chain
// that they are working on.
type ChainRouter struct {
	log          logging.Logger
	lock         sync.RWMutex
	chains       map[[32]byte]*Handler
	timeouts     *timeout.Manager
	gossiper     *timer.Repeater
	closeTimeout time.Duration
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
	sr.closeTimeout = closeTimeout

	go log.RecoverAndPanic(sr.gossiper.Dispatch)
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
	sr.lock.RLock()
	chain, exists := sr.chains[chainID.Key()]
	sr.lock.RUnlock()

	if !exists {
		sr.log.Debug("message referenced a chain, %s, this node doesn't validate", chainID)
		return
	}

	chain.Shutdown()
	close(chain.msgs)

	ticker := time.NewTicker(sr.closeTimeout)
	select {
	case _, _ = <-chain.closed:
	case <-ticker.C:
		chain.Context().Log.Warn("timed out while shutting down")
	}
	ticker.Stop()

	sr.lock.Lock()
	delete(sr.chains, chainID.Key())
	sr.lock.Unlock()
}

// GetAcceptedFrontier routes an incoming GetAcceptedFrontier request from the
// validator with ID [validatorID]  to the consensus engine working on the
// chain with ID [chainID]
func (sr *ChainRouter) GetAcceptedFrontier(validatorID ids.ShortID, chainID ids.ID, requestID uint32) {
	sr.lock.RLock()
	chain, exists := sr.chains[chainID.Key()]
	sr.lock.RUnlock()

	if exists {
		chain.GetAcceptedFrontier(validatorID, requestID)
	} else {
		sr.log.Debug("message referenced a chain, %s, this node doesn't validate", chainID)
	}
}

// AcceptedFrontier routes an incoming AcceptedFrontier request from the
// validator with ID [validatorID]  to the consensus engine working on the
// chain with ID [chainID]
func (sr *ChainRouter) AcceptedFrontier(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs ids.Set) {
	sr.lock.RLock()
	sr.timeouts.Cancel(validatorID, chainID, requestID)
	chain, exists := sr.chains[chainID.Key()]
	sr.lock.RUnlock()

	if exists {
		chain.AcceptedFrontier(validatorID, requestID, containerIDs)
	} else {
		sr.log.Debug("message referenced a chain, %s, this node doesn't validate", chainID)
	}
}

// GetAcceptedFrontierFailed routes an incoming GetAcceptedFrontierFailed
// request from the validator with ID [validatorID]  to the consensus engine
// working on the chain with ID [chainID]
func (sr *ChainRouter) GetAcceptedFrontierFailed(validatorID ids.ShortID, chainID ids.ID, requestID uint32) {
	sr.lock.RLock()
	sr.timeouts.Cancel(validatorID, chainID, requestID)
	chain, exists := sr.chains[chainID.Key()]
	sr.lock.RUnlock()

	if exists {
		chain.GetAcceptedFrontierFailed(validatorID, requestID)
	} else {
		sr.log.Debug("message referenced a chain, %s, this node doesn't validate", chainID)
	}
}

// GetAccepted routes an incoming GetAccepted request from the
// validator with ID [validatorID]  to the consensus engine working on the
// chain with ID [chainID]
func (sr *ChainRouter) GetAccepted(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs ids.Set) {
	sr.lock.RLock()
	chain, exists := sr.chains[chainID.Key()]
	sr.lock.RUnlock()

	if exists {
		chain.GetAccepted(validatorID, requestID, containerIDs)
	} else {
		sr.log.Debug("message referenced a chain, %s, this node doesn't validate", chainID)
	}
}

// Accepted routes an incoming Accepted request from the validator with ID
// [validatorID]  to the consensus engine working on the chain with ID
// [chainID]
func (sr *ChainRouter) Accepted(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs ids.Set) {
	sr.lock.RLock()
	sr.timeouts.Cancel(validatorID, chainID, requestID)
	chain, exists := sr.chains[chainID.Key()]
	sr.lock.RUnlock()

	if exists {
		chain.Accepted(validatorID, requestID, containerIDs)
	} else {
		sr.log.Debug("message referenced a chain, %s, this node doesn't validate", chainID)
	}
}

// GetAcceptedFailed routes an incoming GetAcceptedFailed request from the
// validator with ID [validatorID]  to the consensus engine working on the
// chain with ID [chainID]
func (sr *ChainRouter) GetAcceptedFailed(validatorID ids.ShortID, chainID ids.ID, requestID uint32) {
	sr.lock.RLock()
	sr.timeouts.Cancel(validatorID, chainID, requestID)
	chain, exists := sr.chains[chainID.Key()]
	sr.lock.RUnlock()

	if exists {
		chain.GetAcceptedFailed(validatorID, requestID)
	} else {
		sr.log.Debug("message referenced a chain, %s, this node doesn't validate", chainID)
	}
}

// Get routes an incoming Get request from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (sr *ChainRouter) Get(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerID ids.ID) {
	sr.lock.RLock()
	chain, exists := sr.chains[chainID.Key()]
	sr.lock.RUnlock()

	if exists {
		chain.Get(validatorID, requestID, containerID)
	} else {
		sr.log.Debug("message referenced a chain, %s, this node doesn't validate", chainID)
	}
}

// GetAncestors routes an incoming GetAncestors message from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
// The maximum number of ancestors to respond with is define in snow/engine/commong/bootstrapper.go
func (sr *ChainRouter) GetAncestors(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerID ids.ID) {
	sr.lock.RLock()
	chain, exists := sr.chains[chainID.Key()]
	sr.lock.RUnlock()

	if exists {
		chain.GetAncestors(validatorID, requestID, containerID)
	} else {
		sr.log.Debug("message referenced a chain, %s, this node doesn't validate", chainID)
	}
}

// Put routes an incoming Put request from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (sr *ChainRouter) Put(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerID ids.ID, container []byte) {
	sr.lock.RLock()
	sr.timeouts.Cancel(validatorID, chainID, requestID)
	chain, exists := sr.chains[chainID.Key()]
	sr.lock.RUnlock()

	// This message came in response to a Get message from this node, and when we sent that Get
	// message we set a timeout. Since we got a response, cancel the timeout.
	if exists {
		chain.Put(validatorID, requestID, containerID, container)
	} else {
		sr.log.Debug("message referenced a chain, %s, this node doesn't validate", chainID)
	}
}

// MultiPut routes an incoming MultiPut message from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (sr *ChainRouter) MultiPut(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containers [][]byte) {
	sr.lock.RLock()
	sr.timeouts.Cancel(validatorID, chainID, requestID)
	chain, exists := sr.chains[chainID.Key()]
	sr.lock.RUnlock()

	// This message came in response to a GetAncestors message from this node, and when we sent that
	// message we set a timeout. Since we got a response, cancel the timeout.
	if exists {
		chain.MultiPut(validatorID, requestID, containers)
	} else {
		sr.log.Debug("message referenced a chain, %s, this node doesn't validate", chainID)
	}
}

// GetFailed routes an incoming GetFailed message from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (sr *ChainRouter) GetFailed(validatorID ids.ShortID, chainID ids.ID, requestID uint32) {
	sr.lock.RLock()
	sr.timeouts.Cancel(validatorID, chainID, requestID)
	chain, exists := sr.chains[chainID.Key()]
	sr.lock.RUnlock()

	if exists {
		chain.GetFailed(validatorID, requestID)
	} else {
		sr.log.Debug("message referenced a chain, %s, this node doesn't validate", chainID)
	}
}

// GetAncestorsFailed routes an incoming GetAncestorsFailed message from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (sr *ChainRouter) GetAncestorsFailed(validatorID ids.ShortID, chainID ids.ID, requestID uint32) {
	sr.lock.RLock()
	sr.timeouts.Cancel(validatorID, chainID, requestID)
	chain, exists := sr.chains[chainID.Key()]
	sr.lock.RUnlock()

	if exists {
		chain.GetAncestorsFailed(validatorID, requestID)
	} else {
		sr.log.Debug("message referenced a chain, %s, this node doesn't validate", chainID)
	}
}

// PushQuery routes an incoming PushQuery request from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (sr *ChainRouter) PushQuery(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerID ids.ID, container []byte) {
	sr.lock.RLock()
	chain, exists := sr.chains[chainID.Key()]
	sr.lock.RUnlock()

	if exists {
		chain.PushQuery(validatorID, requestID, containerID, container)
	} else {
		sr.log.Debug("message referenced a chain, %s, this node doesn't validate", chainID)
	}
}

// PullQuery routes an incoming PullQuery request from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (sr *ChainRouter) PullQuery(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerID ids.ID) {
	sr.lock.RLock()
	chain, exists := sr.chains[chainID.Key()]
	sr.lock.RUnlock()

	if exists {
		chain.PullQuery(validatorID, requestID, containerID)
	} else {
		sr.log.Debug("message referenced a chain, %s, this node doesn't validate", chainID)
	}
}

// Chits routes an incoming Chits message from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (sr *ChainRouter) Chits(validatorID ids.ShortID, chainID ids.ID, requestID uint32, votes ids.Set) {
	sr.lock.RLock()
	sr.timeouts.Cancel(validatorID, chainID, requestID)
	chain, exists := sr.chains[chainID.Key()]
	sr.lock.RUnlock()

	// Cancel timeout we set when sent the message asking for these Chits

	if exists {
		chain.Chits(validatorID, requestID, votes)
	} else {
		sr.log.Debug("message referenced a chain, %s, this node doesn't validate", chainID)
	}
}

// QueryFailed routes an incoming QueryFailed message from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (sr *ChainRouter) QueryFailed(validatorID ids.ShortID, chainID ids.ID, requestID uint32) {
	sr.lock.RLock()
	sr.timeouts.Cancel(validatorID, chainID, requestID)
	chain, exists := sr.chains[chainID.Key()]
	sr.lock.RUnlock()

	if exists {
		chain.QueryFailed(validatorID, requestID)
	} else {
		sr.log.Debug("message referenced a chain, %s, this node doesn't validate", chainID)
	}
}

// Shutdown shuts down this router
func (sr *ChainRouter) Shutdown() {
	sr.lock.Lock()
	prevChains := sr.chains
	sr.chains = map[[32]byte]*Handler{}
	sr.lock.Unlock()

	for _, chain := range prevChains {
		chain.Shutdown()
		close(chain.msgs)
	}

	ticker := time.NewTicker(sr.closeTimeout)
	timedout := false
	for _, chain := range prevChains {
		select {
		case _, _ = <-chain.closed:
		case <-ticker.C:
			timedout = true
		}
	}
	if timedout {
		sr.log.Warn("timed out while shutting down the chains")
	}
	ticker.Stop()
	sr.gossiper.Stop()
}

// Gossip accepted containers
func (sr *ChainRouter) Gossip() {
	sr.lock.Lock()
	chains := []*Handler{}
	for _, chain := range sr.chains {
		chains = append(chains, chain)
	}
	sr.lock.Unlock()

	for _, chain := range chains {
		chain.Gossip()
	}
}
