// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dynamic

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// readerCase pins an exponent to the value it produces.
type readerCase[E, V ~uint64] struct {
	name        string
	exponent    E
	value       V
	skipDesired bool // set when value is produced by multiple exponents
}

// towardCase pins a single Toward step. A nil desired must leave the current
// value unchanged.
type towardCase[E ~uint64] struct {
	name    string
	current E
	desired *E
	want    E
}

func testReader[E, V ~uint64](t *testing.T, cases []readerCase[E, V], value func(E) V) {
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := value(c.exponent)
			require.Equal(t, c.value, got)
		})
	}
}

func testSearch[E, V ~uint64](t *testing.T, cases []readerCase[E, V], desired func(V) E) {
	for _, c := range cases {
		if c.skipDesired {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			got := desired(c.value)
			require.Equal(t, c.exponent, got)
		})
	}
}

func testToward[E ~uint64](t *testing.T, cases []towardCase[E], toward func(E, *E) E) {
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := toward(c.current, c.desired)
			require.Equal(t, c.want, got)
		})
	}
}

// fuzzToward asserts that toward stays within [current, desired] and moves
// strictly when desired differs from current.
func fuzzToward[E ~uint64](
	f *testing.F,
	seeds []towardCase[E],
	toward func(E, *E) E,
) {
	f.Helper()

	for _, s := range seeds {
		if s.desired != nil {
			f.Add(uint64(s.current), uint64(*s.desired))
		}
	}
	f.Fuzz(func(t *testing.T, rawCurrent, rawDesired uint64) {
		current, desired := E(rawCurrent), E(rawDesired)

		require.Equal(t, current, toward(current, nil))

		got := toward(current, &desired)
		require.GreaterOrEqual(t, got, min(current, desired))
		require.LessOrEqual(t, got, max(current, desired))
		if current != desired {
			require.NotEqual(t, current, got)
		}
	})
}

// fuzzSearch asserts that desired(v) is the smallest exponent whose value
// reaches v.
func fuzzSearch[E, V ~uint64](
	f *testing.F,
	seeds []readerCase[E, V],
	desired func(V) E,
	value func(E) V,
) {
	f.Helper()

	for _, s := range seeds {
		f.Add(uint64(s.value))
	}
	f.Fuzz(func(t *testing.T, rawV uint64) {
		v := V(rawV)
		got := desired(v)
		require.GreaterOrEqual(t, value(got), v)
		if got > 0 {
			require.Less(t, value(got-1), v)
		}
	})
}
