// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cparams

import (
	"github.com/ava-labs/libevm/libevm"

	ethparams "github.com/ava-labs/libevm/params"
)

var payloads ethparams.ExtraPayloads[*ChainConfig, RulesExtra]

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
		func(extras ethparams.ExtraPayloads[*ChainConfig, RulesExtra]) error {
			payloads = extras
			return fn()
		},
	)
}

func extrasToRegister() ethparams.Extras[*ChainConfig, RulesExtra] {
	return ethparams.Extras[*ChainConfig, RulesExtra]{
		ReuseJSONRoot: true, // when unmarshalling the extra payload.
		NewRules:      constructRulesExtra,
	}
}

// GetExtra returns the extra payload of the config, or nil if none was set with
// [WithExtra] or JSON decoding.
func GetExtra(c *ethparams.ChainConfig) *ChainConfig {
	return payloads.ChainConfig.Get(c)
}

// WithExtra sets the extra payload on c and returns the modified argument.
func WithExtra(c *ethparams.ChainConfig, extra *ChainConfig) *ethparams.ChainConfig {
	payloads.ChainConfig.Set(c, extra)
	return c
}

// GetRulesExtra returns the extra payload of r, as constructed by
// [ethparams.ChainConfig.Rules].
func GetRulesExtra(r ethparams.Rules) *RulesExtra {
	return payloads.Rules.GetPointer(&r)
}
