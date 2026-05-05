// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"context"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/vms/saevm/sae/rpc"

	subnetevmparams "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	libevmparams "github.com/ava-labs/libevm/params"
)

// EthExtrasAPI serves the subnet-evm-specifi`eth_*` JSON-RPC
// methods, with wire shapes matching legacy subnet-evm.
type EthExtrasAPI struct {
	backend rpc.GethBackends
}

// NewEthExtrasAPI binds the API to `backend`. Chain config and current
// header are read on every call, so `backend` may be long-lived.
func NewEthExtrasAPI(backend rpc.GethBackends) *EthExtrasAPI {
	return &EthExtrasAPI{backend: backend}
}

// ActivePrecompilesResult is the per-precompile entry in
// [ActiveRulesResult.ActivePrecompiles].
type ActivePrecompilesResult struct {
	Timestamp uint64 `json:"timestamp"`
}

// ActiveRulesResult is the response shape of `eth_getActiveRulesAt`.
type ActiveRulesResult struct {
	EthRules          libevmparams.Rules                 `json:"ethRules"`
	AvalancheRules    extras.AvalancheRules              `json:"avalancheRules"`
	ActivePrecompiles map[string]ActivePrecompilesResult `json:"precompiles"`
}

// GetActiveRulesAt returns the active eth + avalanche rules and precompile
// activations at `blockTimestamp`, defaulting to the current header's
// timestamp when nil.
func (s *EthExtrasAPI) GetActiveRulesAt(_ context.Context, blockTimestamp *uint64) ActiveRulesResult {
	var timestamp uint64
	if blockTimestamp != nil {
		timestamp = *blockTimestamp
	} else {
		timestamp = s.backend.CurrentHeader().Time
	}
	rules := s.backend.ChainConfig().Rules(common.Big0, subnetevmparams.IsMergeTODO, timestamp)
	res := ActiveRulesResult{
		EthRules:          rules,
		AvalancheRules:    subnetevmparams.GetRulesExtra(rules).AvalancheRules,
		ActivePrecompiles: map[string]ActivePrecompilesResult{},
	}
	for _, cfg := range subnetevmparams.GetRulesExtra(rules).Precompiles {
		if cfg.Timestamp() == nil {
			continue
		}
		res.ActivePrecompiles[cfg.Key()] = ActivePrecompilesResult{
			Timestamp: *cfg.Timestamp(),
		}
	}
	return res
}
