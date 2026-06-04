// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dynamic

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// readerCase pins the value an exponent maps to. skipDesired marks values that
// several exponents share, so the desired function need not return this exact
// exponent.
type readerCase[E, V ~uint64] struct {
	name        string
	exponent    E
	value       V
	skipDesired bool
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

func testDesired[E, V ~uint64](t *testing.T, cases []readerCase[E, V], desired func(V) E) {
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

// fuzzToward checks Toward's invariants for any input: a nil desired is a no-op,
// and otherwise the result stays in [current, desired] and differs from current
// when desired does, which together mean it moved strictly toward desired.
func fuzzToward[E ~uint64](t *testing.T, current, desired E, toward func(E, *E) E) {
	t.Helper()
	require.Equal(t, current, toward(current, nil))

	got := toward(current, &desired)
	require.GreaterOrEqual(t, got, min(current, desired))
	require.LessOrEqual(t, got, max(current, desired))
	if current != desired {
		require.NotEqual(t, current, got)
	}
}

// fuzzDesired exercises the property that desired(v) is the smallest exponent
// whose value reaches v.
func fuzzDesired[E, V ~uint64](t *testing.T, v V, desired func(V) E, value func(E) V) {
	t.Helper()
	got := desired(v)
	require.GreaterOrEqual(t, value(got), v)
	if got > 0 {
		require.Less(t, value(got-1), v)
	}
}
