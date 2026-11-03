// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Column widths, in cells.
const (
	timeWidth = 9
	stepWidth = 30
	cellWidth = 16
	idWidth   = 14
	typeWidth = 28
)

// styles colors the output. plain leaves it uncolored, for -print and tests.
type styles struct {
	header, ok, bad, dim, cursor lipgloss.Style
}

var plain = styles{
	header: lipgloss.NewStyle(),
	ok:     lipgloss.NewStyle(),
	bad:    lipgloss.NewStyle(),
	dim:    lipgloss.NewStyle(),
	cursor: lipgloss.NewStyle(),
}

// colorful returns the styles for a terminal.
func colorful() styles {
	return styles{
		header: lipgloss.NewStyle().Bold(true),
		ok:     lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		bad:    lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
		dim:    lipgloss.NewStyle().Faint(true),
		cursor: lipgloss.NewStyle().Reverse(true),
	}
}

// Nodes returns every node in events, sorted.
func Nodes(events []Event) []string {
	seen := make(map[string]bool)
	var nodes []string
	for _, e := range events {
		if e.Node != "" && !seen[e.Node] {
			seen[e.Node] = true
			nodes = append(nodes, e.Node)
		}
	}
	slices.Sort(nodes)
	return nodes
}

// countNodes counts the avalanchego nodes among nodes, leaving out the
// fuzzer, which accepts nothing.
func countNodes(nodes []string) int {
	n := len(nodes)
	if slices.Contains(nodes, fuzzerNode) {
		n--
	}
	return n
}

// short returns a node or block ID cut to its first characters, which are
// enough to tell IDs in one run apart.
func short(id string) string {
	id = strings.TrimPrefix(id, "NodeID-")
	if len(id) > 6 {
		return id[:6]
	}
	return id
}

// defaultKinds is every kind except votes, which add a row per poll.
func defaultKinds() map[Kind]bool {
	show := make(map[Kind]bool, len(Kinds))
	for _, k := range Kinds {
		show[k] = k != KindVote
	}
	return show
}

// pad cuts or pads s to exactly width cells.
func pad(s string, width int) string {
	s = ansi.Truncate(s, width-1, "…")
	return s + strings.Repeat(" ", width-ansi.StringWidth(s))
}

// renderSteps renders steps as a table with a column per node. cursor marks a
// step; -1 marks none.
func renderSteps(steps []Step, nodes []string, cursor int, st styles) string {
	var b strings.Builder
	b.WriteString(st.header.Render("  " + pad("time", timeWidth) + pad("step", stepWidth) + nodeHeaders(nodes)))
	b.WriteByte('\n')

	var start int64
	if len(steps) > 0 {
		start = steps[0].Time
	}
	for i, s := range steps {
		label := s.Label
		if s.From != "" {
			label += " from " + short(s.From)
		}
		var line strings.Builder
		line.WriteString(marker(i == cursor) + pad(fmt.Sprintf("+%dms", (s.Time-start)/1e6), timeWidth) + pad(label, stepWidth))
		for _, node := range nodes {
			c, ok := s.Cells[node]
			switch {
			case !ok:
				line.WriteString(st.dim.Render(pad("·", cellWidth)))
			case c.OK:
				line.WriteString(st.ok.Render(pad(c.Text, cellWidth)))
			default:
				line.WriteString(st.bad.Render(pad(c.Text, cellWidth)))
			}
		}
		if i == cursor {
			b.WriteString(st.cursor.Render(line.String()))
		} else {
			b.WriteString(line.String())
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func nodeHeaders(nodes []string) string {
	var b strings.Builder
	for _, node := range nodes {
		b.WriteString(pad(short(node), cellWidth))
	}
	return b.String()
}

func marker(selected bool) string {
	if selected {
		return "▸ "
	}
	return "  "
}

// renderTips renders each node's preferred block as of time t: the latest
// preference that any of its events recorded at or before t.
func renderTips(events []Event, nodes []string, t int64, st styles) string {
	tips := make(map[string]Block)
	for _, e := range events {
		if e.Timestamp > t {
			break
		}
		if len(e.Preferred) > 0 {
			tips[e.Node] = e.Preferred[len(e.Preferred)-1]
		}
	}

	var line strings.Builder
	line.WriteString("  " + pad("tips", timeWidth+stepWidth))
	for _, node := range nodes {
		tip, ok := tips[node]
		if !ok {
			line.WriteString(st.dim.Render(pad("·", cellWidth)))
			continue
		}
		line.WriteString(pad(short(tip.Block)+"@"+height(tip), cellWidth))
	}
	return line.String()
}

// renderDetails renders each event in s, one line each, cut to width.
func renderDetails(s Step, nodes []string, width int, st styles) string {
	var b strings.Builder
	for _, node := range nodes {
		c, ok := s.Cells[node]
		if !ok {
			continue
		}
		b.WriteString(st.dim.Render(pad(short(node), 8)))
		b.WriteString(ansi.Truncate(c.Event.Raw, width-8, "…"))
		b.WriteByte('\n')
	}
	return b.String()
}

// renderTxs renders txs as a table. typ keeps only txs of that type; empty
// keeps all. cursor marks a row; -1 marks none.
func renderTxs(txs []TxInfo, nodes []string, typ string, cursor int, st styles) string {
	var b strings.Builder
	b.WriteString(st.header.Render("  " + pad("time", timeWidth) + pad("tx", idWidth) + pad("type", typeWidth) + pad("accepted", 10) + "refused"))
	b.WriteByte('\n')

	var start int64
	if len(txs) > 0 {
		start = txs[0].FirstSeen
	}
	for i, tx := range filterTxs(txs, typ) {
		typeName := tx.Type
		if typeName == "" {
			typeName = "?"
		}
		refused := ""
		if n := len(tx.Refused); n > 0 {
			refused = fmt.Sprintf("%d refused: %s", n, firstRefusal(tx))
		}
		line := marker(i == cursor) +
			pad(fmt.Sprintf("+%dms", (tx.FirstSeen-start)/1e6), timeWidth) +
			pad(tx.ID, idWidth) +
			pad(typeName, typeWidth) +
			pad(fmt.Sprintf("%d/%d", len(tx.Accepted), countNodes(nodes)), 10) +
			refused
		switch {
		case i == cursor:
			line = st.cursor.Render(line)
		case len(tx.Accepted) == 0:
			line = st.bad.Render(line)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// filterTxs returns the txs of type typ, or all txs if typ is empty.
func filterTxs(txs []TxInfo, typ string) []TxInfo {
	if typ == "" {
		return txs
	}

	var kept []TxInfo
	for _, tx := range txs {
		if tx.Type == typ {
			kept = append(kept, tx)
		}
	}
	return kept
}

// firstRefusal returns one refusal's error, from the node that sorts first, so
// the output is the same on every run.
func firstRefusal(tx TxInfo) string {
	nodes := make([]string, 0, len(tx.Refused))
	for node := range tx.Refused {
		nodes = append(nodes, node)
	}
	slices.Sort(nodes)
	return tx.Refused[nodes[0]]
}

// renderBlocks renders blocks as a table. typ keeps only blocks that hold a tx
// of that type; empty keeps all. cursor marks a row; -1 marks none.
func renderBlocks(blocks []BlockInfo, nodes []string, typ string, cursor int, st styles) string {
	var b strings.Builder
	b.WriteString(st.header.Render("  " + pad("time", timeWidth) + pad("block", idWidth) + pad("height", 8) + pad("built by", 10) + pad("txs", typeWidth) + pad("accepted", 10) + "problems"))
	b.WriteByte('\n')

	var start int64
	if len(blocks) > 0 {
		start = blocks[0].FirstSeen
	}
	for i, blk := range filterBlocks(blocks, typ) {
		builder := short(blk.Builder)
		if builder == "" {
			builder = "?"
		}
		line := marker(i == cursor) +
			pad(fmt.Sprintf("+%dms", (blk.FirstSeen-start)/1e6), timeWidth) +
			pad(blk.ID, idWidth) +
			pad(height(Block{Height: blk.Height}), 8) +
			pad(builder, 10) +
			pad(txSummary(blk.Txs), typeWidth) +
			pad(fmt.Sprintf("%d/%d", len(blk.Accepted), countNodes(nodes)), 10) +
			blockProblems(blk)
		switch {
		case i == cursor:
			line = st.cursor.Render(line)
		case len(blk.Rejected) > 0 || len(blk.Accepted) == 0:
			line = st.bad.Render(line)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// filterBlocks returns the blocks that hold a tx of type typ, or all blocks if
// typ is empty.
func filterBlocks(blocks []BlockInfo, typ string) []BlockInfo {
	if typ == "" {
		return blocks
	}

	var kept []BlockInfo
	for _, blk := range blocks {
		if slices.ContainsFunc(blk.Txs, func(tx Tx) bool { return tx.Type == typ }) {
			kept = append(kept, blk)
		}
	}
	return kept
}

// txSummary counts txs by type, such as "2 BaseTx, 1 CreateSubnetTx". It is
// empty for a block that no node accepted, since its txs are unknown.
func txSummary(txs []Tx) string {
	counts := make(map[string]int)
	for _, tx := range txs {
		counts[tx.Type]++
	}
	types := make([]string, 0, len(counts))
	for typ := range counts {
		types = append(types, typ)
	}
	slices.Sort(types)

	parts := make([]string, len(types))
	for i, typ := range types {
		parts[i] = fmt.Sprintf("%d %s", counts[typ], typ)
	}
	return strings.Join(parts, ", ")
}

// blockProblems describes failed verifications and rejections, or is empty.
func blockProblems(blk BlockInfo) string {
	var parts []string
	failed := 0
	var firstErr string
	for _, err := range blk.Verified {
		if err != "" {
			failed++
			if firstErr == "" || err < firstErr {
				firstErr = err // the smallest, so the output is the same every run
			}
		}
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed verify: %s", failed, firstErr))
	}
	if n := len(blk.Rejected); n > 0 {
		parts = append(parts, fmt.Sprintf("rejected on %d", n))
	}
	return strings.Join(parts, "; ")
}

// followSuggestions returns every tx and block ID, sorted, for the follow
// prompt to complete.
func followSuggestions(txs []TxInfo, blocks []BlockInfo) []string {
	seen := make(map[string]bool)
	var ids []string
	add := func(id string) {
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, tx := range txs {
		add(tx.ID)
	}
	for _, blk := range blocks {
		add(blk.ID)
		add(blk.Inner)
	}
	slices.Sort(ids)
	return ids
}
