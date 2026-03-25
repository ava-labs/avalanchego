// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proxytime

import (
	"cmp"
	"fmt"
	"math"
	"testing"
	"time"

	gocmp "github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func frac(num, den uint64) FractionalSecond[uint64] {
	return FractionalSecond[uint64]{Numerator: num, Denominator: den}
}

func (tm *Time[D]) assertEq(tb testing.TB, desc string, seconds uint64, fraction FractionalSecond[D]) (equal bool) {
	tb.Helper()
	want := &Time[D]{
		seconds:  seconds,
		fraction: fraction.Numerator,
		hertz:    fraction.Denominator,
	}
	if diff := gocmp.Diff(want, tm, CmpOpt[D]()); diff != "" {
		tb.Errorf("%s diff (-want +got):\n%s", desc, diff)
		return false
	}
	return true
}

func (tm *Time[D]) requireEq(tb testing.TB, desc string, seconds uint64, fraction FractionalSecond[D]) {
	tb.Helper()
	if !tm.assertEq(tb, desc, seconds, fraction) {
		tb.FailNow()
	}
}

func TestTickAndCmp(t *testing.T) {
	const rate = 500
	tm := New(0, uint64(500))
	tm.assertEq(t, "New(0, ...)", 0, frac(0, rate))

	steps := []struct {
		tick              uint64
		wantSec, wantFrac uint64
	}{
		{
			tick:    100,
			wantSec: 0, wantFrac: 100,
		},
		{
			tick:    399,
			wantSec: 0, wantFrac: 499,
		},
		{
			// Although this is a no-op, it's useful to see the fraction for
			// understanding the next step.
			tick:    0,
			wantSec: 0, wantFrac: rate - 1,
		},
		{
			tick:    1,
			wantSec: 1, wantFrac: 0,
		},
		{
			tick:    rate,
			wantSec: 2, wantFrac: 0,
		},
		{
			tick:    400,
			wantSec: 2, wantFrac: 400,
		},
		{
			tick:    200,
			wantSec: 3, wantFrac: 100,
		},
		{
			tick:    3*rate + 100,
			wantSec: 6, wantFrac: 200,
		},
		{
			tick:    299,
			wantSec: 6, wantFrac: 499,
		},
		{
			tick:    2,
			wantSec: 7, wantFrac: 1,
		},
		{
			tick:    rate - 1,
			wantSec: 8, wantFrac: 0,
		},
		{
			// Set fraction to anything non-zero so we can test overflow
			// prevention with a tick of 2^64-1.
			tick:    1,
			wantSec: 8, wantFrac: 1,
		},
		{
			tick:     math.MaxUint64,
			wantSec:  8 + math.MaxUint64/rate,
			wantFrac: 1 + math.MaxUint64%rate,
		},
	}

	var ticked uint64
	for _, s := range steps {
		old := tm.Clone()

		tm.Tick(s.tick)
		ticked += s.tick
		tm.requireEq(t, fmt.Sprintf("%+d", ticked), s.wantSec, frac(s.wantFrac, rate))

		if got, want := tm.Compare(old), cmp.Compare(s.tick, 0); got != want {
			t.Errorf("After %T.Tick(%d); ticked.Cmp(original) got %d; want %d", tm, s.tick, got, want)
		}
		if got, want := old.Compare(tm), cmp.Compare(0, s.tick); got != want {
			t.Errorf("After %T.Tick(%d); original.Cmp(ticked) got %d; want %d", tm, s.tick, got, want)
		}
	}
}

func TestSetRate(t *testing.T) {
	const (
		initSeconds = 42
		divisor     = 3
		initRate    = uint64(1000 * divisor)
	)
	tm := New(initSeconds, initRate)

	const tick = uint64(100 * divisor)
	tm.Tick(tick)
	tm.requireEq(t, "baseline", initSeconds, frac(tick, initRate))

	steps := []struct {
		newRate, wantNumerator uint64
	}{
		{
			newRate:       initRate / divisor, // no rounding
			wantNumerator: tick / divisor,
		},
		{
			newRate:       initRate * 5,
			wantNumerator: tick * 5,
		},
		{
			newRate:       15_000, // same as above, but shows the numbers explicitly
			wantNumerator: 1_500,
		},
		{
			newRate:       75, // 15_000 / 200
			wantNumerator: 8,  // ceil(1_500/200 == 7.5)
		},
		{
			newRate:       25, // 75 / 3
			wantNumerator: 3,  // ceil(8/3 == 2.66...)
		},
	}

	for _, s := range steps {
		old := tm.Rate()
		tm.SetRate(s.newRate)
		desc := fmt.Sprintf("rate changed from %d to %d", old, s.newRate)
		tm.requireEq(t, desc, initSeconds, frac(s.wantNumerator, s.newRate))
	}
}

func TestSetRateRoundUpFullSecond(t *testing.T) {
	tests := []struct {
		rate, tick           uint64
		newRate, wantRoundUp uint64
	}{
		{
			rate:        100,
			tick:        99,
			newRate:     10,
			wantRoundUp: 1,
		},
		{
			rate:        100,
			tick:        99,
			newRate:     11,
			wantRoundUp: 1,
		},
		{
			rate:        100,
			tick:        98,
			newRate:     11,
			wantRoundUp: 2,
		},
		{
			rate:        97,
			tick:        92,
			newRate:     13,
			wantRoundUp: 5,
		},
	}

	for _, tt := range tests {
		const startUnix = 42

		t.Run(fmt.Sprintf("%d_of_%d_scaled_down_to_%d", tt.tick, tt.rate, tt.newRate), func(t *testing.T) {
			tm := New(startUnix, tt.rate)
			tm.Tick(tt.tick)
			tm.SetRate(tt.newRate)

			tm.assertEq(t, "After scaling rate down to force tick to next second", startUnix+1, frac(0, tt.newRate))
		})
	}
}

func TestAsTime(t *testing.T) {
	stdlib := time.Date(1986, time.October, 1, 0, 0, 0, 0, time.UTC)

	const rate uint64 = 500
	tm := New(uint64(stdlib.Unix()), rate) //nolint:gosec // Known to not overflow
	if got, want := tm.AsTime(), stdlib; !got.Equal(want) {
		t.Fatalf("%T.AsTime() at construction got %v; want %v", tm, got, want)
	}

	tm.Tick(1)
	if got, want := tm.AsTime(), stdlib.Add(2*time.Millisecond); !got.Equal(want) {
		t.Fatalf("%T.AsTime() after ticking 1/%d got %v; want %v", tm, rate, got, want)
	}
}

func TestCanotoRoundTrip(t *testing.T) {
	tests := []struct {
		name                string
		seconds, rate, tick uint64
	}{
		{
			name:    "non_zero_fields",
			seconds: 42,
			rate:    10_000,
			tick:    1_234,
		},
		{
			name: "zero_seconds",
			rate: 100,
			tick: 1,
		},
		{
			name:    "zero_fractional_second",
			seconds: 999,
			rate:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := New(tt.seconds, tt.rate)
			tm.Tick(tt.tick)

			got := new(Time[uint64])
			require.NoErrorf(t, got.UnmarshalCanoto(tm.MarshalCanoto()), "%T.UnmarshalCanoto(%[1]T.MarshalCanoto())", got)
			got.assertEq(t, fmt.Sprintf("%T.UnmarshalCanoto(%[1]T.MarshalCanoto())", tm), tt.seconds, frac(tt.tick, tt.rate))
		})
	}
}

func TestFastForward(t *testing.T) {
	const rate = uint64(1000)
	tm := New(42, rate)

	steps := []struct {
		tickBefore uint64
		ffTo       uint64
		ffToFrac   uint64
		wantSec    uint64
		wantFrac   FractionalSecond[uint64]
	}{
		{
			tickBefore: 0,
			ffTo:       41, // in the past
			wantSec:    0,
			wantFrac:   frac(0, rate),
		},
		{
			tickBefore: 100, // 42.100
			ffTo:       42,  // in the past
			wantSec:    0,
			wantFrac:   frac(0, rate),
		},
		{
			tickBefore: 0, // 42.100
			ffTo:       43,
			wantSec:    0,
			wantFrac:   frac(900, rate),
		},
		{
			tickBefore: 0, // 43.000
			ffTo:       44,
			wantSec:    1,
			wantFrac:   frac(0, rate),
		},
		{
			tickBefore: 200, // 44.200
			ffTo:       50,
			wantSec:    5,
			wantFrac:   frac(800, rate),
		},
		{
			tickBefore: 0, // 50.000
			ffTo:       50,
			ffToFrac:   900,
			wantSec:    0,
			wantFrac:   frac(900, rate),
		},
		{
			tickBefore: 0, // 50.900
			ffTo:       51,
			ffToFrac:   100,
			wantSec:    0,
			wantFrac:   frac(200, rate),
		},
		{
			tickBefore: 100, // 51.200
			ffTo:       51,
			ffToFrac:   200,
			wantSec:    0,
			wantFrac:   frac(0, rate),
		},
	}

	for _, s := range steps {
		tm.Tick(s.tickBefore)
		gotSec, gotFrac := tm.FastForwardTo(s.ffTo, s.ffToFrac)
		assert.Equal(t, s.wantSec, gotSec, "Fast-forwarded seconds")
		assert.Equal(t, s.wantFrac, gotFrac, "Fast-forwarded fractional numerator")

		if t.Failed() {
			t.FailNow()
		}
	}
}

func TestConvertMilliseconds(t *testing.T) {
	type Hz uint64

	tests := []struct {
		rate          Hz
		ms            uint64
		wantSec       uint64
		wantNumerator Hz // ms * (rate / 1000)
	}{
		{
			rate:          1000,
			ms:            42,
			wantNumerator: 42,
		},
		{
			rate:          1234 * 2,
			ms:            1000 / 2,
			wantNumerator: 1234,
		},
		{
			rate:          98765 * 4,
			ms:            1000 / 4,
			wantNumerator: 98765,
		},
		{
			rate:          142857 * 1000,
			ms:            1,
			wantNumerator: 142857,
		},
		{
			rate:          1_001,
			ms:            500,
			wantNumerator: 500,
		},
		{
			rate:          1000,
			ms:            1001,
			wantSec:       1,
			wantNumerator: 1,
		},
		{
			rate:          1000,
			ms:            314_159,
			wantSec:       314,
			wantNumerator: 159,
		},
	}

	for _, tt := range tests {
		gotSec, gotFrac := ConvertMilliseconds(tt.rate, tt.ms)
		wantFrac := FractionalSecond[Hz]{
			Numerator:   tt.wantNumerator,
			Denominator: tt.rate,
		}
		if gotSec != tt.wantSec || gotFrac != wantFrac {
			t.Errorf("ConvertMilliseconds(%d, %d) got (%v, %v); want (%v, %v)", tt.ms, tt.rate, gotSec, gotFrac, tt.wantSec, wantFrac)
		}
	}
}

func TestCmpUnix(t *testing.T) {
	tests := []struct {
		tm         *Time[uint64]
		tick       uint64
		cmpAgainst uint64
		want       int
	}{
		{
			tm:         New[uint64](42, 1e6),
			cmpAgainst: 42,
			want:       0,
		},
		{
			tm:         New[uint64](42, 1e6),
			tick:       1,
			cmpAgainst: 42,
			want:       1,
		},
		{
			tm:         New[uint64](41, 100),
			tick:       99,
			cmpAgainst: 42,
			want:       -1,
		},
	}

	for _, tt := range tests {
		tt.tm.Tick(tt.tick)
		if got := tt.tm.CompareUnix(tt.cmpAgainst); got != tt.want {
			t.Errorf("Time{%s}.CmpUnix(%d) got %d; want %d", tt.tm.String(), tt.cmpAgainst, got, tt.want)
		}
	}
}

func TestCompareDifferentRates(t *testing.T) {
	fromFrac := func(num, denom uint64) *Time[uint64] {
		// All comparisons are targeting fractional differences so we want the
		// Unix seconds to be equal, but the actual value is irrelevant.
		tm := New(42, denom)
		tm.Tick(num)
		return tm
	}

	tests := []struct {
		tm, u *Time[uint64]
		want  int
	}{
		{
			tm:   fromFrac(0, 1e6),
			u:    fromFrac(0, 2e6),
			want: 0,
		},
		{
			tm:   fromFrac(5, 10),
			u:    fromFrac(10, 20),
			want: 0,
		},
		{
			tm:   fromFrac(3, 7),
			u:    fromFrac(4, 8),
			want: -1,
		},
		{
			tm:   fromFrac(1<<62, 1<<63),
			u:    fromFrac(1, 2),
			want: 0,
		},
		{
			tm:   fromFrac(math.MaxUint64/2, math.MaxUint64),
			u:    fromFrac(math.MaxUint64/2+1, math.MaxUint64),
			want: -1,
		},
		{
			tm:   fromFrac(1<<61+1, 1<<62),
			u:    fromFrac(1<<62, 1<<63),
			want: 1,
		},
		{
			tm:   fromFrac(math.MaxUint64-1, math.MaxUint64),
			u:    fromFrac(1, math.MaxUint64-1),
			want: 1,
		},
	}

	for _, tt := range tests {
		a, b := tt.tm, tt.u
		want := tt.want

		assert.Equalf(t, want, a.Compare(b), "Time{%s}.Compare(%s)", a, b)
		assert.Equalf(t, -want, b.Compare(a), "Time{%s}.Compare(%s)", b, a)
	}
}

func FuzzFractionLessThanHertz(f *testing.F) {
	// Breaking this invariant could result in a panic. All methods that set
	// [Time.fraction] have been inspected and here they're exercised.
	f.Fuzz(func(t *testing.T, unix, hertz, tick, to, toFrac, sub, newRate uint64) {
		if hertz == 0 || newRate == 0 {
			t.Skip("zero rate")
		}

		tm := New(unix, hertz)
		req := func(t *testing.T, after string) {
			t.Helper()
			require.Lessf(t, tm.fraction, tm.hertz, "after %s", after)
		}
		req(t, "New()")

		tm.FastForwardTo(to, toFrac)
		req(t, "FastForwardTo()")

		tm.Tick(tick)
		req(t, "Tick()")

		tm.Sub(sub)
		req(t, "Sub()")

		tm.SetRate(newRate)
		req(t, "SetRate()")
	})
}
