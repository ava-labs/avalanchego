// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"

	"github.com/ava-labs/avalanchego/vms/saevm/sae"
)

// HealthCheck reports the VM's consensus state, trie database scheme, and state
// sync progress.
//
// It overrides the [sae.VM] method promoted through the embedded pointer, which
// MUST NOT be called here: the engine health-checks the VM throughout state
// syncing, but [VM.SetState] only constructs the [sae.VM] once bootstrapping
// begins, so the promoted method would be called on a nil receiver. The
// C-Chain-level [VM.mode] and [VM.stateScheme] are valid from [VM.Initialize]
// onwards.
//
// It never reports the chain as unhealthy: these details are informational, and
// the conditions that make a chain unhealthy are reported by the engine and by
// the node's own health checks. In particular a failed state sync, which is
// fatal to the chain, is surfaced to the engine by [VM.SetState].
func (vm *VM) HealthCheck(context.Context) (any, error) {
	return sae.Health{
		State:       sae.HealthState(vm.mode.Get()),
		StateScheme: vm.stateScheme,
		StateSync:   vm.SummaryHandler.Health(),
	}, nil
}
