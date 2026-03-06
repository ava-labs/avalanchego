// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package intmath

import (
	"errors"
	"math"
	"math/rand/v2"
	"testing"
)

const max = math.MaxUint64

func TestBoundedSubtract(t *testing.T) {
	tests := []struct {
		a, b, floor, want uint64
	}{
		{a: 1, b: 2, floor: 0, want: 0}, // a < b
		{a: 2, b: 1, floor: 0, want: 1}, // not bounded
		{a: 2, b: 1, floor: 1, want: 1}, // a - b == floor
		{a: 2, b: 2, floor: 1, want: 1}, // bounded
		{a: 3, b: 1, floor: 1, want: 2},
		{a: max, b: 10, floor: max - 9, want: max - 9}, // `a` threshold (`max+1`) would overflow uint64
		{a: max, b: 10, floor: max - 11, want: max - 10},
	}

	for _, tt := range tests {
		if got := BoundedSubtract(tt.a, tt.b, tt.floor); got != tt.want {
			t.Errorf("BoundedSubtract[%T](%[1]d, %d, %d) got %d; want %d", tt.a, tt.b, tt.floor, got, tt.want)
		}
	}
}

func TestBoundedMultiply(t *testing.T) {
	tests := []struct {
		a, b, ceil, want uint64
	}{
		{a: 2, b: 3, ceil: 10, want: 6},              // not bounded
		{a: 2, b: 3, ceil: 6, want: 6},               // a*b == ceil
		{a: 2, b: 3, ceil: 5, want: 5},               // bounded
		{a: 0, b: 5, ceil: 10, want: 0},              // a == 0
		{a: 0, b: 0, ceil: 0, want: 0},               // all zero
		{a: 1, b: 1, ceil: 0, want: 0},               // ceil == 0 bounds everything
		{a: 2, b: max, ceil: max, want: max},         // a*b would overflow uint64
		{a: max, b: max, ceil: max, want: max},       // both at max, would overflow
		{a: max, b: 2, ceil: max - 1, want: max - 1}, // a*b overflows, bounded to max-1
	}

	for _, tt := range tests {
		l, r := tt.a, tt.b
		for range 2 {
			if got := BoundedMultiply(l, r, tt.ceil); got != tt.want {
				t.Errorf("BoundedMultiply[%T](%[1]d, %d, %d) got %d; want %d", l, r, tt.ceil, got, tt.want)
			}
			l, r = r, l
		}
	}
}

func TestMulDiv(t *testing.T) {
	// Invariants:
	// wantQuo == wantQuoCeil i.f.f. wantRem == 0
	// (wantRem + wantExtra) ∈ {0, div}
	// wantRem < div && wantExtra < div
	tests := []struct {
		a, b, div              uint64
		wantQuo, wantRem       uint64
		wantQuoCeil, wantExtra uint64
	}{
		{
			a: 5, b: 2, div: 3, // 10/3
			wantQuo: 3, wantRem: 1,
			wantQuoCeil: 4, wantExtra: 2,
		},
		{
			a: 5, b: 3, div: 3, // 15/3
			wantQuo: 5, wantRem: 0,
			wantQuoCeil: 5, wantExtra: 0,
		},
		{
			a: max, b: 4, div: 8, // must avoid overflow
			wantQuo: max / 2, wantRem: 4,
			wantQuoCeil: max/2 + 1, wantExtra: 4,
		},
	}

	for _, tt := range tests {
		if gotQuo, gotRem, err := MulDiv(tt.a, tt.b, tt.div); err != nil || gotQuo != tt.wantQuo || gotRem != tt.wantRem {
			t.Errorf("MulDiv[%T](%[1]d, %d, %d) got (%d, %d, %v); want (%d, %d, nil)", tt.a, tt.b, tt.div, gotQuo, gotRem, err, tt.wantQuo, tt.wantRem)
		}
		if gotQuo, gotExtra, err := MulDivCeil(tt.a, tt.b, tt.div); err != nil || gotQuo != tt.wantQuoCeil || gotExtra != tt.wantExtra {
			t.Errorf("MulDivCeil[%T](%[1]d, %d, %d) got (%d, %d, %v) want (%d, %d, nil)", tt.a, tt.b, tt.div, gotQuo, gotExtra, err, tt.wantQuoCeil, tt.wantExtra)
		}
	}

	for name, fn := range map[string](func(_, _, _ uint64) (uint64, uint64, error)){
		"MulDiv":     MulDiv[uint64],
		"MulDivCeil": MulDivCeil[uint64],
	} {
		if _, _, err := fn(max, 2, 1); !errors.Is(err, ErrOverflow) {
			t.Errorf("%s[uint64]([max uint64], 2, 1) got error %v; want %v", name, err, ErrOverflow)
		}
	}
}

func TestCeilDiv(t *testing.T) {
	type test struct {
		num, den, want uint64
	}

	tests := []test{
		{num: 4, den: 2, want: 2},
		{num: 4, den: 1, want: 4},
		{num: 4, den: 3, want: 2},
		{num: 10, den: 3, want: 4},
		{num: max, den: 2, want: 1 << 63}, // must not overflow
	}

	rng := rand.New(rand.NewPCG(0, 0)) //nolint:gosec // Reproducibility is valuable for tests
	for range 50 {
		l := uint64(rng.Uint32())
		r := uint64(rng.Uint32())

		tests = append(tests, []test{
			{num: l*r + 1, den: l, want: r + 1},
			{num: l*r + 0, den: l, want: r},
			{num: l*r - 1, den: l, want: r},
			// l <-> r
			{num: l*r + 1, den: r, want: l + 1},
			{num: l*r + 0, den: r, want: l},
			{num: l*r - 1, den: r, want: l},
		}...)
	}

	for _, tt := range tests {
		if got := CeilDiv(tt.num, tt.den); got != tt.want {
			t.Errorf("CeilDiv[%T](%[1]d, %d) got %d; want %d", tt.num, tt.den, got, tt.want)
		}
	}
}
