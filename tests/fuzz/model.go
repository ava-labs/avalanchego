// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

// lifecycle is the state of an entity the fuzzer committed to the chain.
type lifecycle uint8

const (
	// active means the entity exists on-chain now.
	active lifecycle = iota
	// ended means the chain terminated the entity on its own: a staker's time
	// passed, an L1 validator's balance drained, an atomic UTXO was consumed.
	ended
	// removed means a committed tx terminated the entity.
	removed
)

// A registry holds the entities of one type that the fuzzer committed, each
// with a [lifecycle] state. An op draws a reference from it, biased to a state
// so the op can reach active, ended, or removed references. The zero value is
// ready to use, and a registry is not safe for concurrent use.
//
// The registry records identity and lifecycle, not value: it does not re-derive
// balances or rewards, so it cannot share the executor's bugs. The invariant
// oracles judge value.
type registry[K comparable, E any] struct {
	items map[K]*tracked[E]
	order []K // keys in insertion order, so draw is deterministic
}

type tracked[E any] struct {
	value E
	state lifecycle
}

// put records e under k as [active]. A second put with the same key replaces
// the entity and resets it to active.
func (r *registry[K, E]) put(k K, e E) {
	if r.items == nil {
		r.items = make(map[K]*tracked[E])
	}
	if _, ok := r.items[k]; !ok {
		r.order = append(r.order, k)
	}
	r.items[k] = &tracked[E]{value: e, state: active}
}

// transition moves k to state s. It does nothing if k is unknown.
func (r *registry[K, E]) transition(k K, s lifecycle) {
	if t, ok := r.items[k]; ok {
		t.state = s
	}
}

// get returns the entity under k and its state, and whether k is known.
func (r *registry[K, E]) get(k K) (E, lifecycle, bool) {
	if t, ok := r.items[k]; ok {
		return t.value, t.state, true
	}

	var zero E
	return zero, active, false
}

// draw returns the key and entity that arg selects among the entities in state
// s, and whether any entity is in state s. The selection is stable under
// insertion order, so a replayed trace draws the same entity.
func (r *registry[K, E]) draw(s lifecycle, arg uint64) (K, E, bool) {
	var keys []K
	for _, k := range r.order {
		if r.items[k].state == s {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		var (
			zk K
			ze E
		)
		return zk, ze, false
	}

	k := keys[arg%uint64(len(keys))]
	return k, r.items[k].value, true
}

// keys returns the keys of the entities in state s, in insertion order.
// reconcile uses it to find the active entities to re-check.
func (r *registry[K, E]) keys(s lifecycle) []K {
	var keys []K
	for _, k := range r.order {
		if r.items[k].state == s {
			keys = append(keys, k)
		}
	}
	return keys
}

// all returns every entity, in any state, in insertion order. A check that
// must account for an entity whatever its state, such as summing L1 balances,
// uses it.
func (r *registry[K, E]) all() []E {
	out := make([]E, 0, len(r.order))
	for _, k := range r.order {
		out = append(out, r.items[k].value)
	}
	return out
}
