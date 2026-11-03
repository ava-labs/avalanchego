// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderSteps(t *testing.T) {
	events := loadConflictRun(t)
	nodes := Nodes(events)
	steps := Follow(events, Expand(events, []string{"A"}), defaultKinds(), 50)

	out := renderSteps(steps, nodes, -1, plain)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.Len(t, lines, 1+len(steps), "a header and one line per step:\n%s", out)

	require.Contains(t, lines[0], "a")
	require.Contains(t, lines[1], "push gossip from b")
	require.Contains(t, lines[1], "✓ added")
	require.Contains(t, lines[1], "✗ tx conflicts")
	require.Contains(t, lines[2], "pull request from a")
	require.Contains(t, lines[2], "↑ served")
	require.Contains(t, lines[3], "■ built @3")
	require.Contains(t, lines[5], "★ accept @3")
}

func TestRenderTxs(t *testing.T) {
	events := loadConflictRun(t)

	out := renderTxs(Txs(events), Nodes(events), "", -1, plain)
	require.Contains(t, out, "BaseTx")
	require.Contains(t, out, "3/4")
	require.Contains(t, out, "1 refused")

	out = renderTxs(Txs(events), Nodes(events), "BaseTx", -1, plain)
	require.NotContains(t, out, " B ", "B has no type, so the BaseTx filter hides it")
}

func TestRenderBlocks(t *testing.T) {
	events := loadConflictRun(t)

	out := renderBlocks(Blocks(events), Nodes(events), "", -1, plain)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.Len(t, lines, 2, "a header and one block:\n%s", out)
	require.Contains(t, lines[1], "X")
	require.Contains(t, lines[1], "3")
	require.Contains(t, lines[1], "1 BaseTx")
	require.Contains(t, lines[1], "3/4")

	out = renderBlocks(Blocks(events), Nodes(events), "CreateSubnetTx", -1, plain)
	require.Len(t, strings.Split(strings.TrimRight(out, "\n"), "\n"), 1, "no block holds a CreateSubnetTx")
}

func TestFollowSuggestions(t *testing.T) {
	events := loadConflictRun(t)

	require.Equal(t, []string{"A", "B", "X", "x"}, followSuggestions(Txs(events), Blocks(events)))
}

func TestRenderFuzzedRun(t *testing.T) {
	events := loadFuzzedRun(t)

	out := renderTxs(Txs(events), Nodes(events), "", -1, plain)
	require.Contains(t, out, "3/4", "the fuzzer is not counted as a node")
}
