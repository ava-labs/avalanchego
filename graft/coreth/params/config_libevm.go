// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"math/big"

	"github.com/ava-labs/libevm/libevm"

	"github.com/ava-labs/avalanchego/vms/saevm/cchain/cparams"

	ethparams "github.com/ava-labs/libevm/params"
)

func extrasToRegister() ethparams.Extras[*cparams.ChainConfig, RulesExtra] {
	return ethparams.Extras[*cparams.ChainConfig, RulesExtra]{
		ReuseJSONRoot: true, // Reuse the root JSON input when unmarshalling the extra payload.
		NewRules:      constructRulesExtra,
	}
}

// RegisterExtras registers hooks and payloads with libevm. It MUST NOT be
// called more than once and therefore is only allowed to be used in tests and
// `package main`, to avoid polluting other packages that transitively depend on
// this one but don't need registration.
//
// Without a call to RegisterExtras, much of the functionality of this package
// will work, and most will simply panic.
func RegisterExtras() {
	payloads = ethparams.RegisterExtras(extrasToRegister())
}

// WithTempRegisteredExtras runs `fn` with temporary registration otherwise
// equivalent to a call to [RegisterExtras], but limited to the life of `fn`.
//
// This function is not intended for direct use. Use
// `evm.WithTempRegisteredLibEVMExtras()` instead as it calls this along with
// all other temporary-registration functions.
func WithTempRegisteredExtras(lock libevm.ExtrasLock, fn func() error) error {
	old := payloads
	defer func() { payloads = old }()

	return ethparams.WithTempRegisteredExtras(
		lock, extrasToRegister(),
		func(extras ethparams.ExtraPayloads[*cparams.ChainConfig, RulesExtra]) error {
			payloads = extras
			return fn()
		},
	)
}

var payloads ethparams.ExtraPayloads[*cparams.ChainConfig, RulesExtra]

// constructRulesExtra acts as an adjunct to the [params.ChainConfig.Rules]
// method. Its primary purpose is to construct the extra payload for the
// [params.Rules] but it MAY also modify the [params.Rules].
//
//nolint:revive // General-purpose types lose the meaning of args if unused ones are removed
func constructRulesExtra(c *ethparams.ChainConfig, r *ethparams.Rules, cEx *cparams.ChainConfig, blockNum *big.Int, isMerge bool, timestamp uint64) RulesExtra {
	var rules RulesExtra
	if cEx == nil {
		return rules
	}
	rules.AvalancheRules = cEx.Rules(timestamp)
	return rules
}
