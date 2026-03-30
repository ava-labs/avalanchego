// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gastime

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/arr4n/shed/testerr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/intmath"
	"github.com/ava-labs/avalanchego/vms/saevm/proxytime"

	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
)

func mustNew(tb testing.TB, at time.Time, target, startingExcess gas.Gas, gasPriceConfig saetypes.GasPriceConfig) *Time {
	tb.Helper()
	tm, err := New(at, target, startingExcess, gasPriceConfig)
	require.NoError(tb, err, "New(%v, %d, %d, %v)", at, target, startingExcess, gasPriceConfig)
	return tm
}

func (tm *Time) cloneViaCanotoRoundTrip(tb testing.TB) *Time {
	tb.Helper()
	x := new(Time)
	require.NoErrorf(tb, x.UnmarshalCanoto(tm.MarshalCanoto()), "%T.UnmarshalCanoto(%[1]T.MarshalCanoto())", tm)
	return x
}

func TestClone(t *testing.T) {
	tm := mustNew(t, time.Unix(42, 1), 1e6, 1e5, saetypes.GasPriceConfig{TargetToExcessScaling: 100, MinPrice: 200})

	if diff := cmp.Diff(tm, tm.Clone(), CmpOpt()); diff != "" {
		t.Errorf("%T.Clone() diff (-want +got):\n%s", tm, diff)
	}
	if diff := cmp.Diff(tm, tm.cloneViaCanotoRoundTrip(t), CmpOpt()); diff != "" {
		t.Errorf("%T.UnmarshalCanoto(%[1]T.MarshalCanoto()) diff (-want +got):\n%s", tm, diff)
	}
}

// state captures parameters about a [Time] for assertion in tests. It includes
// both explicit (i.e. struct fields) and derived parameters (e.g. gas price),
// which aid testing of behaviour and invariants in a more fine-grained manner
// than direct comparison of two instances.
type state struct {
	UnixTime             uint64
	ConsumedThisSecond   proxytime.FractionalSecond[gas.Gas]
	Rate, Target, Excess gas.Gas
	Price                gas.Price
}

func (tm *Time) state() state {
	return state{
		UnixTime:           tm.Unix(),
		ConsumedThisSecond: tm.Fraction(),
		Rate:               tm.Rate(),
		Target:             tm.Target(),
		Excess:             tm.Excess(),
		Price:              tm.Price(),
	}
}

func (tm *Time) requireState(tb testing.TB, desc string, want state, opts ...cmp.Option) {
	tb.Helper()
	if diff := cmp.Diff(want, tm.state(), opts...); diff != "" {
		tb.Fatalf("%s (-want +got):\n%s", desc, diff)
	}
}

func TestNew(t *testing.T) {
	frac := func(num, den gas.Gas) (f proxytime.FractionalSecond[gas.Gas]) {
		f.Numerator = num
		f.Denominator = den
		return
	}

	ignore := cmpopts.IgnoreFields(state{}, "Rate", "Price")

	tests := []struct {
		name           string
		unix, nanos    int64
		target, excess gas.Gas
		want           state
	}{
		{
			name:   "rate at nanosecond resolution",
			unix:   42,
			nanos:  123_456,
			target: 1e9 / TargetToRate,
			want: state{
				UnixTime:           42,
				ConsumedThisSecond: frac(123_456, 1e9),
				Target:             1e9 / TargetToRate,
			},
		},
		{
			name:   "scaling in constructor not applied to starting excess",
			unix:   100,
			nanos:  TargetToRate,
			target: 50 / TargetToRate,
			excess: 987_654,
			want: state{
				UnixTime:           100,
				ConsumedThisSecond: frac(1, 50),
				Target:             50 / TargetToRate,
				Excess:             987_654,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := time.Unix(tt.unix, tt.nanos)
			got := mustNew(t, tm, tt.target, tt.excess, DefaultGasPriceConfig())
			got.requireState(t, fmt.Sprintf("New(%v, %d, %d)", tm, tt.target, tt.excess), tt.want, ignore)
		})
	}
}

func TestScaling(t *testing.T) {
	const initExcess = gas.Gas(1_234_567_890)
	tm := mustNew(t, time.Unix(42, 0), 1.6e6, initExcess, DefaultGasPriceConfig())

	// The initial price isn't important in this test; what we care about is
	// that it's invariant under scaling of the target etc.
	initPrice := tm.Price()
	if initPrice == 1 {
		t.Fatalf("Bad test setup: increase initial excess to achieve %T > 1", initPrice)
	}

	ignore := cmpopts.IgnoreFields(state{}, "UnixTime", "ConsumedThisSecond")

	tm.requireState(t, "initial", state{
		Rate:   3.2e6,
		Target: 1.6e6,
		Excess: initExcess,
		Price:  initPrice,
	}, ignore)

	tm.SetTarget(3.2e6)
	tm.requireState(t, "after SetTarget()", state{
		Rate:   6.4e6,
		Target: 3.2e6,
		Excess: 2 * initExcess,
		Price:  initPrice, // unchanged
	}, ignore)

	// SetRate is identical to setting via the target, as long as the rate is
	// even. Although the documentation states that SetTarget is preferred, we
	// still need to test SetRate.
	const (
		wantTargetViaRate = 2e6
		wantRate          = wantTargetViaRate * TargetToRate
	)
	want := state{
		Rate:   wantRate,
		Target: wantTargetViaRate,
		Excess: (func() gas.Gas {
			// Scale the _initial_ excess relative to the new and _initial_
			// rates, not the most recent rate before scaling.
			x, _, err := intmath.MulDivCeil(initExcess, wantRate, 3.2e6)
			require.NoErrorf(t, err, "intmath.MulDivCeil(%d, %d, %d)", initExcess, 4e6, 3.2e6)
			return x
		})(),
		Price: initPrice, // unchanged
	}
	for roundingError := range gas.Gas(TargetToRate) {
		r := wantRate + roundingError
		tm.SetRate(r)
		tm.requireState(t, fmt.Sprintf("after SetRate(%d)", r), want, ignore)
	}

	testPostClone := func(t *testing.T, cloned *Time) {
		t.Helper()
		want := want
		cloned.requireState(t, "unchanged immediately after clone", want, ignore)

		cloned.SetRate(cloned.Rate() * 2)
		tm.requireState(t, "original Time unchanged by setting clone's rate", want, ignore)

		want.Rate *= 2
		want.Target *= 2
		want.Excess *= 2
		cloned.requireState(t, "scaling after clone and then SetRate()", want, ignore)
	}

	t.Run("clone", func(t *testing.T) {
		testPostClone(t, tm.Clone())
	})

	t.Run("canoto_roundtrip", func(t *testing.T) {
		testPostClone(t, tm.cloneViaCanotoRoundTrip(t))
	})
}

func TestExcess(t *testing.T) {
	const rate = gas.Gas(3.2e6)
	tm := mustNew(t, time.Unix(42, 0), rate/2, 0, DefaultGasPriceConfig())

	frac := func(num gas.Gas) (f proxytime.FractionalSecond[gas.Gas]) {
		f.Numerator = num
		f.Denominator = rate
		return f
	}

	ignore := cmpopts.IgnoreFields(state{}, "Rate", "Target", "Price")

	tm.requireState(t, "initial", state{
		UnixTime:           42,
		ConsumedThisSecond: frac(0),
		Excess:             0,
	}, ignore)

	// NOTE: when R = 2T, excess increases or decreases by half the passage of
	// time, depending on whether time was Tick()ed or FastForward()ed,
	// respectively.

	steps := []struct {
		desc string
		// Only one of fast-forwarding or ticking per step.
		ffToBefore     uint64
		ffToBeforeFrac gas.Gas
		tickBefore     gas.Gas
		want           state
	}{
		{
			desc:       "initial tick 1/2s",
			tickBefore: rate / 2,
			want: state{
				UnixTime:           42,
				ConsumedThisSecond: frac(rate / 2),
				Excess:             (rate / 2) / 2,
			},
		},
		{
			desc:       "total tick 3/4s",
			tickBefore: rate / 4,
			want: state{
				UnixTime:           42,
				ConsumedThisSecond: frac(3 * rate / 4),
				Excess:             3 * rate / 8,
			},
		},
		{
			desc:       "total tick 5/4s",
			tickBefore: rate / 2,
			want: state{
				UnixTime:           43,
				ConsumedThisSecond: frac(rate / 4),
				Excess:             5 * rate / 8,
			},
		},
		{
			desc:       "total tick 11.25s",
			tickBefore: 10 * rate,
			want: state{
				UnixTime:           53,
				ConsumedThisSecond: frac(rate / 4),
				Excess:             45 * rate / 8, // (11*4 + 1) quarters of ticking, halved
			},
		},
		{
			desc:       "no op fast forward",
			ffToBefore: 53,
			want: state{ // unchanged
				UnixTime:           53,
				ConsumedThisSecond: frac(rate / 4),
				Excess:             45 * rate / 8,
			},
		},
		{
			desc:       "fast forward 11.25s to 13s",
			ffToBefore: 55,
			want: state{
				UnixTime:           55,
				ConsumedThisSecond: frac(0),
				Excess:             45*rate/8 - 7*rate/8,
			},
		},
		{
			desc:           "fast forward 0.5s to 13.5s",
			ffToBefore:     55,
			ffToBeforeFrac: rate / 2,
			want: state{
				UnixTime:           55,
				ConsumedThisSecond: frac(rate / 2),
				Excess:             45*rate/8 - 7*rate/8 - (rate/2)/2,
			},
		},
		{
			desc:           "fast forward 0.75s to 14.25s",
			ffToBefore:     56,
			ffToBeforeFrac: rate / 4,
			want: state{
				UnixTime:           56,
				ConsumedThisSecond: frac(rate / 4),
				Excess:             45*rate/8 - 7*rate/8 - (rate/2)/2 - 3*(rate/4)/2,
			},
		},
		{
			desc:       "fast forward causes overflow when seconds multiplied by R",
			ffToBefore: math.MaxUint64,
			want: state{
				UnixTime:           math.MaxUint64,
				ConsumedThisSecond: frac(0),
				Excess:             0,
			},
		},
	}

	for _, s := range steps {
		ffSec, ffFrac := s.ffToBefore, s.ffToBeforeFrac
		tick := s.tickBefore

		switch ff := (ffSec > 0 || ffFrac > 0); {
		case ff && tick > 0:
			t.Fatalf("Bad test setup (%q) only FastForward() or Tick() before", s.desc)
		case ff:
			tm.FastForwardTo(ffSec, ffFrac)
		case tick > 0:
			tm.Tick(tick)
		}
		tm.requireState(t, s.desc, s.want, ignore)
	}
}

func TestMinAndStaticPrice(t *testing.T) {
	const (
		target = 1e6
		excess = target * DefaultTargetToExcessScaling // i.e. Price == floor(e)*MinPrice
	)

	tests := []struct {
		name     string
		minPrice gas.Price
		static   bool
		want     gas.Price
		wantErr  testerr.Want
	}{
		{
			name:     "min=1",
			minPrice: 1,
			want:     2,
		},
		{
			name:     "min=100",
			minPrice: 100,
			want:     271,
		},
		{
			name:     "high_min_no_overflow",
			minPrice: math.MaxUint64 / 2,
			want:     math.MaxUint64,
		},
		{
			name:     "zero_min_errors",
			minPrice: 0,
			wantErr:  testerr.Is(errInvalidGasPriceConfig),
		},
		{
			name:     "static_pricing_returns_min",
			minPrice: 123_456,
			static:   true,
			want:     123_456,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultGasPriceConfig()
			cfg.MinPrice = tt.minPrice
			cfg.StaticPricing = tt.static

			tm, err := New(time.Unix(0, 0), target, excess, cfg)
			if diff := testerr.Diff(err, tt.wantErr); diff != "" {
				t.Fatalf("New(..., %+v) %s", cfg, diff)
			}
			if tt.wantErr != nil {
				return
			}
			if got := tm.Price(); got != tt.want {
				t.Errorf("New(..., excess=%d, %+v).Price() got %d; want %d", gas.Gas(excess), cfg, got, tt.want)
			}
		})
	}
}

func TestTargetClamping(t *testing.T) {
	tm := mustNew(t, time.Unix(0, 0), MaxTarget+1, 0, DefaultGasPriceConfig())
	require.Equal(t, MaxTarget, tm.Target(), "tm.Target() clamped by constructor")

	tests := []struct {
		setTo, want gas.Gas
	}{
		{setTo: 0, want: MinTarget},
		{setTo: 10, want: 10},
		{setTo: MaxTarget + 1, want: MaxTarget},
		{setTo: 20, want: 20},
		{setTo: math.MaxUint64, want: MaxTarget},
	}

	for _, tt := range tests {
		tm.SetTarget(tt.setTo)
		assert.Equalf(t, tt.want, tm.Target(), "%T.Target() after setting to %#x", tm, tt.setTo)
		assert.Equalf(t, tm.Target()*TargetToRate, tm.Rate(), "%T.Rate() == %d * %[1]T.Target()", tm, TargetToRate)
	}
}

func TestNoExcessOverflow(t *testing.T) {
	tm := mustNew(t, time.Unix(0, 0), 1, math.MaxUint64-100, DefaultGasPriceConfig())
	tm.SetTarget(MaxTarget)
	require.Equal(t, gas.Gas(math.MaxUint64), tm.Excess(), "Excess() after scaling")
	tm.FastForwardTo(1, 0)
	require.Less(t, tm.Excess(), gas.Gas(math.MaxUint64), "Excess() after capped and then fast-forwarding")
}

func TestTickExcessOverflow(t *testing.T) {
	const (
		shortFall   = 2
		startExcess = math.MaxUint64 - shortFall
		tick        = TargetToRate * (1 + shortFall) // increases excess by 1+shortFall -> overflow risk
	)
	tm := mustNew(t, time.Unix(0, 0), 1, startExcess, DefaultGasPriceConfig())
	tm.Tick(tick)
	require.Greater(t, tm.Excess(), gas.Gas(startExcess), "Excess() must increase after Tick(>1)")
}
