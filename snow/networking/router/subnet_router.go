// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"sync"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/networking/handler"
	"github.com/ava-labs/gecko/snow/networking/timeout"
	"github.com/ava-labs/gecko/utils/logging"
)

// ChainRouter routes incoming messages from the validator network
// to the consensus engines that the messages are intended for.
// Note that consensus engines are uniquely identified by the ID of the chain
// that they are working on.
type ChainRouter struct {
	log      logging.Logger
	lock     sync.RWMutex
	chains   map[[32]byte]*handler.Handler
	timeouts *timeout.Manager
}

// Initialize the router
// When this router receives an incoming message, it cancels the timeout in [timeouts]
// associated with the request that caused the incoming message, if applicable
func (sr *ChainRouter) Initialize(log logging.Logger, timeouts *timeout.Manager) {
	sr.log = log
	sr.chains = make(map[[32]byte]*handler.Handler)
	sr.timeouts = timeouts
}

// AddChain registers the specified chain so that incoming
// messages can be routed to it
func (sr *ChainRouter) AddChain(chain *handler.Handler) {
	sr.lock.Lock()
	defer sr.lock.Unlock()

	chainID := chain.Context().ChainID
	sr.log.Debug("Adding %s to the routing table", chainID)
	sr.chains[chainID.Key()] = chain
}

// RemoveChain removes the specified chain so that incoming
// messages can't be routed to it
func (sr *ChainRouter) RemoveChain(chainID ids.ID) {
	sr.lock.Lock()
	defer sr.lock.Unlock()

	if chain, exists := sr.chains[chainID.Key()]; exists {
		chain.Shutdown()
		delete(sr.chains, chainID.Key())
	} else {
		sr.log.Warn("Message referenced a chain, %s, this validator is not validating", chainID)
	}
}

// GetAcceptedFrontier routes an incoming GetAcceptedFrontier request from the
// validator with ID [validatorID]  to the consensus engine working on the
// chain with ID [chainID]
func (sr *ChainRouter) GetAcceptedFrontier(validatorID ids.ShortID, chainID ids.ID, requestID uint32) {
	sr.lock.RLock()
	defer sr.lock.RUnlock()

	if chain, exists := sr.chains[chainID.Key()]; exists {
		chain.GetAcceptedFrontier(validatorID, requestID)
	} else {
		sr.log.Warn("Message referenced a chain, %s, this validator is not validating", chainID)
	}
}

// AcceptedFrontier routes an incoming AcceptedFrontier request from the
// validator with ID [validatorID]  to the consensus engine working on the
// chain with ID [chainID]
func (sr *ChainRouter) AcceptedFrontier(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs ids.Set) {
	sr.lock.RLock()
	defer sr.lock.RUnlock()

	sr.timeouts.Cancel(validatorID, chainID, requestID)
	if chain, exists := sr.chains[chainID.Key()]; exists {
		chain.AcceptedFrontier(validatorID, requestID, containerIDs)
	} else {
		sr.log.Warn("Message referenced a chain, %s, this validator is not validating", chainID)
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
		sr.log.Warn("Message referenced a chain, %s, this validator is not validating", chainID)
	}
}

// GetAccepted routes an incoming GetAccepted request from the
// validator with ID [validatorID]  to the consensus engine working on the
// chain with ID [chainID]
func (sr *ChainRouter) GetAccepted(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs ids.Set) {
	sr.lock.RLock()
	defer sr.lock.RUnlock()

	if chain, exists := sr.chains[chainID.Key()]; exists {
		chain.GetAccepted(validatorID, requestID, containerIDs)
	} else {
		sr.log.Warn("Message referenced a chain, %s, this validator is not validating", chainID)
	}
}

// Accepted routes an incoming Accepted request from the validator with ID
// [validatorID]  to the consensus engine working on the chain with ID
// [chainID]
func (sr *ChainRouter) Accepted(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerIDs ids.Set) {
	sr.lock.RLock()
	defer sr.lock.RUnlock()

	sr.timeouts.Cancel(validatorID, chainID, requestID)
	if chain, exists := sr.chains[chainID.Key()]; exists {
		chain.Accepted(validatorID, requestID, containerIDs)
	} else {
		sr.log.Warn("Message referenced a chain, %s, this validator is not validating", chainID)
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
		sr.log.Warn("Message referenced a chain, %s, this validator is not validating", chainID)
	}
}

// Get routes an incoming Get request from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (sr *ChainRouter) Get(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerID ids.ID) {
	sr.lock.RLock()
	defer sr.lock.RUnlock()

	if chain, exists := sr.chains[chainID.Key()]; exists {
		chain.Get(validatorID, requestID, containerID)
	} else {
		sr.log.Warn("Message referenced a chain, %s, this validator is not validating", chainID)
	}
}

// Put routes an incoming Put request from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (sr *ChainRouter) Put(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerID ids.ID, container []byte) {
	sr.lock.RLock()
	defer sr.lock.RUnlock()

	// This message came in response to a Get message from this node, and when we sent that Get
	// message we set a timeout. Since we got a response, cancel the timeout.
	sr.timeouts.Cancel(validatorID, chainID, requestID)
	if chain, exists := sr.chains[chainID.Key()]; exists {
		chain.Put(validatorID, requestID, containerID, container)
	} else {
		sr.log.Warn("Message referenced a chain, %s, this validator is not validating", chainID)
	}
}

// GetFailed routes an incoming GetFailed message from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (sr *ChainRouter) GetFailed(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerID ids.ID) {
	sr.lock.RLock()
	defer sr.lock.RUnlock()

	sr.timeouts.Cancel(validatorID, chainID, requestID)
	if chain, exists := sr.chains[chainID.Key()]; exists {
		chain.GetFailed(validatorID, requestID, containerID)
	} else {
		sr.log.Warn("Message referenced a chain, %s, this validator is not validating", chainID)
	}
}

// PushQuery routes an incoming PushQuery request from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (sr *ChainRouter) PushQuery(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerID ids.ID, container []byte) {
	sr.lock.RLock()
	defer sr.lock.RUnlock()

	if chain, exists := sr.chains[chainID.Key()]; exists {
		chain.PushQuery(validatorID, requestID, containerID, container)
	} else {
		sr.log.Warn("Message referenced a chain, %s, this validator is not validating", chainID)
	}
}

// PullQuery routes an incoming PullQuery request from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (sr *ChainRouter) PullQuery(validatorID ids.ShortID, chainID ids.ID, requestID uint32, containerID ids.ID) {
	sr.lock.RLock()
	defer sr.lock.RUnlock()

	if chain, exists := sr.chains[chainID.Key()]; exists {
		chain.PullQuery(validatorID, requestID, containerID)
	} else {
		sr.log.Warn("Message referenced a chain, %s, this validator is not validating", chainID)
	}
}

// Chits routes an incoming Chits message from the validator with ID [validatorID]
// to the consensus engine working on the chain with ID [chainID]
func (sr *ChainRouter) Chits(validatorID ids.ShortID, chainID ids.ID, requestID uint32, votes ids.Set) {
	sr.lock.RLock()
	defer sr.lock.RUnlock()

	// Cancel timeout we set when sent the message asking for these Chits
	sr.timeouts.Cancel(validatorID, chainID, requestID)
	if chain, exists := sr.chains[chainID.Key()]; exists {
		chain.Chits(validatorID, requestID, votes)
	} else {
		sr.log.Warn("Message referenced a chain, %s, this validator is not validating", chainID)
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
		sr.log.Warn("Message referenced a chain, %s, this validator is not validating", chainID)
	}
}

// Shutdown shuts down this router
func (sr *ChainRouter) Shutdown() {
	sr.lock.RLock()
	defer sr.lock.RUnlock()

	sr.shutdown()
}

func (sr *ChainRouter) shutdown() {
	for _, chain := range sr.chains {
		chain.Shutdown()
	}
}
