// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snow

import (
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var (
	_ Acceptor = acceptorWrapper{}

	_ AcceptorGroup = (*acceptorGroup)(nil)
)

// Acceptor is implemented when a struct is monitoring if a message is accepted
type Acceptor interface {
	// Accept must be called before [containerID] is committed to the VM as
	// accepted.
	//
	// If the returned error is non-nil, the chain associated with [ctx] should
	// shut down and not commit [container] or any other container to its
	// database as accepted.
	Accept(ctx *ConsensusContext, containerID ids.ID, container []byte) error
}

type acceptorWrapper struct {
	Acceptor

	// If true and Accept returns an error, the chain this callback corresponds
	// to will stop.
	dieOnError bool
}

type AcceptorGroup interface {
	// Calling Accept() calls all of the registered acceptors for the relevant
	// chain.
	Acceptor

	// RegisterAcceptor causes [acceptor] to be called every time an operation
	// is accepted on chain [chainID].
	// If [dieOnError], chain [chainID] stops if Accept returns a non-nil error.
	RegisterAcceptor(chainID ids.ID, acceptorName string, acceptor Acceptor, dieOnError bool) error

	// DeregisterAcceptor removes an acceptor from the group.
	DeregisterAcceptor(chainID ids.ID, acceptorName string) error
}

type acceptorGroup struct {
	log logging.Logger

	lock sync.RWMutex
	// Chain ID --> Acceptor Name --> Acceptor
	acceptors map[ids.ID]map[string]acceptorWrapper
}

func NewAcceptorGroup(log logging.Logger) AcceptorGroup {
	return &acceptorGroup{
		log:       log,
		acceptors: make(map[ids.ID]map[string]acceptorWrapper),
	}
}

func (a *acceptorGroup) Accept(ctx *ConsensusContext, containerID ids.ID, container []byte) error {
	a.lock.RLock()
	defer a.lock.RUnlock()

	for acceptorName, acceptor := range a.acceptors[ctx.ChainID] {
		if err := acceptor.Accept(ctx, containerID, container); err != nil {
			a.log.Error("failed accepting container",
				zap.String("acceptorName", acceptorName),
				zap.Stringer("chainID", ctx.ChainID),
				zap.Stringer("containerID", containerID),
				zap.Error(err),
			)
			if acceptor.dieOnError {
				return fmt.Errorf("acceptor %s on chain %s erred while accepting %s: %w", acceptorName, ctx.ChainID, containerID, err)
			}
		}
	}
	return nil
}

func (a *acceptorGroup) RegisterAcceptor(chainID ids.ID, acceptorName string, acceptor Acceptor, dieOnError bool) error {
	a.lock.Lock()
	defer a.lock.Unlock()

	acceptors, exist := a.acceptors[chainID]
	if !exist {
		acceptors = make(map[string]acceptorWrapper)
		a.acceptors[chainID] = acceptors
	}

	if _, ok := acceptors[acceptorName]; ok {
		return fmt.Errorf("callback %s already exists on chain %s", acceptorName, chainID)
	}

	acceptors[acceptorName] = acceptorWrapper{
		Acceptor:   acceptor,
		dieOnError: dieOnError,
	}
	return nil
}

func (a *acceptorGroup) DeregisterAcceptor(chainID ids.ID, acceptorName string) error {
	a.lock.Lock()
	defer a.lock.Unlock()

	acceptors, exist := a.acceptors[chainID]
	if !exist {
		return fmt.Errorf("chain %s has no callbacks", chainID)
	}

	if _, ok := acceptors[acceptorName]; !ok {
		return fmt.Errorf("callback %s does not exist on chain %s", acceptorName, chainID)
	}

	if len(acceptors) == 1 {
		delete(a.acceptors, chainID)
	} else {
		delete(acceptors, acceptorName)
	}
	return nil
}
