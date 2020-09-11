// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package triggers

import (
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// EventDispatcher receives events from consensus and dispatches the events to triggers
type EventDispatcher struct {
	lock          sync.Mutex
	log           logging.Logger
	chainHandlers map[[32]byte]map[string]interface{}
	handlers      map[string]interface{}
}

// Initialize creates the EventDispatcher's initial values
func (ed *EventDispatcher) Initialize(log logging.Logger) {
	ed.log = log
	ed.chainHandlers = make(map[[32]byte]map[string]interface{})
	ed.handlers = make(map[string]interface{})
}

// Accept is called when a transaction or block is accepted
func (ed *EventDispatcher) Accept(chainID, containerID ids.ID, container []byte) {
	ed.lock.Lock()
	defer ed.lock.Unlock()

	for id, handler := range ed.handlers {
		handler, ok := handler.(Acceptor)
		if !ok {
			continue
		}

		if err := handler.Accept(chainID, containerID, container); err != nil {
			ed.log.Error("unable to Accept on %s for chainID %s: %s", id, chainID, err)
		}
	}

	events, exist := ed.chainHandlers[chainID.Key()]
	if !exist {
		return
	}
	for id, handler := range events {
		handler, ok := handler.(Acceptor)
		if !ok {
			continue
		}

		if err := handler.Accept(chainID, containerID, container); err != nil {
			ed.log.Error("unable to Accept on %s for chainID %s: %s", id, chainID, err)
		}
	}
}

// Reject is called when a transaction or block is rejected
func (ed *EventDispatcher) Reject(chainID, containerID ids.ID, container []byte) {
	ed.lock.Lock()
	defer ed.lock.Unlock()

	for id, handler := range ed.handlers {
		handler, ok := handler.(Rejector)
		if !ok {
			continue
		}

		if err := handler.Reject(chainID, containerID, container); err != nil {
			ed.log.Error("unable to Reject on %s for chainID %s: %s", id, chainID, err)
		}
	}

	events, exist := ed.chainHandlers[chainID.Key()]
	if !exist {
		return
	}
	for id, handler := range events {
		handler, ok := handler.(Rejector)
		if !ok {
			continue
		}

		if err := handler.Reject(chainID, containerID, container); err != nil {
			ed.log.Error("unable to Reject on %s for chainID %s: %s", id, chainID, err)
		}
	}
}

// Issue is called when a transaction or block is issued
func (ed *EventDispatcher) Issue(chainID, containerID ids.ID, container []byte) {
	ed.lock.Lock()
	defer ed.lock.Unlock()

	for id, handler := range ed.handlers {
		handler, ok := handler.(Issuer)
		if !ok {
			continue
		}

		if err := handler.Issue(chainID, containerID, container); err != nil {
			ed.log.Error("unable to Issue on %s for chainID %s: %s", id, chainID, err)
		}
	}

	events, exist := ed.chainHandlers[chainID.Key()]
	if !exist {
		return
	}
	for id, handler := range events {
		handler, ok := handler.(Issuer)
		if !ok {
			continue
		}

		if err := handler.Issue(chainID, containerID, container); err != nil {
			ed.log.Error("unable to Issue on %s for chainID %s: %s", id, chainID, err)
		}
	}
}

// RegisterChain places a new chain handler into the system
func (ed *EventDispatcher) RegisterChain(chainID ids.ID, identifier string, handler interface{}) error {
	ed.lock.Lock()
	defer ed.lock.Unlock()

	chainIDKey := chainID.Key()
	events, exist := ed.chainHandlers[chainIDKey]
	if !exist {
		events = make(map[string]interface{})
		ed.chainHandlers[chainIDKey] = events
	}

	if _, ok := events[identifier]; ok {
		return fmt.Errorf("handler %s already exists on chain %s", identifier, chainID)
	}

	events[identifier] = handler
	return nil
}

// DeregisterChain removes a chain handler from the system
func (ed *EventDispatcher) DeregisterChain(chainID ids.ID, identifier string) error {
	ed.lock.Lock()
	defer ed.lock.Unlock()

	chainIDKey := chainID.Key()
	events, exist := ed.chainHandlers[chainIDKey]
	if !exist {
		return fmt.Errorf("chain %s has no handlers", chainID)
	}

	if _, ok := events[identifier]; !ok {
		return fmt.Errorf("handler %s does not exist on chain %s", identifier, chainID)
	}

	if len(events) == 1 {
		delete(ed.chainHandlers, chainIDKey)
	} else {
		delete(events, identifier)
	}
	return nil
}

// Register places a new handler into the system
func (ed *EventDispatcher) Register(identifier string, handler interface{}) error {
	ed.lock.Lock()
	defer ed.lock.Unlock()

	if _, exist := ed.handlers[identifier]; exist {
		return fmt.Errorf("handler %s already exists", identifier)
	}

	ed.handlers[identifier] = handler
	return nil
}

// Deregister removes a handler from the system
func (ed *EventDispatcher) Deregister(identifier string) error {
	ed.lock.Lock()
	defer ed.lock.Unlock()

	if _, exist := ed.handlers[identifier]; !exist {
		return fmt.Errorf("handler %s already exists", identifier)
	}

	delete(ed.handlers, identifier)
	return nil
}
