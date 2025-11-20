// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompiletest

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"
)

// PredicateTest defines a unit test/benchmark for verifying a precompile predicate.
type PredicateTest struct {
	Name string

	Config precompileconfig.Config

	PredicateContext *precompileconfig.PredicateContext

	Predicate   predicate.Predicate
	Rules       precompileconfig.Rules
	Gas         uint64
	GasErr      error
	ExpectedErr error
}

func (test PredicateTest) Run(t testing.TB) {
	t.Helper()
	require := require.New(t)
	predicater := test.Config.(precompileconfig.Predicater)

	predicateGas, predicateGasErr := predicater.PredicateGas(test.Predicate, test.Rules)
	require.ErrorIs(predicateGasErr, test.GasErr)
	if test.GasErr != nil {
		return
	}

	require.Equal(test.Gas, predicateGas)

	predicateRes := predicater.VerifyPredicate(test.PredicateContext, test.Predicate)
	require.ErrorIs(predicateRes, test.ExpectedErr)
}

func RunPredicateTests(t *testing.T, tests []PredicateTest) {
	t.Helper()

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.Run(t)
		})
	}
}

func (test PredicateTest) RunBenchmark(b *testing.B) {
	b.ReportAllocs()
	start := time.Now()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		test.Run(b)
	}
	b.StopTimer()
	elapsed := uint64(time.Since(start))
	if elapsed < 1 {
		elapsed = 1
	}

	gasUsed := test.Gas * uint64(b.N)
	b.ReportMetric(float64(test.Gas), "gas/op")
	// Keep it as uint64, multiply 100 to get two digit float later
	mgasps := (100 * 1000 * gasUsed) / elapsed
	b.ReportMetric(float64(mgasps)/100, "mgas/s")
}

func RunPredicateBenchmarks(b *testing.B, tests []PredicateTest) {
	b.Helper()

	for _, test := range tests {
		b.Run(test.Name, func(b *testing.B) {
			test.RunBenchmark(b)
		})
	}
}
