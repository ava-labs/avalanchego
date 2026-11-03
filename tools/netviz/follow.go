// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"slices"
	"strconv"
)

// Kind is what an event did to a followed ID, most important first.
type Kind int

const (
	_ Kind = iota
	KindAccept
	KindReject
	KindBuild
	KindVerify
	KindGossip
	KindTip
	KindVote
	KindFuzzer
)

// fuzzerNode is the node name of the fuzzer's own events.
const fuzzerNode = "fuzzer"

// Kinds lists every Kind, most important first.
var Kinds = []Kind{KindAccept, KindReject, KindBuild, KindVerify, KindGossip, KindTip, KindVote, KindFuzzer}

// String returns the name that the filter keys show.
func (k Kind) String() string {
	switch k {
	case KindAccept:
		return "accept"
	case KindReject:
		return "reject"
	case KindBuild:
		return "build"
	case KindVerify:
		return "verify"
	case KindGossip:
		return "gossip"
	case KindTip:
		return "tip"
	case KindVote:
		return "vote"
	case KindFuzzer:
		return "fuzzer"
	default:
		return "Kind(" + strconv.Itoa(int(k)) + ")"
	}
}

// A Cell is what one node did in a step.
type Cell struct {
	Kind  Kind
	OK    bool   // false for a refusal or a failed verification
	Text  string // a short summary, such as "✗ tx conflicts with other tx"
	Event *Event
}

// A Step is the events, at most one per node, that the same kind of input
// caused close together in time, such as one gossip message reaching every
// peer.
type Step struct {
	Time  int64 // timestamp of the first event
	Op    string
	Label string // what the input was, such as "push gossip"
	From  string // set when every event in the step came from the same node
	Kind  Kind
	Cells map[string]Cell // by node ID
}

// Expand returns ids plus the IDs linked to them. A tx brings in the blocks
// that accepted it: the P-Chain block and the proposervm block around it. A
// block brings in the other block and its txs. A tx does not bring in the
// other txs in its block, so following a tx does not show their gossip.
func Expand(events []Event, ids []string) map[string]bool {
	asked := make(map[string]bool, len(ids))
	for _, id := range ids {
		asked[id] = true
	}

	followed := make(map[string]bool, len(ids))
	for id := range asked {
		followed[id] = true
	}
	for _, e := range events {
		for _, at := range e.AcceptedTxs {
			var outer []string
			for _, b := range e.Accepted {
				if b.Height == at.Height {
					outer = append(outer, b.Block)
				}
			}

			blockAsked := asked[at.Block]
			for _, id := range outer {
				blockAsked = blockAsked || asked[id]
			}
			txAsked := false
			for _, tx := range at.Txs {
				txAsked = txAsked || asked[tx.ID]
			}
			if !blockAsked && !txAsked {
				continue
			}

			followed[at.Block] = true
			for _, id := range outer {
				followed[id] = true
			}
			if blockAsked {
				for _, tx := range at.Txs {
					followed[tx.ID] = true
				}
			}
		}
	}
	return followed
}

// Follow returns the steps that concern the followed IDs, for the kinds in
// show. Events within window nanoseconds of a step's first event join it if
// they have the same op and kind and their node is not in it yet.
func Follow(events []Event, followed map[string]bool, show map[Kind]bool, window int64) []Step {
	var steps []Step
	for i := range events {
		e := &events[i]
		c, ok := classify(e, followed, show)
		if !ok {
			continue
		}

		if s := joinable(steps, e, c.Kind, window); s != nil {
			s.Cells[e.Node] = c
			if s.From != e.From {
				s.From = ""
			}
			continue
		}
		steps = append(steps, Step{
			Time:  e.Timestamp,
			Op:    e.Op,
			Label: label(e),
			From:  e.From,
			Kind:  c.Kind,
			Cells: map[string]Cell{e.Node: c},
		})
	}
	return steps
}

// label names an event's input for its step's row. The app ops alone do not
// say what they carried, so it uses the gossip kind or the app protocol when
// the node recorded one.
func label(e *Event) string {
	if e.Node == fuzzerNode {
		s := "#" + strconv.Itoa(e.Step) + " " + e.Op
		if e.Path != "" && e.To != nil {
			s += " " + e.Path + " to " + short(e.To.ID)
		}
		return s
	}

	switch e.Gossip {
	case "push":
		return "push gossip"
	case "pull_request":
		return "pull request"
	case "pull_response":
		return "pull response"
	case "pull_failed":
		return "pull failed"
	}
	if e.P2PHandler != "" {
		return e.Op + " " + e.P2PHandler
	}
	return e.Op
}

// joinable returns the latest step within window of e that e can join, or
// nil. Steps are in time order, so the search stops at the first step that
// started too early.
func joinable(steps []Step, e *Event, kind Kind, window int64) *Step {
	for i := len(steps) - 1; i >= 0 && e.Timestamp-steps[i].Time <= window; i-- {
		s := &steps[i]
		if _, ok := s.Cells[e.Node]; s.Op == e.Op && s.Kind == kind && !ok {
			return s
		}
	}
	return nil
}

// classify returns the most important thing e did to a followed ID, among
// the kinds in show.
func classify(e *Event, followed map[string]bool, show map[Kind]bool) (Cell, bool) {
	for _, kind := range Kinds {
		if !show[kind] {
			continue
		}
		if c, ok := cellFor(e, followed, kind); ok {
			c.Kind = kind
			c.Event = e
			return c, true
		}
	}
	return Cell{}, false
}

func cellFor(e *Event, followed map[string]bool, kind Kind) (Cell, bool) {
	switch kind {
	case KindAccept:
		if b, ok := findBlock(e.Accepted, followed); ok {
			return Cell{OK: true, Text: "★ accept @" + height(b)}, true
		}
	case KindReject:
		if b, ok := findBlock(e.Rejected, followed); ok {
			return Cell{Text: "✗ reject @" + height(b)}, true
		}
	case KindBuild:
		if b, ok := findBlock(e.Built, followed); ok {
			return Cell{OK: true, Text: "■ built @" + height(b)}, true
		}
	case KindVerify:
		if b, ok := findBlock(e.Verified, followed); ok {
			if b.Err != "" {
				return Cell{Text: "✗ verify: " + b.Err}, true
			}
			return Cell{OK: true, Text: "✓ verify"}, true
		}
	case KindGossip:
		for _, it := range e.Items {
			if !followed[it.ID] {
				continue
			}
			if it.Err != "" {
				return Cell{Text: "✗ " + it.Err}, true
			}
			return Cell{OK: true, Text: "✓ added"}, true
		}
		for _, id := range e.Served {
			if followed[id] {
				return Cell{OK: true, Text: "↑ served"}, true
			}
		}
	case KindTip:
		for _, b := range e.Preferred {
			if followed[b.Block] {
				return Cell{OK: true, Text: "→ prefer @" + height(b)}, true
			}
		}
	case KindVote:
		if e.Vote != nil && followed[e.Vote.PreferredAtHeight] {
			return Cell{OK: true, Text: "◆ vote"}, true
		}
	case KindFuzzer:
		if e.Node != fuzzerNode {
			break
		}
		if followed[e.Tx] {
			return outcomeCell(e.Outcome), true
		}
		for _, at := range e.Scanned {
			if followed[at.Block] || slices.ContainsFunc(at.Txs, func(tx Tx) bool { return followed[tx.ID] }) {
				return Cell{OK: true, Text: "◎ scanned @" + height(Block{Height: at.Height})}, true
			}
		}
	}
	return Cell{}, false
}

// outcomeCell shows how a fuzzer op's tx ended. A tx that no node decided
// while the op waited is not a refusal.
func outcomeCell(outcome string) Cell {
	switch outcome {
	case "committed", "sent":
		return Cell{OK: true, Text: "▶ " + outcome}
	case "undecided":
		return Cell{OK: true, Text: "… " + outcome}
	default:
		return Cell{Text: "✗ " + outcome}
	}
}

func findBlock(blocks []Block, followed map[string]bool) (Block, bool) {
	for _, b := range blocks {
		if followed[b.Block] {
			return b, true
		}
	}
	return Block{}, false
}

func height(b Block) string {
	return strconv.FormatUint(b.Height, 10)
}

// A TxInfo is what the logs show about one tx across nodes.
type TxInfo struct {
	ID        string
	Type      string // empty until some node accepts the tx
	FirstSeen int64
	Accepted  map[string]bool   // by node ID
	Refused   map[string]string // by node ID, the refusal's error
}

// Txs returns every tx that appears in events, by when it first appeared.
// events must be in time order, as Load returns them.
func Txs(events []Event) []TxInfo {
	var txs []TxInfo
	index := make(map[string]int)
	get := func(id string, ts int64) *TxInfo {
		i, ok := index[id]
		if !ok {
			i = len(txs)
			index[id] = i
			txs = append(txs, TxInfo{
				ID:        id,
				FirstSeen: ts,
				Accepted:  make(map[string]bool),
				Refused:   make(map[string]string),
			})
		}
		return &txs[i]
	}

	for _, e := range events {
		// The fuzzer names a tx when it issues it, before any node accepts
		// it or tells its type.
		if e.Node == fuzzerNode && e.Tx != "" {
			if tx := get(e.Tx, e.Timestamp); tx.Type == "" {
				tx.Type = e.TxType
			}
		}
		for _, it := range e.Items {
			if it.ID == "" {
				continue
			}

			tx := get(it.ID, e.Timestamp)
			if it.Err != "" {
				tx.Refused[e.Node] = it.Err
			}
		}
		for _, at := range e.AcceptedTxs {
			for _, t := range at.Txs {
				tx := get(t.ID, e.Timestamp)
				tx.Type = t.Type
				tx.Accepted[e.Node] = true
			}
		}
	}
	return txs
}

// A BlockInfo is what the logs show about one block across nodes.
type BlockInfo struct {
	ID        string // the proposervm block, as consensus names it
	Inner     string // the P-Chain block inside it; empty until a node accepts it
	Height    uint64
	Builder   string // empty if no event shows the build
	FirstSeen int64
	Txs       []Tx              // empty until a node accepts the block
	Verified  map[string]string // by node ID: empty, or the verification error
	Accepted  map[string]bool   // by node ID
	Rejected  map[string]bool   // by node ID
}

// Blocks returns every block that appears in events, by when it first
// appeared. events must be in time order, as Load returns them.
func Blocks(events []Event) []BlockInfo {
	var blocks []BlockInfo
	index := make(map[string]int)
	get := func(id string, height uint64, ts int64) *BlockInfo {
		i, ok := index[id]
		if !ok {
			i = len(blocks)
			index[id] = i
			blocks = append(blocks, BlockInfo{
				ID:        id,
				Height:    height,
				FirstSeen: ts,
				Verified:  make(map[string]string),
				Accepted:  make(map[string]bool),
				Rejected:  make(map[string]bool),
			})
		}
		return &blocks[i]
	}

	for _, e := range events {
		for _, b := range e.Built {
			get(b.Block, b.Height, e.Timestamp).Builder = e.Node
		}
		for _, b := range e.Verified {
			get(b.Block, b.Height, e.Timestamp).Verified[e.Node] = b.Err
		}
		for _, b := range e.Rejected {
			get(b.Block, b.Height, e.Timestamp).Rejected[e.Node] = true
		}
		for _, b := range e.Accepted {
			blk := get(b.Block, b.Height, e.Timestamp)
			blk.Accepted[e.Node] = true
			// The P-Chain block inside is the accepted_txs entry at the same
			// height.
			for _, at := range e.AcceptedTxs {
				if at.Height == b.Height {
					blk.Inner = at.Block
					blk.Txs = at.Txs
				}
			}
		}
	}
	return blocks
}
