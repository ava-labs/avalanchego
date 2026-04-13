// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/libevm/core/types"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/saevm/gastime"
	"github.com/ava-labs/avalanchego/vms/saevm/hook/hookstest"
	"github.com/ava-labs/avalanchego/vms/saevm/proxytime"
)

func TestGasTime(t *testing.T) {
	const (
		unix   = 42
		frac   = 12_345
		scale  = 250
		target = 1_000_000_000 / gastime.TargetToRate / scale
		nanos  = frac * scale
	)
	rate := gastime.SafeRateOfTarget(target)

	hooks := hookstest.NewStub(target, hookstest.WithNow(func() time.Time {
		return time.Unix(unix, nanos)
	}))
	parent := &types.Header{
		Number: big.NewInt(1),
	}
	hdr, err := hooks.BuildHeader(parent)
	require.NoErrorf(t, err, "%T.BuildHeader()", hooks)

	got := GasTime(hooks, hdr, parent)
	want := proxytime.New(unix, rate)
	want.Tick(frac)

	if diff := cmp.Diff(want, got, proxytime.CmpOpt[gas.Gas]()); diff != "" {
		t.Errorf("GasTime(...) diff (-want +got):\n%s", diff)
	}
}

func FuzzTimeExtraction(f *testing.F) {
	// There are two different ways that the gas time of a block can be
	// calculated, both of which result in the same value. While neither can
	// result in an overflow due to required invariants, this guarantee is much
	// easier to reason about when inspecting [GasTime]. The alternative, via
	// [proxytime.Of] and then [proxytime.Time.SetRate], is more obviously
	// correct but too general-purpose and requires ignoring/checking a returned
	// `error`. We can therefore use this equivalence for differential fuzzing.

	f.Fuzz(func(t *testing.T, unix int64, target uint64, subSec int64) {
		if subSec < 0 || time.Duration(subSec) >= time.Second {
			t.Skip("Invalid sub-second value")
		}
		if target == 0 {
			t.Skip("Zero target")
		}

		hooks := hookstest.NewStub(gas.Gas(target), hookstest.WithNow(func() time.Time {
			return time.Unix(unix, subSec)
		}))
		parent := &types.Header{
			Number: big.NewInt(1),
		}
		hdr, err := hooks.BuildHeader(parent)
		require.NoErrorf(t, err, "%T.BuildHeader()", hooks)

		t.Run("PreciseTime", func(t *testing.T) {
			got := PreciseTime(hooks, hdr)
			want := hooks.Now()
			if got != want {
				t.Errorf("PreciseTime() = %v; want %v", got, want)
			}
		})

		t.Run("GasTime", func(t *testing.T) {
			got := GasTime(hooks, hdr, parent)

			want := proxytime.Of[gas.Gas](hooks.Now())
			rate := gastime.SafeRateOfTarget(gas.Gas(target))
			want.SetRate(rate)

			if diff := cmp.Diff(want, got, proxytime.CmpOpt[gas.Gas]()); diff != "" {
				t.Errorf("diff (-proxytime.Of +GasTime):\n%s", diff)
			}
		})
	})
}
