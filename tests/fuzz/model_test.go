// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegistry(t *testing.T) {
	t.Run("zero_value_draws_nothing", func(t *testing.T) {
		var r registry[int, string]
		_, _, ok := r.draw(active, 0)
		require.False(t, ok)
	})

	t.Run("put_then_draw_active", func(t *testing.T) {
		var r registry[int, string]
		r.put(1, "a")
		r.put(2, "b")

		k, v, ok := r.draw(active, 0)
		require.True(t, ok)
		require.Equal(t, 1, k)
		require.Equal(t, "a", v)

		// arg selects by modulo over insertion order.
		k, v, ok = r.draw(active, 1)
		require.True(t, ok)
		require.Equal(t, 2, k)
		require.Equal(t, "b", v)

		k, _, ok = r.draw(active, 2)
		require.True(t, ok)
		require.Equal(t, 1, k, "arg wraps by modulo")
	})

	t.Run("transition_splits_the_states", func(t *testing.T) {
		var r registry[int, string]
		r.put(1, "a")
		r.put(2, "b")
		r.transition(1, ended)

		_, _, ok := r.draw(active, 0)
		require.True(t, ok)
		k, _, ok := r.draw(active, 0)
		require.True(t, ok)
		require.Equal(t, 2, k, "only the untransitioned entity is active")

		k, v, ok := r.draw(ended, 0)
		require.True(t, ok)
		require.Equal(t, 1, k)
		require.Equal(t, "a", v)

		_, _, ok = r.draw(removed, 0)
		require.False(t, ok)
	})

	t.Run("transition_unknown_key_is_a_noop", func(t *testing.T) {
		var r registry[int, string]
		r.put(1, "a")
		r.transition(99, removed)

		_, state, ok := r.get(1)
		require.True(t, ok)
		require.Equal(t, active, state)
	})

	t.Run("get_reports_state_and_presence", func(t *testing.T) {
		var r registry[int, string]
		r.put(1, "a")
		r.transition(1, removed)

		v, state, ok := r.get(1)
		require.True(t, ok)
		require.Equal(t, "a", v)
		require.Equal(t, removed, state)

		_, _, ok = r.get(2)
		require.False(t, ok)
	})

	t.Run("put_replaces_and_resets_to_active", func(t *testing.T) {
		var r registry[int, string]
		r.put(1, "a")
		r.transition(1, ended)
		r.put(1, "a2")

		v, state, ok := r.get(1)
		require.True(t, ok)
		require.Equal(t, "a2", v)
		require.Equal(t, active, state, "a re-put entity is active again")
		require.Len(t, r.keys(active), 1, "replacing a key does not duplicate it")
	})

	t.Run("keys_lists_a_state_in_order", func(t *testing.T) {
		var r registry[int, string]
		r.put(3, "c")
		r.put(1, "a")
		r.put(2, "b")
		r.transition(1, ended)

		require.Equal(t, []int{3, 2}, r.keys(active))
		require.Equal(t, []int{1}, r.keys(ended))
	})
}
