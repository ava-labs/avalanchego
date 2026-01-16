// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package tempextrastest exists solely to test [evm.WithTempRegisteredLibEVMExtras]
// because the primary [evm] tests leak goroutines. These result in race
// conditions with the temporary registration of extras, which is intended to be
// done separately.
package tempextrastest

import (
	"testing"

	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/params"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm"

	cparams "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
)

func TestWithTempRegisteredLibEVMExtras(t *testing.T) {
	params.TestOnlyClearRegisteredExtras()
	state.TestOnlyClearRegisteredExtras()
	types.TestOnlyClearRegisteredExtras()
	vm.TestOnlyClearRegisteredHooks()

	var reRegistered bool
	t.Cleanup(func() {
		if !reRegistered {
			evm.RegisterAllLibEVMExtras()
		}
	})

	payloadTests := map[string]func(t *testing.T){
		"params": func(t *testing.T) {
			t.Helper()
			require.False(t, cparams.GetRulesExtra(params.Rules{}).AvalancheRules.IsEtna)
		},
	}

	t.Run("with_temp_registration", func(t *testing.T) {
		require.NoError(t, evm.WithTempRegisteredLibEVMExtras(func() error {
			t.Run("payloads", func(t *testing.T) {
				for pkg, fn := range payloadTests {
					t.Run(pkg, fn)
				}
			})
			return nil
		}))
	})

	// These are deliberately placed after the tests of temporary registration,
	// to demonstrate that (a) they are indeed temporary, and (b) they would
	// otherwise panic.
	t.Run("without_registration", func(t *testing.T) {
		t.Run("payloads", func(t *testing.T) {
			for pkg, fn := range payloadTests {
				t.Run(pkg, func(t *testing.T) {
					require.Panics(t, func() { fn(t) })
				})
			}
		})
	})

	evm.RegisterAllLibEVMExtras()
	reRegistered = true

	t.Run("with_permanent_registration", func(t *testing.T) {
		t.Run("payloads", func(t *testing.T) {
			for pkg, fn := range payloadTests {
				t.Run(pkg, fn)
			}
		})
	})
}
