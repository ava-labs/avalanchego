// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompiletest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	"github.com/ava-labs/avalanchego/graft/evm/precompileconfig"
	"github.com/ava-labs/avalanchego/vms/evm/warpmsg"
)

// MessageTest defines a unit test/benchmark for verifying a Warp message.
type MessageTest struct {
	Name string

	VerifyContext *warpmsg.VerifyContext

	Packed      warpmsg.Packed
	Rules       precompileconfig.Rules
	Gas         uint64
	GasErr      error
	ExpectedErr error
}

func (test MessageTest) Run(t testing.TB) {
	t.Helper()
	require := require.New(t)
	gas, gasErr := warp.MessageGas(test.Packed, test.Rules)
	require.ErrorIs(gasErr, test.GasErr)
	if test.GasErr != nil {
		return
	}

	require.Equal(test.Gas, gas)

	err := warp.VerifyMessage(test.VerifyContext, test.Packed)
	require.ErrorIs(err, test.ExpectedErr)
}

func RunMessageTests(t *testing.T, tests []MessageTest) {
	t.Helper()

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.Run(t)
		})
	}
}

func (test MessageTest) RunBenchmark(b *testing.B) {
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

func RunMessageBenchmarks(b *testing.B, tests []MessageTest) {
	b.Helper()

	for _, test := range tests {
		b.Run(test.Name, func(b *testing.B) {
			test.RunBenchmark(b)
		})
	}
}
