// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"bytes"
	"fmt"
	"maps"
	"slices"
	"strings"

	"pgregory.net/rapid"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
)

// The fuzzer writes events.log in the nodes' format, with node set to
// "fuzzer" (Stateful Fuzzing.md 5.1): one line per op, one per check, one
// per failed check. A nil runner.events writes nothing.

// A draw is a value that an op drew. Ref names it as the model does, such as
// "actor 0" or "subnet #2" (the third active subnet), which stays the same
// when the program reruns. ID does not.
type draw struct {
	Label string `json:"label,omitempty"`
	Ref   string `json:"ref"`
	ID    string `json:"id"`
}

// A loggedTx is a tx in an accepted block, as nodes log it in accepted_txs.
type loggedTx struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// A scannedBlock is an accepted P-Chain block that a check read.
type scannedBlock struct {
	Block  string     `json:"block"`
	Height uint64     `json:"height"`
	Txs    []loggedTx `json:"txs"`
}

// A transition is a registry entry that a check moved to another lifecycle
// state because the nodes publish it there.
type transition struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Subnet string `json:"subnet,omitempty"`
	To     string `json:"to"`
}

// A found is a UTXO that the model did not hold and that a check explained.
type found struct {
	Kind   string `json:"kind"`
	UTXO   string `json:"utxo"`
	Amount uint64 `json:"amount"`
}

// endEvent writes the event of the op or check that is running. Defer it: a
// failed or skipped rapid check stops by panicking, and then the line gets the
// panic's value, which says why, as its err.
func (r *runner) endEvent() {
	p := recover()
	var err error
	if p != nil {
		err = fmt.Errorf("stopped: %v", p)
	}
	r.ev.End(err)
	r.ev = nil
	if p != nil {
		panic(p)
	}
}

// fail writes a check_failed event that names check and says what differs,
// and fails t.
func (r *runner) fail(t *rapid.T, check string, err error) {
	_, ev := r.events.Start(t.Context(), "check_failed")
	ev.Set("step", r.step)
	ev.Set("check", check)
	ev.End(err)
	r.failed = check
	t.Fatalf("%s: %v", check, err)
}

// valueParts is the actors' value, by where it is.
type valueParts struct {
	UTXOs        uint64 `json:"utxos"`
	L1Balances   uint64 `json:"l1_balances"`
	StakeReturns uint64 `json:"stake_returns"`
	XChain       uint64 `json:"x_chain"`
	InFlight     uint64 `json:"in_flight"`
}

// growth names each part that differs between before and after, with the
// signed change, such as "utxos -10, l1_balances +25".
func growth(before, after valueParts) string {
	parts := []struct {
		name          string
		before, after uint64
	}{
		{"utxos", before.UTXOs, after.UTXOs},
		{"l1_balances", before.L1Balances, after.L1Balances},
		{"stake_returns", before.StakeReturns, after.StakeReturns},
		{"x_chain", before.XChain, after.XChain},
		{"in_flight", before.InFlight, after.InFlight},
	}

	var changes []string
	for _, p := range parts {
		switch {
		case p.after > p.before:
			changes = append(changes, fmt.Sprintf("%s +%d", p.name, p.after-p.before))
		case p.after < p.before:
			changes = append(changes, fmt.Sprintf("%s -%d", p.name, p.before-p.after))
		}
	}

	return strings.Join(changes, ", ")
}

// diffViews describes each UTXO and L1 balance on which a and b differ, a's
// first, or returns "" if they agree.
func diffViews(a, b view) string {
	var diffs []string
	utxoIDs := slices.SortedFunc(maps.Keys(a.utxos), ids.ID.Compare)
	for id := range b.utxos {
		if _, ok := a.utxos[id]; !ok {
			utxoIDs = append(utxoIDs, id)
		}
	}
	for _, id := range utxoIDs {
		if x, y := a.utxos[id], b.utxos[id]; !bytes.Equal(x, y) {
			diffs = append(diffs, fmt.Sprintf("UTXO %s: %s and %s", id, describeUTXO(x), describeUTXO(y)))
		}
	}

	for _, id := range slices.SortedFunc(maps.Keys(a.l1), ids.ID.Compare) {
		if a.l1[id] != b.l1[id] {
			diffs = append(diffs, fmt.Sprintf("L1 validator %s: a balance of %d and %d", id, a.l1[id], b.l1[id]))
		}
	}
	if a.time != b.time || a.price != b.price || a.excess != b.excess {
		diffs = append(diffs, fmt.Sprintf("validator fee state: time %d, price %d, excess %d and time %d, price %d, excess %d", a.time, a.price, a.excess, b.time, b.price, b.excess))
	}

	return strings.Join(diffs, "; ")
}

// describeUTXO decodes UTXO bytes, so a failure shows the amount and owners
// instead of hex.
func describeUTXO(b []byte) string {
	if len(b) == 0 {
		return "missing"
	}

	var u avax.UTXO
	if _, err := platform.Codec.Unmarshal(b, &u); err != nil {
		return fmt.Sprintf("%x, which does not parse: %v", b, err)
	}

	return fmt.Sprintf("%+v", u.Out)
}

// logTxs returns each tx of txs as nodes log it.
func logTxs(txs []*platform.Tx) []loggedTx {
	logged := make([]loggedTx, len(txs))
	for i, tx := range txs {
		logged[i] = loggedTx{ID: tx.ID().String(), Type: txName(tx.Unsigned)}
	}

	return logged
}
