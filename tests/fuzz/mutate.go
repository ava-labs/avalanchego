// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"context"
	"encoding/hex"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// mutateOp runs an issue op as the next step, with a new tx of proto's type
// that has one field mutated (Stateful Fuzzing.md 2.2). It counts the outcome
// under the type and the mutation, so the counts show which mutation reached
// which check.
func (r *runner) mutateOp(t *rapid.T, proto platform.UnsignedTx) {
	r.runOp(t, func(ctx context.Context, t *rapid.T) (string, outcome) {
		stat := "issue " + txName(proto) + " mutated"
		r.ev.Set("source", newTx.String())
		r.ev.Set("tx_type", txName(proto))
		utx, err := r.build(t, proto)
		if err != nil {
			return stat, r.skip(t, err)
		}

		tx, m := r.mutate(t, utx)
		r.ev.Set("mutation", m)

		stat += " " + m.label()
		return stat, r.issueSigned(ctx, t, tx, stat, "")
	})
}

// drawMutated draws whether issue mutates its new tx. Most txs stay valid, so
// they build the states that the mutated ones probe.
func drawMutated(t *rapid.T) bool {
	return uniform(3).Draw(t, "mutate") == 0
}

// A tier is the source that a mutation draws a field's new value from
// (Stateful Fuzzing.md 2.2).
type tier uint8

const (
	live       tier = iota // the model now: an active entity, a threshold, the actors
	neighbor               // next to a current value: ±1, or a list with one element dropped, duplicated, or swapped
	historical             // a value at the same field of an older tx, or an entity that is gone
	known                  // a value seen at any field
	random                 // fresh bytes, or an integer's edges
	tierCount
)

func (tr tier) String() string {
	return [...]string{
		live:       "live",
		neighbor:   "neighbor",
		historical: "historical",
		known:      "known",
		random:     "random",
	}[tr]
}

// drawTier draws a tier that has a source, by the tiers' weights. The weights
// are a start: the reach counts will move them (Stateful Fuzzing.md 3.1).
func drawTier(t *rapid.T, sources [tierCount]bool) tier {
	weights := [tierCount]int{live: 50, neighbor: 20, historical: 15, known: 10, random: 5}
	var total int
	for tr, ok := range sources {
		if ok {
			total += weights[tr]
		}
	}

	i := uniform(total).Draw(t, "tier")
	for tr, ok := range sources {
		if !ok {
			continue
		}
		if i < weights[tr] {
			return tier(tr)
		}
		i -= weights[tr]
	}
	panic("drawTier: no tier has a source")
}

// A repair is what mutate fixes after it changes the unsigned tx, as Fluffy
// does. Without it almost every mutation stops at the signature check.
// Skipping a part is a mutation of its own, drawn rarely.
type repair uint8

const (
	fullRepair  repair = iota // sign again, pay a larger fee, and sort the inputs and outputs
	noResign                  // keep the signatures of the tx before the mutation
	noFeeRepair               // pay the fee of the tx before the mutation
)

func (rp repair) String() string {
	return [...]string{
		fullRepair:  "full",
		noResign:    "no re-sign",
		noFeeRepair: "no fee repair",
	}[rp]
}

// drawRepair skips a part of the repair 1 draw in 10, each part equally
// often.
func drawRepair(t *rapid.T) repair {
	switch uniform(20).Draw(t, "repair") {
	case 0:
		return noResign
	case 1:
		return noFeeRepair
	default:
		return fullRepair
	}
}

// A mutation is the one change that mutate made to a valid tx. The op's event
// logs it.
type mutation struct {
	Path   string `json:"path"`
	Tier   string `json:"tier"`
	Old    string `json:"old"`
	New    string `json:"new"`
	Repair string `json:"repair,omitempty"` // empty for a credential, which is not repaired

	key string // Path with "[]" for each list index
}

// label names m in the outcome counts: the field without its tx type or list
// indexes, the tier, and the repair if part of it was skipped.
func (m mutation) label() string {
	l := m.key[strings.IndexByte(m.key, '.')+1:] + " " + m.Tier
	if m.Repair != "" && m.Repair != fullRepair.String() {
		l += ", " + m.Repair
	}

	return l
}

// mutate returns a signed copy of utx with one field changed, and the
// mutation. The field is any field of the unsigned tx or its credentials, and
// the tier is one that has a source for it. After a change to the unsigned tx,
// mutate repairs the rest, unless it draws a repair that skips a part. A
// signer that cannot sign the mutated tx, such as for a subnet the wallet does
// not know, leaves it with the old signatures. utx does not change.
func (r *runner) mutate(t *rapid.T, utx platform.UnsignedTx) (*platform.Tx, mutation) {
	orig := r.sign(t, utx)
	// Parse a copy, since the mutation writes in place and payFee needs utx's
	// fee from before it.
	tx, err := platform.ParseTx(platform.Codec, orig.Bytes())
	require.NoError(t, err)

	fields := mutableFields(tx)
	f := fields[uniform(len(fields)).Draw(t, "field")]
	draws := r.tiers(t, f, utx)
	var sources [tierCount]bool
	for tr, d := range draws {
		sources[tr] = d != nil
	}
	tr := drawTier(t, sources)
	m := mutation{Path: f.path, Tier: tr.String(), Old: render(f), key: f.key}
	m.New = draws[tr](t)
	if f.cred {
		require.NoError(t, tx.Initialize(platform.Codec))

		return tx, m
	}

	rp := drawRepair(t)
	mutated := clone(t, tx.Unsigned)
	if rp != noFeeRepair {
		r.payFee(utx, mutated, f.path)
	}
	sortLists(mutated, f.path)
	signed, err := r.signTx(t.Context(), mutated)
	if rp == noResign || err != nil {
		rp = noResign
		signed = &platform.Tx{Unsigned: mutated, Creds: orig.Creds}
		require.NoError(t, signed.Initialize(platform.Codec))
	}
	m.Repair = rp.String()

	return signed, m
}

// clone returns a copy of utx that shares no memory with it. After a list
// mutation duplicates an element, a change to one copy then changes only that
// copy.
func clone(t *rapid.T, utx platform.UnsignedTx) platform.UnsignedTx {
	b, err := platform.Codec.Marshal(platform.CodecVersion, &utx)
	require.NoError(t, err)

	var c platform.UnsignedTx
	_, err = platform.Codec.Unmarshal(b, &c)
	require.NoError(t, err)

	return c
}

// payFee lowers the largest change output of after by what its fee grew over
// before's, so that it still pays the fee once a mutation changed its size.
// It leaves alone the output that the mutation at path changed, and does
// nothing if no other output can pay.
func (r *runner) payFee(before, after platform.UnsignedTx, path string) {
	calc := fee.NewDynamicCalculator(r.pContext.ComplexityWeights, r.pContext.GasPrice)
	was, err := calc.CalculateFee(before)
	if err != nil {
		return // a proposer-only tx pays no fee
	}
	is, err := calc.CalculateFee(after)
	if err != nil || is <= was || strings.HasSuffix(path, ".Outs") {
		return
	}

	var change *secp256k1fx.TransferOutput
	for i, out := range after.Outputs() {
		o, ok := out.Out.(*secp256k1fx.TransferOutput)
		if !ok || out.AssetID() != r.avaxAssetID || !r.owned(o) || strings.Contains(path+".", fmt.Sprintf(".Outs[%d].", i)) {
			continue
		}
		if change == nil || o.Amt > change.Amt {
			change = o
		}
	}
	if change != nil && change.Amt >= is-was {
		change.Amt -= is - was
	}
}

// sortLists sorts each list of inputs and outputs in utx, as the wallet does,
// except the list at path: sorting would undo a mutation that reordered it.
func sortLists(utx platform.UnsignedTx, path string) {
	name := txName(utx)
	walk(reflect.ValueOf(utx), name, name, serialized, func(f field) {
		if f.kind != listField || f.path == path {
			return
		}

		switch l := f.v.Interface().(type) {
		case []*avax.TransferableOutput:
			avax.SortTransferableOutputs(l, platform.Codec)
		case []*avax.TransferableInput:
			utils.Sort(l)
		}
	})
}

// fieldKind is what a mutation treats a field as.
type fieldKind uint8

const (
	idField    fieldKind = iota // an [ids.ID], [ids.NodeID], or [ids.ShortID]
	intField                    // an unsigned integer
	listField                   // a list with at least one element
	ownerField                  // an [secp256k1fx.OutputOwners] or a [message.PChainOwner]
	sigField                    // a secp256k1 signature in a credential
)

// A field is a value inside a tx that a mutation can change.
type field struct {
	path string // such as "BaseTx.BaseTx.Outs[1].Out.Amt", led by the tx type
	key  string // path with "[]" for each list index; pools are keyed by it
	v    reflect.Value
	kind fieldKind
	cred bool // in the credentials, not the unsigned tx
}

// mutableFields returns the fields of tx that a mutation can change: those of
// its unsigned tx, then those of its credentials.
func mutableFields(tx *platform.Tx) []field {
	var fields []field
	keep := func(cred bool) func(field) {
		return func(f field) {
			if f.v.CanSet() {
				f.cred = cred
				fields = append(fields, f)
			}
		}
	}

	name := txName(tx.Unsigned)
	walk(reflect.ValueOf(tx.Unsigned), name, name, serialized, keep(false))
	creds := name + ".Creds"
	walk(reflect.ValueOf(&tx.Creds).Elem(), creds, creds, serialized, keep(true))

	return fields
}

// walk calls visit on each field in v: each ID, unsigned integer, non-empty
// list, owner, and signature. It reaches through pointers, interfaces, lists,
// and the struct fields that follow accepts. An owner is a field, and so is
// each value in it. A byte list is one field: its bytes are not.
func walk(v reflect.Value, path, key string, follow func(reflect.StructField) bool, visit func(field)) {
	f := field{path: path, key: key, v: v}
	switch typ := v.Type(); typ {
	case reflect.TypeFor[ids.ID](), reflect.TypeFor[ids.NodeID](), reflect.TypeFor[ids.ShortID]():
		f.kind = idField
		visit(f)

		return
	case reflect.TypeFor[[secp256k1.SignatureLen]byte]():
		f.kind = sigField
		visit(f)

		return
	case reflect.TypeFor[secp256k1fx.OutputOwners](), reflect.TypeFor[message.PChainOwner]():
		f.kind = ownerField
		visit(f)
	}

	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			walk(v.Elem(), path, key, follow, visit)
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if sf := v.Type().Field(i); follow(sf) {
				walk(v.Field(i), path+"."+sf.Name, key+"."+sf.Name, follow, visit)
			}
		}
	case reflect.Slice:
		if v.Len() == 0 {
			return
		}
		f.kind = listField
		visit(f)
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return
		}
		for i := range v.Len() {
			walk(v.Index(i), fmt.Sprintf("%s[%d]", path, i), key+"[]", follow, visit)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		f.kind = intField
		visit(f)
	}
}

// serialized accepts the struct fields that the codec writes.
func serialized(f reflect.StructField) bool {
	return f.Tag.Get("serialize") == "true"
}

// observe adds f's value to the pool of its Go type, at f's key.
func (r *runner) observe(f field) {
	switch f.kind {
	case idField:
		switch id := f.v.Interface().(type) {
		case ids.ID:
			r.seenIDs.add(f.key, id)
		case ids.NodeID:
			r.seenNodeIDs.add(f.key, id)
		case ids.ShortID:
			r.seenShortIDs.add(f.key, id)
		}
	case intField:
		r.seenInts.add(f.key, f.v.Uint())
	case ownerField:
		r.seenOwners.add(f.key, ownerOf(f.v))
	case sigField:
		r.seenSigs.add(f.key, f.v.Interface().([secp256k1.SignatureLen]byte))
	}
}

// render returns f's value as the event log shows it.
func render(f field) string {
	switch f.kind {
	case idField:
		return fmt.Sprint(f.v.Interface())
	case intField:
		return strconv.FormatUint(f.v.Uint(), 10)
	case ownerField:
		return ownerOf(f.v).String()
	case listField:
		return fmt.Sprintf("%d elements", f.v.Len())
	default:
		return fmt.Sprintf("%x", f.v.Interface())
	}
}

// A tierDraw sets a field to a value from one tier's source, and returns the
// value as the event log shows it.
type tierDraw func(*rapid.T) string

// tiers returns, for each tier, how to draw f's new value from that tier's
// source, or nil when the source holds nothing for f. utx is the unsigned tx
// that holds f, as it was before the mutation.
func (r *runner) tiers(t *rapid.T, f field, utx platform.UnsignedTx) [tierCount]tierDraw {
	switch f.kind {
	case idField:
		switch f.v.Interface().(type) {
		case ids.ID:
			return idTiers(t, r, f, &r.seenIDs)
		case ids.NodeID:
			return idTiers(t, r, f, &r.seenNodeIDs)
		default:
			return idTiers(t, r, f, &r.seenShortIDs)
		}
	case intField:
		return intDraws(f, r.thresholds(f, utx), r.seenInts.at(f.key), r.seenInts.all.order)
	case listField:
		return r.listTiers(f)
	case ownerField:
		return r.ownerTiers(f)
	default:
		return r.sigTiers(f, utx.Bytes())
	}
}

func idTiers[K id](t *rapid.T, r *runner, f field, seen *pool[K]) [tierCount]tierDraw {
	set := func(k K) string {
		f.v.Set(reflect.ValueOf(k))

		return k.String()
	}
	current, gone := r.entities(t, f.key)

	var draws [tierCount]tierDraw
	if ks := ofType[K](current); len(ks) > 0 {
		draws[live] = func(t *rapid.T) string { return set(pick(t, ks, "live")) }
	}
	if ks := slices.Concat(seen.at(f.key), ofType[K](gone)); len(ks) > 0 {
		draws[historical] = func(t *rapid.T) string { return set(pick(t, ks, "historical")) }
	}
	if ks := seen.all.order; len(ks) > 0 {
		draws[known] = func(t *rapid.T) string { return set(pick(t, ks, "known")) }
	}
	draws[random] = func(t *rapid.T) string { return set(drawT[K](t, "random")) }

	return draws
}

// thresholds returns the values that the validity of the integer f in utx
// turns on, when the model knows them: those of its key (threshold), and for
// an output amount the two values at which utx stops paying its fee at the
// minimum gas price (checkFee). The wallet pays twice the gas price it read,
// so an output can take that much more before the fee check turns.
func (r *runner) thresholds(f field, utx platform.UnsignedTx) []uint64 {
	if v, ok := r.threshold(f.key); ok {
		return []uint64{v}
	}

	name := f.key[strings.LastIndexByte(f.key, '.')+1:]
	output := strings.Contains(f.key, ".Outs[]") || strings.Contains(f.key, ".ExportedOutputs[]")
	if name != "Amt" || !output {
		return nil
	}
	want, err := r.fees.CalculateFee(utx)
	if err != nil {
		return nil // a proposer-only tx pays no fee
	}
	v, err := valueOf(utx)
	if err != nil {
		return nil
	}
	burned, err := v.burned(r.avaxAssetID)
	if err != nil || burned < want {
		return nil
	}

	pays := f.v.Uint() + burned - want

	return []uint64{pays, pays + 1}
}

// intDraws draws an integer of f's size. thresholds are the values that
// validity turns on: the live tier sets one, and the neighbor tier draws next
// to one or to f's own value. The historical tier draws from past, and the
// known tier from seen. A value that overflows f wraps.
func intDraws(f field, thresholds, past, seen []uint64) [tierCount]tierDraw {
	bits := f.v.Type().Bits()
	maxValue := ^uint64(0) >> (64 - bits)
	set := func(v uint64) string {
		f.v.SetUint(v & maxValue)

		return strconv.FormatUint(v&maxValue, 10)
	}
	fitting := func(vs []uint64) []uint64 {
		return slices.DeleteFunc(slices.Clone(vs), func(v uint64) bool { return v > maxValue })
	}
	own := f.v.Uint()

	var draws [tierCount]tierDraw
	draws[live] = func(t *rapid.T) string {
		if len(thresholds) > 0 {
			return set(pick(t, thresholds, "threshold"))
		}

		return set(own)
	}
	draws[neighbor] = func(t *rapid.T) string {
		base := own
		if len(thresholds) > 0 && rapid.Bool().Draw(t, "nearThreshold") {
			base = pick(t, thresholds, "threshold")
		}
		offset := uint64(1)
		if rapid.Bool().Draw(t, "farther") {
			offset = uint64(rapid.IntRange(2, 16).Draw(t, "offset"))
		}
		if rapid.Bool().Draw(t, "below") {
			return set(base - offset)
		}

		return set(base + offset)
	}
	if vs := fitting(past); len(vs) > 0 {
		draws[historical] = func(t *rapid.T) string { return set(pick(t, vs, "historical")) }
	}
	if vs := fitting(seen); len(vs) > 0 {
		draws[known] = func(t *rapid.T) string { return set(pick(t, vs, "known")) }
	}
	draws[random] = func(t *rapid.T) string {
		switch uniform(4).Draw(t, "edge") {
		case 0:
			return set(0)
		case 1:
			return set(maxValue)
		case 2:
			return set(maxValue - 1)
		default:
			return set(uniformBits(t, bits))
		}
	}

	return draws
}

// uniformBits returns an integer of the given bits, each bit a fair draw:
// rapid's own integers lean toward small values.
func uniformBits(t *rapid.T, bits int) uint64 {
	var v uint64
	for b := range bits {
		if rapid.Bool().Draw(t, "bit") {
			v |= 1 << b
		}
	}

	return v
}

// listTiers has the neighbor tier: drop, duplicate, or swap one element. A
// list of inputs also has the live tier while a tx is pending: swap one input
// for an input of a pending tx (doubleSpend).
func (r *runner) listTiers(f field) [tierCount]tierDraw {
	var draws [tierCount]tierDraw
	if ins, ok := f.v.Interface().([]*avax.TransferableInput); ok {
		if spent := r.pendingTxInputs(f.key); len(spent) > 0 {
			draws[live] = func(t *rapid.T) string { return doubleSpend(t, ins, spent) }
		}
	}

	draws[neighbor] = func(t *rapid.T) string {
		n := f.v.Len()
		i := uniform(n).Draw(t, "element")
		ops := 2
		if n > 1 {
			ops++ // a swap needs two elements
		}

		switch uniform(ops).Draw(t, "listOp") {
		case 0:
			f.v.Set(reflect.AppendSlice(f.v.Slice(0, i), f.v.Slice(i+1, n)))

			return fmt.Sprintf("dropped element %d", i)
		case 1:
			dup := reflect.MakeSlice(f.v.Type(), 0, n+1)
			dup = reflect.AppendSlice(dup, f.v.Slice(0, i+1))
			f.v.Set(reflect.AppendSlice(dup, f.v.Slice(i, n)))

			return fmt.Sprintf("duplicated element %d", i)
		default:
			j := (i + 1 + uniform(n-1).Draw(t, "other")) % n
			reflect.Swapper(f.v.Interface())(i, j)

			return fmt.Sprintf("swapped elements %d and %d", i, j)
		}
	}

	return draws
}

// pendingTxInputs returns the inputs of the pending P-Chain txs that sit in
// lists of the same name as the list at key, Ins or ImportedInputs, so that a
// P-Chain input takes a P-Chain input and an import an atomic one. They come
// in the order the txs went out.
func (r *runner) pendingTxInputs(key string) []*avax.TransferableInput {
	name := key[strings.LastIndexByte(key, '.')+1:]

	var inputs []*avax.TransferableInput
	for _, p := range r.pending {
		if p.tx == nil {
			continue
		}

		utx := txName(p.tx.Unsigned)
		walk(reflect.ValueOf(p.tx.Unsigned), utx, utx, serialized, func(f field) {
			ins, ok := f.v.Interface().([]*avax.TransferableInput)
			if ok && f.kind == listField && strings.HasSuffix(f.key, "."+name) {
				inputs = append(inputs, ins...)
			}
		})
	}

	return inputs
}

// doubleSpend swaps a drawn input of ins for a copy of a drawn input of
// spent, the inputs of pending txs, and sorts ins again, as the codec
// requires; sortLists leaves the mutated list alone. It draws an input that
// holds at least as much as the one it replaces, if there is one, so the tx
// still pays its fee and burns the rest, and the double spend stays valid.
// The spent-once invariant judges whichever txs the nodes accept (Stateful
// Fuzzing.md 2.1).
func doubleSpend(t *rapid.T, ins, spent []*avax.TransferableInput) string {
	i := uniform(len(ins)).Draw(t, "element")
	enough := slices.DeleteFunc(slices.Clone(spent), func(in *avax.TransferableInput) bool {
		return in.AssetID() != ins[i].AssetID() || in.In.Amount() < ins[i].In.Amount()
	})
	if len(enough) == 0 {
		enough = spent
	}
	in := pick(t, enough, "pendingInput")

	b, err := platform.Codec.Marshal(platform.CodecVersion, in)
	require.NoError(t, err)
	var cp avax.TransferableInput
	_, err = platform.Codec.Unmarshal(b, &cp)
	require.NoError(t, err)

	ins[i] = &cp
	utils.Sort(ins)

	return fmt.Sprintf("element %d spends %s, as a pending tx does", i, in.InputID())
}

func (r *runner) ownerTiers(f field) [tierCount]tierDraw {
	set := func(o ownerValue) string {
		o.set(f.v)

		return o.String()
	}

	var draws [tierCount]tierDraw
	draws[live] = func(t *rapid.T) string {
		o := r.multisig(t)

		return set(newOwnerValue(0, o.Threshold, o.Addrs))
	}
	if owners := r.seenOwners.at(f.key); len(owners) > 0 {
		draws[historical] = func(t *rapid.T) string { return set(pick(t, owners, "historical")) }
	}
	if owners := r.seenOwners.all.order; len(owners) > 0 {
		draws[known] = func(t *rapid.T) string { return set(pick(t, owners, "known")) }
	}
	draws[random] = func(t *rapid.T) string {
		if !rapid.Bool().Draw(t, "garbage") {
			return set(newOwnerValue(0, 1, []ids.ShortID{drawT[ids.ShortID](t, "key")}))
		}

		addrs := make([]ids.ShortID, rapid.IntRange(0, 3).Draw(t, "addrs"))
		for i := range addrs {
			addrs[i] = drawT[ids.ShortID](t, "addr")
		}

		return set(newOwnerValue(uniformBits(t, 64), uint32(uniformBits(t, 32)), addrs))
	}

	return draws
}

func (r *runner) sigTiers(f field, unsigned []byte) [tierCount]tierDraw {
	sign := func(t *rapid.T, key interface{ Sign([]byte) ([]byte, error) }) string {
		b, err := key.Sign(unsigned)
		require.NoError(t, err)

		sig := [secp256k1.SignatureLen]byte(b)
		f.v.Set(reflect.ValueOf(sig))

		return hex.EncodeToString(sig[:])
	}
	set := func(sig [secp256k1.SignatureLen]byte) string {
		f.v.Set(reflect.ValueOf(sig))

		return hex.EncodeToString(sig[:])
	}

	var draws [tierCount]tierDraw
	draws[live] = func(t *rapid.T) string {
		key, ok := r.keys.Get(rapid.SampledFrom(r.actors).Draw(t, "signer"))
		require.True(t, ok)
		return sign(t, key)
	}
	if sigs := r.seenSigs.at(f.key); len(sigs) > 0 {
		draws[historical] = func(t *rapid.T) string { return set(pick(t, sigs, "historical")) }
	}
	if sigs := r.seenSigs.all.order; len(sigs) > 0 {
		draws[known] = func(t *rapid.T) string { return set(pick(t, sigs, "known")) }
	}
	draws[random] = func(t *rapid.T) string {
		if rapid.Bool().Draw(t, "garbage") {
			return set([secp256k1.SignatureLen]byte(rapid.SliceOfN(rapid.Byte(), secp256k1.SignatureLen, secp256k1.SignatureLen).Draw(t, "sig")))
		}

		// A key of zero cannot sign, and rapid leans toward zeros.
		b := rapid.SliceOfN(rapid.Byte(), secp256k1.PrivateKeyLen, secp256k1.PrivateKeyLen).Draw(t, "key")
		b[len(b)-1] |= 1
		key, err := secp256k1.ToPrivateKey(b)
		require.NoError(t, err)
		return sign(t, key)
	}

	return draws
}

// entities returns the active entities of the kind that the ID field at key
// holds, and those that ended or were removed: the map from ID fields to
// registries, which only the live and historical tiers need. A field of no
// kind that the model holds has neither.
func (r *runner) entities(t *rapid.T, key string) (current, gone []any) {
	switch name := key[strings.LastIndexByte(key, '.')+1:]; {
	case name == "Subnet" || name == "SubnetID":
		return anys(r.subnets.keys(active)), anys(slices.Concat(r.subnets.keys(ended), r.subnets.keys(removed)))
	case strings.HasSuffix(key, ".UTXOID.TxID"):
		chainID := constants.PlatformChainID
		if strings.Contains(key, ".ImportedInputs[]") {
			chainID = r.xChainID
		}
		utxos, err := r.utxos.UTXOs(t.Context(), chainID, constants.PlatformChainID)
		require.NoError(t, err)

		// The model holds UTXOs in a map, and a replay must draw the same one.
		slices.SortFunc(utxos, func(a, b *avax.UTXO) int { return a.InputID().Compare(b.InputID()) })
		txIDs := make([]any, len(utxos))
		for i, u := range utxos {
			txIDs[i] = u.TxID
		}

		return txIDs, nil
	case key == "RewardValidatorTx.TxID":
		return anys(slices.SortedFunc(maps.Keys(r.rewardIndex), ids.ID.Compare)), nil
	case name == "TxID":
		return anys(r.autoRenew.keys(active)), anys(r.autoRenew.keys(ended))
	case name == "ValidationID":
		return anys(r.l1.keys(active)), anys(slices.Concat(r.l1.keys(ended), r.l1.keys(removed)))
	case key == "RemoveSubnetValidatorTx.NodeID":
		var nodeIDs []any
		for _, subnetID := range slices.SortedFunc(maps.Keys(r.subnetValidators), ids.ID.Compare) {
			nodeIDs = append(nodeIDs, anys(r.subnetValidators[subnetID])...)
		}

		return nodeIDs, nil
	case name == "NodeID":
		nodeIDs := make([]any, len(r.primaryValidators))
		for i, v := range r.primaryValidators {
			nodeIDs[i] = v.NodeID
		}

		return nodeIDs, anys(slices.Concat(r.validators.keys(ended), r.validators.keys(removed)))
	case strings.HasSuffix(key, ".Asset.ID"):
		return []any{r.avaxAssetID}, nil
	case name == "BlockchainID":
		return []any{constants.PlatformChainID}, nil
	case name == "SourceChain" || name == "DestinationChain":
		return []any{r.xChainID}, nil
	case name == "Addrs[]" || name == "Addresses[]":
		return anys(r.actors), nil
	}

	return nil, nil
}

// threshold returns the value that the validity of the integer at key turns
// on, when the model knows one: the minimum stake for a weight or a staked
// amount, the chain time for a time, and the minimum stake duration for an end
// time or a period. The nodes' chain time follows the wall clock.
func (r *runner) threshold(key string) (uint64, bool) {
	now := uint64(time.Now().Unix())
	minStake := r.minValidatorStake
	if strings.Contains(key, "Delegator") {
		minStake = r.minDelegatorStake
	}

	switch name := key[strings.LastIndexByte(key, '.')+1:]; {
	case name == "Wght", name == "Amt" && strings.Contains(key, ".StakeOuts[]"):
		return minStake, true
	case name == "End":
		return now + uint64(minStakeDuration/time.Second), true
	case name == "Start", name == "Time", name == "Timestamp":
		return now, true
	case name == "Period":
		return uint64(heliconMinStakeDuration / time.Second), true
	}

	return 0, false
}

// An ownerValue is an owner set in a form that a pool can hold: addrs holds
// the addresses' bytes one after another, because a slice is not comparable.
type ownerValue struct {
	locktime  uint64
	threshold uint32
	addrs     string
}

func newOwnerValue(locktime uint64, threshold uint32, addrs []ids.ShortID) ownerValue {
	var b strings.Builder
	for _, addr := range addrs {
		b.Write(addr[:])
	}

	return ownerValue{locktime: locktime, threshold: threshold, addrs: b.String()}
}

// ownerOf returns the owner set in v, a field of kind ownerField.
func ownerOf(v reflect.Value) ownerValue {
	switch o := v.Interface().(type) {
	case secp256k1fx.OutputOwners:
		return newOwnerValue(o.Locktime, o.Threshold, o.Addrs)
	case message.PChainOwner:
		return newOwnerValue(0, o.Threshold, o.Addresses)
	default:
		panic(fmt.Sprintf("ownerOf: a %T is not an owner", o))
	}
}

// set stores o in v, a settable field of kind ownerField. A
// [message.PChainOwner] has no locktime.
func (o ownerValue) set(v reflect.Value) {
	switch p := v.Addr().Interface().(type) {
	case *secp256k1fx.OutputOwners:
		p.Locktime, p.Threshold, p.Addrs = o.locktime, o.threshold, o.addresses()
	case *message.PChainOwner:
		p.Threshold, p.Addresses = o.threshold, o.addresses()
	}
}

func (o ownerValue) addresses() []ids.ShortID {
	var addrs []ids.ShortID
	for b := o.addrs; len(b) > 0; b = b[ids.ShortIDLen:] {
		addrs = append(addrs, ids.ShortID([]byte(b[:ids.ShortIDLen])))
	}

	return addrs
}

func (o ownerValue) String() string {
	s := fmt.Sprintf("%d of %v", o.threshold, o.addresses())
	if o.locktime != 0 {
		s += fmt.Sprintf(", locked until %d", o.locktime)
	}

	return s
}

// pick returns an element of vs that t draws, each equally likely.
func pick[V any](t *rapid.T, vs []V, label string) V {
	return vs[uniform(len(vs)).Draw(t, label)]
}

func ofType[K any](vs []any) []K {
	var ks []K
	for _, v := range vs {
		if k, ok := v.(K); ok {
			ks = append(ks, k)
		}
	}

	return ks
}

func anys[V any](vs []V) []any {
	a := make([]any, len(vs))
	for i, v := range vs {
		a[i] = v
	}

	return a
}
