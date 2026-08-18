// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"

	"github.com/ava-labs/libevm/core/rawdb"

	"github.com/ava-labs/avalanchego/snow"
)

// Consensus states reported by [Health.State].
const (
	healthStateInitializing  = "initializing"
	healthStateStateSyncing  = "stateSyncing"
	healthStateBootstrapping = "bootstrapping"
	healthStateNormalOp      = "normalOp"
	healthStateUnknown       = "unknown"
)

// Health is the detail returned by [VM.HealthCheck]. It is serialized into the
// node's health API response under the chain's `message.engine.vm` key, so the
// JSON field names below are an observable interface.
//
// Health deliberately mirrors the C-Chain's health details (see
// graft/coreth/plugin/evm.Health) so that tooling observing a node does not
// have to care which implementation is running the chain.
type Health struct {
	// State is the consensus state the VM is currently in.
	State string `json:"state"`
	// StateScheme is the resolved trie database scheme the VM is running with.
	StateScheme string `json:"stateScheme"`

	// TODO(#5513): report the state sync status, as the C-Chain's health
	// details do, once SAE implements C-Chain state sync.
}

// HealthCheck reports the VM's consensus state and trie database scheme.
//
// It never reports the chain as unhealthy: these details are informational, and
// the conditions that make a chain unhealthy are reported by the engine and by
// the node's own health checks.
func (vm *VM) HealthCheck(context.Context) (any, error) {
	return Health{
		State:       healthState(vm.consensusState.Get()),
		StateScheme: vm.stateScheme(),
	}, nil
}

// stateScheme returns the trie database scheme in use, substituting the default
// that [saedb.Config] documents for an unset scheme.
func (vm *VM) stateScheme() string {
	if scheme := vm.config.DBConfig.Scheme; scheme != "" {
		return scheme
	}
	return rawdb.HashScheme
}

// healthState maps a consensus state to its health representation. The
// [snow.State] stringer is not used because its output is prose rather than a
// stable identifier.
func healthState(state snow.State) string {
	switch state {
	case snow.Initializing:
		return healthStateInitializing
	case snow.StateSyncing:
		return healthStateStateSyncing
	case snow.Bootstrapping:
		return healthStateBootstrapping
	case snow.NormalOp:
		return healthStateNormalOp
	default:
		return healthStateUnknown
	}
}
