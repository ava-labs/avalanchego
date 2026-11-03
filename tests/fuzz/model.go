// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"fmt"
	"slices"

	"pgregory.net/rapid"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/eventlog"
	"github.com/ava-labs/avalanchego/utils/set"
)

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

func (l lifecycle) String() string {
	switch l {
	case active:
		return "active"
	case ended:
		return "ended"
	case removed:
		return "removed"
	default:
		return fmt.Sprintf("lifecycle(%d)", uint8(l))
	}
}

// A registry holds the keys of the entities of one kind that the run has
// seen, each with a [lifecycle] state. It keeps an entity after it ends or is
// removed, so the ID rule (drawID) can reach it. The zero value is ready to
// use, and a registry is not safe for concurrent use.
//
// The registry records identity and lifecycle, not value: it does not re-derive
// balances or rewards, so it cannot share the executor's bugs. The invariant
// oracles judge value.
type registry[K comparable] struct {
	states map[K]lifecycle
	order  []K // keys in insertion order, so a replay draws the same entity
}

// put records k as [active]. A key put again, after it ended or was removed,
// is active again.
func (r *registry[K]) put(k K) {
	if r.states == nil {
		r.states = make(map[K]lifecycle)
	}
	if _, ok := r.states[k]; !ok {
		r.order = append(r.order, k)
	}
	r.states[k] = active
}

// transition moves k to state s. It does nothing if k is unknown.
func (r *registry[K]) transition(k K, s lifecycle) {
	if _, ok := r.states[k]; ok {
		r.states[k] = s
	}
}

// state returns k's state, and whether k is known.
func (r *registry[K]) state(k K) (lifecycle, bool) {
	s, ok := r.states[k]
	return s, ok
}

// keys returns the keys in state s, in insertion order.
func (r *registry[K]) keys(s lifecycle) []K {
	var keys []K
	for _, k := range r.order {
		if r.states[k] == s {
			keys = append(keys, k)
		}
	}

	return keys
}

// id is a Go ID type: [ids.ID], [ids.NodeID], or [ids.ShortID].
type id interface {
	ids.ID | ids.NodeID | ids.ShortID
	String() string
}

// A seenSet holds every value of one Go type that the run has seen, such as
// IDs of any kind and in any lifecycle state, in the order first seen. The
// zero value is ready to use.
type seenSet[K comparable] struct {
	order []K
	has   set.Set[K]
}

// add records ks. An ID already seen keeps its place.
func (s *seenSet[K]) add(ks ...K) {
	for _, k := range ks {
		if !s.has.Contains(k) {
			s.has.Add(k)
			s.order = append(s.order, k)
		}
	}
}

// A pool holds every value of one Go type that the run has seen: all of them,
// and by field path those seen at each path, such as
// "AddSubnetValidatorTx.SubnetValidator.Subnet". A path has "[]" in place of
// list indexes. The zero value is ready to use.
type pool[V comparable] struct {
	all   seenSet[V]
	paths map[string]*seenSet[V]
}

// add records that v was seen at path.
func (p *pool[V]) add(path string, v V) {
	p.all.add(v)
	if p.paths == nil {
		p.paths = make(map[string]*seenSet[V])
	}
	s, ok := p.paths[path]
	if !ok {
		s = &seenSet[V]{}
		p.paths[path] = s
	}
	s.add(v)
}

// at returns the values seen at path, in the order first seen.
func (p *pool[V]) at(path string) []V {
	if s, ok := p.paths[path]; ok {
		return s.order
	}

	return nil
}

// drawID draws an ID for a field by the ID rule (Stateful Fuzzing.md 2):
// one of active, the active entities of the field's kind, 5 times in 6;
// otherwise, half and half, any ID in seen or a fresh one. It never skips, so
// an op may draw an ID before or after its other draws. An empty kind falls
// through to seen or drawT, and the node or the wallet then decides.
//
// The active branch wins on 0 to 4 and loses on 5, not the other way round:
// rapid leans toward small values, so this keeps the split near 5 in 6 (about
// 85 in 100), and an input of zeros, which some tests replay, draws an active
// entity.
//
// It adds the draw to ev's draws, named by where it came from: "<label> #i"
// for the i-th active entity, "seen #i" for the i-th seen ID, or "fresh".
func drawID[K id](t *rapid.T, ev *eventlog.Event, label string, active []K, seen *seenSet[K]) K {
	var (
		k   K
		ref string
	)
	switch {
	case len(active) > 0 && rapid.IntRange(0, 5).Draw(t, label+"Branch") < 5:
		k = rapid.SampledFrom(active).Draw(t, label)
		ref = fmt.Sprintf("%s #%d", label, slices.Index(active, k))
	case len(seen.order) > 0 && rapid.Bool().Draw(t, label+"Seen"):
		k = rapid.SampledFrom(seen.order).Draw(t, label)
		ref = fmt.Sprintf("seen #%d", slices.Index(seen.order, k))
	default:
		k, ref = drawT[K](t, label), "fresh"
	}
	ev.Append("draws", draw{Label: label, Ref: ref, ID: k.String()})

	return k
}

// drawT returns an ID made of bytes that t draws, so the logged program holds
// it.
func drawT[K id](t *rapid.T, label string) K {
	var k K

	return K(rapid.SliceOfN(rapid.Byte(), len(k), len(k)).Draw(t, label))
}
