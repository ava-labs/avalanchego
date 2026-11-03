// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// conflictRun is a small copy of a real run: node b gossips tx A and node d
// gossips tx B, which conflicts with A. Node c builds outer block X, which
// holds P-Chain block x with A, and every node accepts it.
var conflictRun = map[string]string{
	"a/logs/events.log": `{"timestamp":100,"node":"a","chain":"P","op":"app_gossip","from":"b","gossip":"push","p2p_handler":"tx_gossip","items":[{"id":"A"}]}
{"timestamp":110,"node":"a","chain":"P","op":"app_gossip","from":"d","items":[{"id":"B","err":"tx conflicts with other tx"}]}
{"timestamp":300,"node":"a","chain":"P","op":"push_query","from":"c","request_id":7,"verified":[{"block":"X","height":3}],"preferred":[{"block":"X","height":3}]}
{"timestamp":320,"node":"a","chain":"P","op":"chits","from":"c","request_id":9,"vote":{"preferred_at_height":"X","accepted":"W","accepted_height":2}}
{"timestamp":400,"node":"a","chain":"P","op":"chits","from":"d","request_id":10,"accepted_txs":[{"block":"x","height":3,"txs":[{"id":"A","type":"BaseTx"}]}],"accepted":[{"block":"X","height":3}],"last_accepted":[{"block":"X","height":3}]}
`,
	"b/logs/events.log": `{"timestamp":105,"node":"b","chain":"P","op":"app_request","from":"a","request_id":3,"gossip":"pull_request","p2p_handler":"tx_gossip","served":["A"]}
`,
	"c/logs/events.log": `{"timestamp":101,"node":"c","chain":"P","op":"app_gossip","from":"b","gossip":"push","p2p_handler":"tx_gossip","items":[{"id":"A"}]}
{"timestamp":290,"node":"c","chain":"P","op":"notify","msg":"Pending Transactions","built":[{"block":"X","height":3}],"verified":[{"block":"X","height":3}]}
{"timestamp":401,"node":"c","chain":"P","op":"chits","from":"a","request_id":7,"accepted_txs":[{"block":"x","height":3,"txs":[{"id":"A","type":"BaseTx"}]}],"accepted":[{"block":"X","height":3}]}
`,
	"d/logs/events.log": `{"timestamp":102,"node":"d","chain":"P","op":"app_gossip","from":"b","items":[{"id":"A","err":"tx conflicts with other tx"}]}
{"timestamp":301,"node":"d","chain":"P","op":"push_query","from":"c","request_id":7,"verified":[{"block":"X","height":3}]}
{"timestamp":402,"node":"d","chain":"P","op":"chits","from":"a","request_id":4,"accepted_txs":[{"block":"x","height":3,"txs":[{"id":"A","type":"BaseTx"}]}],"accepted":[{"block":"X","height":3}]}
`,
}

func loadConflictRun(t *testing.T) []Event {
	t.Helper()

	return loadRun(t, conflictRun)
}

// loadRun writes each file of run, by its path under a temp dir, and loads
// the dir.
func loadRun(t *testing.T, run map[string]string) []Event {
	t.Helper()

	dir := t.TempDir()
	for name, content := range run {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}

	events, err := Load([]string{dir})
	require.NoError(t, err)
	return events
}

func TestLoad(t *testing.T) {
	events := loadConflictRun(t)
	require.Len(t, events, 12)
	for i := 1; i < len(events); i++ {
		require.LessOrEqual(t, events[i-1].Timestamp, events[i].Timestamp)
	}
	require.Equal(t, "b", events[0].From)
	require.Contains(t, events[0].Raw, `"items":[{"id":"A"}]`)
}

func TestExpand(t *testing.T) {
	events := loadConflictRun(t)

	require.Equal(t, map[string]bool{"A": true, "x": true, "X": true}, Expand(events, []string{"A"}), "a tx brings in the blocks that accepted it")
	require.Equal(t, map[string]bool{"X": true, "x": true, "A": true}, Expand(events, []string{"X"}), "a block brings in its txs")
	require.Equal(t, map[string]bool{"B": true}, Expand(events, []string{"B"}), "a tx that never commits brings in nothing")
}

func TestFollow(t *testing.T) {
	events := loadConflictRun(t)
	all := map[Kind]bool{KindAccept: true, KindReject: true, KindBuild: true, KindVerify: true, KindGossip: true, KindTip: true}

	steps := Follow(events, Expand(events, []string{"A"}), all, 50)

	type cell struct {
		Kind Kind
		OK   bool
	}
	type step struct {
		Op    string
		From  string
		Cells map[string]cell
	}
	got := make([]step, len(steps))
	for i, s := range steps {
		got[i] = step{Op: s.Op, From: s.From, Cells: make(map[string]cell)}
		for node, c := range s.Cells {
			got[i].Cells[node] = cell{Kind: c.Kind, OK: c.OK}
		}
	}
	require.Equal(t, []step{
		{Op: "app_gossip", From: "b", Cells: map[string]cell{"a": {KindGossip, true}, "c": {KindGossip, true}, "d": {KindGossip, false}}},
		{Op: "app_request", From: "a", Cells: map[string]cell{"b": {KindGossip, true}}},
		{Op: "notify", Cells: map[string]cell{"c": {KindBuild, true}}},
		{Op: "push_query", From: "c", Cells: map[string]cell{"a": {KindVerify, true}, "d": {KindVerify, true}}},
		{Op: "chits", Cells: map[string]cell{"a": {KindAccept, true}, "c": {KindAccept, true}, "d": {KindAccept, true}}},
	}, got, "B's gossip at a and a's vote are not about A's blocks or A")
}

func TestFollowVotes(t *testing.T) {
	events := loadConflictRun(t)

	steps := Follow(events, Expand(events, []string{"X"}), map[Kind]bool{KindVote: true}, 50)
	require.Len(t, steps, 1)
	require.Equal(t, KindVote, steps[0].Cells["a"].Kind)
}

func TestTxs(t *testing.T) {
	events := loadConflictRun(t)

	txs := Txs(events)
	require.Len(t, txs, 2)

	require.Equal(t, "A", txs[0].ID)
	require.Equal(t, "BaseTx", txs[0].Type)
	require.Equal(t, int64(100), txs[0].FirstSeen)
	require.Equal(t, map[string]bool{"a": true, "c": true, "d": true}, txs[0].Accepted)
	require.Equal(t, map[string]string{"d": "tx conflicts with other tx"}, txs[0].Refused)

	require.Equal(t, "B", txs[1].ID)
	require.Empty(t, txs[1].Type, "never accepted, so never typed")
	require.Empty(t, txs[1].Accepted)
	require.Equal(t, map[string]string{"a": "tx conflicts with other tx"}, txs[1].Refused)
}

func TestBlocks(t *testing.T) {
	events := loadConflictRun(t)

	blocks := Blocks(events)
	require.Len(t, blocks, 1)
	require.Equal(t, BlockInfo{
		ID:        "X",
		Inner:     "x",
		Height:    3,
		Builder:   "c",
		FirstSeen: 290,
		Txs:       []Tx{{ID: "A", Type: "BaseTx"}},
		Verified:  map[string]string{"a": "", "c": "", "d": ""},
		Accepted:  map[string]bool{"a": true, "c": true, "d": true},
		Rejected:  map[string]bool{},
	}, blocks[0])
}

// fuzzerLog is the fuzzer's events.log for conflictRun: it issued A to b
// through b's API and B to d by gossip, and its check after A found A in
// block x.
const fuzzerLog = `{"timestamp":90,"node":"fuzzer","op":"issue","step":1,"tx_type":"BaseTx","tx":"A","to":{"ref":"node 1","id":"b"},"path":"api","outcome":"committed"}
{"timestamp":95,"node":"fuzzer","op":"issue","step":2,"tx_type":"BaseTx","tx":"B","to":{"ref":"node 3","id":"d"},"path":"gossip","outcome":"dropped","decided_by":"gossip: tx conflicts with other tx"}
{"timestamp":500,"node":"fuzzer","op":"check","step":2,"scanned":[{"block":"x","height":3,"txs":[{"id":"A","type":"BaseTx"}]}],"height":3}
`

// loadFuzzedRun loads conflictRun with the fuzzer's events.log beside the
// nodes' dirs, where the fuzzer writes it.
func loadFuzzedRun(t *testing.T) []Event {
	t.Helper()

	run := maps.Clone(conflictRun)
	run["events.log"] = fuzzerLog
	return loadRun(t, run)
}

func TestFollowFuzzer(t *testing.T) {
	events := loadFuzzedRun(t)

	steps := Follow(events, Expand(events, []string{"A"}), defaultKinds(), 50)
	require.NotEmpty(t, steps)

	first := steps[0]
	require.Equal(t, "#1 issue api to b", first.Label, "the op comes first, with its path and target")
	require.Equal(t, Cell{Kind: KindFuzzer, OK: true, Text: "▶ committed", Event: first.Cells[fuzzerNode].Event}, first.Cells[fuzzerNode])

	last := steps[len(steps)-1]
	require.Equal(t, "#2 check", last.Label)
	require.Equal(t, "◎ scanned @3", last.Cells[fuzzerNode].Text)

	steps = Follow(events, Expand(events, []string{"B"}), defaultKinds(), 50)
	require.Equal(t, "✗ dropped", steps[0].Cells[fuzzerNode].Text)
	require.False(t, steps[0].Cells[fuzzerNode].OK)
}

func TestTxsFromFuzzer(t *testing.T) {
	txs := Txs(loadFuzzedRun(t))
	require.Len(t, txs, 2)

	require.Equal(t, int64(90), txs[0].FirstSeen, "the op that issued A is its first sighting")
	require.Equal(t, map[string]bool{"a": true, "c": true, "d": true}, txs[0].Accepted, "the fuzzer's check is not a node that accepts")
	require.Equal(t, "BaseTx", txs[1].Type, "the fuzzer names the type of a tx that no node accepted")
}
