// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xputtest

import (
	"sync"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/utils/logging"
)

type issuableVM interface {
	IssueTx([]byte, func(choices.Status)) (ids.ID, error)
}

// Issuer manages all the chain transaction flushing.
type Issuer struct {
	lock      sync.Mutex
	log       logging.Logger
	vms       map[[32]byte]issuableVM
	locks     map[[32]byte]sync.Locker
	callbacks chan func()
}

// Initialize this flusher
func (i *Issuer) Initialize(log logging.Logger) {
	i.lock.Lock()
	defer i.lock.Unlock()
	i.log = log
	i.vms = make(map[[32]byte]issuableVM)
	i.locks = make(map[[32]byte]sync.Locker)
	i.callbacks = make(chan func(), 1000)

	go func() {
		for callback := range i.callbacks {
			callback()
		}
	}()
}

// RegisterChain implements the registrant
func (i *Issuer) RegisterChain(ctx *snow.Context, vm interface{}) {
	i.lock.Lock()
	defer i.lock.Unlock()

	key := ctx.ChainID.Key()

	switch vm := vm.(type) {
	case issuableVM:
		i.vms[key] = vm
		i.locks[key] = &ctx.Lock
	}
}

// IssueTx issue the transaction to the chain and register the timeout.
func (i *Issuer) IssueTx(chainID ids.ID, tx []byte, finalized func(choices.Status)) {
	i.lock.Lock()
	defer i.lock.Unlock()

	key := chainID.Key()
	if lock, exists := i.locks[key]; exists {
		i.callbacks <- func() {
			lock.Lock()
			defer lock.Unlock()
			if vm, exists := i.vms[key]; exists {
				if _, err := vm.IssueTx(tx, finalized); err != nil {
					i.log.Error("Issuing the tx returned with %s unexpectedly", err)
				}
			}
		}
	} else {
		i.log.Warn("Attempted to issue a Tx to an unsupported chain %s", chainID)
	}
}
