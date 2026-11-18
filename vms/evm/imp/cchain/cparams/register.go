// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cparams

import (
	"math/big"

	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/params"
)

var payloads params.ExtraPayloads[*ChainConfig, Rules]

// GetExtra returns the registered extra [ChainConfig] on c. The returned value
// will not be nil.
func GetExtra(c *params.ChainConfig) *ChainConfig {
	ex := payloads.ChainConfig.Get(c)
	if ex == nil {
		ex = &ChainConfig{}
		payloads.ChainConfig.Set(c, ex)
	}
	return ex
}

// WithExtra sets the extra payload on c and returns the modified argument.
func WithExtra(c *params.ChainConfig, extra *ChainConfig) *params.ChainConfig {
	payloads.ChainConfig.Set(c, extra)
	return c
}

// GetRulesExtra returns the registered extra [Rules] on r.
func GetRulesExtra(r params.Rules) *Rules {
	return payloads.Rules.GetPointer(&r)
}

// RegisterExtras registers hooks and payloads with libevm. It MUST NOT be
// called more than once and therefore is only allowed to be used in tests and
// `package main`, to avoid polluting other packages that transitively depend on
// this one but don't need registration.
//
// Without a call to RegisterExtras, much of the functionality of this package
// will work, and most will simply panic.
func RegisterExtras() {
	payloads = params.RegisterExtras(extrasToRegister())
}

// WithTempRegisteredExtras runs `fn` with temporary registration otherwise
// equivalent to a call to [RegisterExtras], but limited to the life of `fn`.
//
// This function is not intended for direct use. Use
// `libevm.WithTempRegisteredExtras()` instead as it calls this along with
// all other temporary-registration functions.
func WithTempRegisteredExtras(lock libevm.ExtrasLock, fn func() error) error {
	old := payloads
	defer func() { payloads = old }()

	return params.WithTempRegisteredExtras(
		lock, extrasToRegister(),
		func(extras params.ExtraPayloads[*ChainConfig, Rules]) error {
			payloads = extras
			return fn()
		},
	)
}

func extrasToRegister() params.Extras[*ChainConfig, Rules] {
	return params.Extras[*ChainConfig, Rules]{
		ReuseJSONRoot: true, // Reuse the root JSON input when unmarshalling the extra payload.
		NewRules:      constructRulesExtra,
	}
}

// constructRulesExtra acts as an adjunct to the [params.ChainConfig.Rules]
// method. Its primary purpose is to construct the extra payload for the
// [params.Rules] but it MAY also modify the [params.Rules].
//
//nolint:revive // General-purpose types lose the meaning of args if unused ones are removed
func constructRulesExtra(c *params.ChainConfig, r *params.Rules, cEx *ChainConfig, blockNum *big.Int, isMerge bool, timestamp uint64) Rules {
	if cEx == nil {
		return Rules{}
	}
	return cEx.Rules(timestamp)
}
