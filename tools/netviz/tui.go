// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"

	tea "github.com/charmbracelet/bubbletea"
)

// model is the TUI's state. It has two screens: the tx list, and the steps of
// the followed IDs.
type model struct {
	events []Event
	nodes  []string
	txs    []TxInfo
	blocks []BlockInfo
	types  []string // every known tx type, sorted
	st     styles
	window int64

	typ        string // the list's type filter; empty shows every type
	showBlocks bool   // the list shows blocks instead of txs
	listCursor int

	following bool
	followIDs []string
	show      map[Kind]bool
	steps     []Step
	cursor    int // the last step shown; playback reveals steps one at a time
	playing   bool
	interval  time.Duration

	input     textinput.Model
	inputting bool

	width, height int
}

type tickMsg struct{}

func newModel(events []Event, window int64) model {
	txs := Txs(events)
	seen := make(map[string]bool)
	var types []string
	for _, tx := range txs {
		if tx.Type != "" && !seen[tx.Type] {
			seen[tx.Type] = true
			types = append(types, tx.Type)
		}
	}
	slices.Sort(types)

	blocks := Blocks(events)
	input := textinput.New()
	input.ShowSuggestions = true
	input.SetSuggestions(followSuggestions(txs, blocks))
	input.Placeholder = "tx or block ID"
	input.Prompt = "follow: "

	return model{
		events:   events,
		nodes:    Nodes(events),
		txs:      txs,
		blocks:   blocks,
		types:    types,
		st:       colorful(),
		window:   window,
		show:     defaultKinds(),
		interval: 300 * time.Millisecond,
		input:    input,
		width:    160,
		height:   40,
	}
}

func (model) Init() tea.Cmd {
	return nil
}

func (m model) tick() tea.Cmd {
	return tea.Tick(m.interval, func(time.Time) tea.Msg { return tickMsg{} })
}

// follow switches to the steps of ids.
func (m *model) follow(ids []string) {
	m.following = true
	m.followIDs = ids
	m.cursor = 0
	m.playing = false
	m.refresh()
}

// refresh recomputes the steps after the followed IDs or the kinds change.
func (m *model) refresh() {
	m.steps = Follow(m.events, Expand(m.events, m.followIDs), m.show, m.window)
	m.cursor = min(m.cursor, max(len(m.steps)-1, 0))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		if !m.playing {
			return m, nil
		}
		if m.cursor >= len(m.steps)-1 {
			m.playing = false
			return m, nil
		}
		m.cursor++
		return m, m.tick()

	case tea.KeyMsg:
		if m.inputting {
			return m.updateInput(msg)
		}
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			return m, tea.Quit
		}
		if msg.String() == "/" {
			m.inputting = true
			m.input.SetValue("")
			return m, m.input.Focus()
		}
		if m.following {
			return m.updateFollow(msg)
		}
		return m.updateList(msg)
	}
	return m, nil
}

func (m model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.inputting = false
		m.input.Blur()
		return m, nil
	case "enter":
		m.inputting = false
		m.input.Blur()
		if ids := strings.Fields(m.input.Value()); len(ids) > 0 {
			m.follow(ids)
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	ids := m.listIDs()
	switch msg.String() {
	case "up", "k":
		m.listCursor = max(m.listCursor-1, 0)
	case "down", "j":
		m.listCursor = min(m.listCursor+1, max(len(ids)-1, 0))
	case "t":
		m.typ = nextType(m.types, m.typ)
		m.listCursor = 0
	case "b":
		m.showBlocks = !m.showBlocks
		m.listCursor = 0
	case "enter":
		if m.listCursor < len(ids) {
			m.follow([]string{ids[m.listCursor]})
		}
	}
	return m, nil
}

// listIDs returns the IDs of the rows the list shows, in order.
func (m model) listIDs() []string {
	var ids []string
	if m.showBlocks {
		for _, blk := range filterBlocks(m.blocks, m.typ) {
			ids = append(ids, blk.ID)
		}
		return ids
	}
	for _, tx := range filterTxs(m.txs, m.typ) {
		ids = append(ids, tx.ID)
	}
	return ids
}

// nextType returns the type after typ in types, cycling through "" (every
// type) after the last one.
func nextType(types []string, typ string) string {
	if typ == "" {
		if len(types) == 0 {
			return ""
		}
		return types[0]
	}

	i := slices.Index(types, typ)
	if i < 0 || i == len(types)-1 {
		return ""
	}
	return types[i+1]
}

func (m model) updateFollow(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key := msg.String(); key {
	case "esc":
		m.following = false
		m.playing = false
	case " ":
		m.playing = !m.playing
		if m.playing {
			if m.cursor >= len(m.steps)-1 {
				m.cursor = 0
			}
			return m, m.tick()
		}
	case "right", "l":
		m.cursor = min(m.cursor+1, max(len(m.steps)-1, 0))
	case "left", "h":
		m.cursor = max(m.cursor-1, 0)
	case "g":
		m.cursor = 0
	case "G":
		m.cursor = max(len(m.steps)-1, 0)
	case "+", "=":
		m.interval = max(m.interval/2, 25*time.Millisecond)
	case "-":
		m.interval = min(m.interval*2, 4*time.Second)
	default:
		if len(key) == 1 && key[0] >= '1' && int(key[0]-'1') < len(Kinds) {
			k := Kinds[key[0]-'1']
			m.show[k] = !m.show[k]
			m.refresh()
		}
	}
	return m, nil
}

func (m model) View() string {
	var b strings.Builder
	if m.following {
		b.WriteString(m.followView())
	} else {
		b.WriteString(m.listView())
	}
	if m.inputting {
		b.WriteString("\n" + m.input.View())
	}
	return b.String()
}

func (m model) listView() string {
	typ := m.typ
	if typ == "" {
		typ = "all"
	}
	what, other := "txs", "blocks"
	if m.showBlocks {
		what, other = "blocks", "txs"
	}
	n := len(m.listIDs())

	var b strings.Builder
	b.WriteString(m.st.header.Render(fmt.Sprintf("netviz ▸ %d %s on %d nodes   type: %s", n, what, len(m.nodes), typ)))
	b.WriteString("\n\n")

	// Keep the cursor on screen: show the rows around it.
	rows := max(m.height-6, 3)
	first := max(min(m.listCursor-rows/2, n-rows), 0)
	end := min(first+rows, n)
	if m.showBlocks {
		shown := filterBlocks(m.blocks, m.typ)
		b.WriteString(renderBlocks(shown[first:end], m.nodes, "", m.listCursor-first, m.st))
	} else {
		shown := filterTxs(m.txs, m.typ)
		b.WriteString(renderTxs(shown[first:end], m.nodes, "", m.listCursor-first, m.st))
	}
	b.WriteString("\n")
	b.WriteString(m.st.dim.Render(fmt.Sprintf("[↑/↓] move  [t] type  [b] %s  [enter] follow  [/] follow an ID (tab completes)  [q] quit", other)))
	return b.String()
}

func (m model) followView() string {
	var b strings.Builder

	title := "netviz ▸ follow " + strings.Join(m.followIDs, " ")
	if typ := m.followedType(); typ != "" {
		title += " (" + typ + ")"
	}
	state := "paused"
	if m.playing {
		state = "playing"
	}
	b.WriteString(m.st.header.Render(title))
	fmt.Fprintf(&b, "   %s %s/step   step %d/%d\n", state, m.interval, min(m.cursor+1, len(m.steps)), len(m.steps))
	b.WriteString(m.kindsLine() + "\n\n")

	if len(m.steps) == 0 {
		b.WriteString(m.st.dim.Render("no events for these IDs and kinds") + "\n")
		b.WriteString(m.st.dim.Render("[1-8] kinds  [esc] list  [/] follow an ID  [q] quit"))
		return b.String()
	}

	// Show the steps up to the cursor, as many as fit, ending at the cursor.
	detailRows := len(m.steps[m.cursor].Cells) + 1
	rows := max(m.height-10-detailRows, 3)
	first := max(m.cursor-rows+1, 0)
	b.WriteString(renderSteps(m.steps[first:m.cursor+1], m.nodes, m.cursor-first, m.st))
	b.WriteString(renderTips(m.events, m.nodes, m.steps[m.cursor].Time, m.st) + "\n\n")
	b.WriteString(renderDetails(m.steps[m.cursor], m.nodes, m.width, m.st))
	b.WriteString(m.st.dim.Render("[space] play/pause  [←/→] step  [g/G] first/last  [+/-] speed  [1-8] kinds  [esc] list  [/] follow an ID  [q] quit"))
	return b.String()
}

// followedType describes the followed ID: a tx's type, or a block's height
// and txs. It is empty when the followed ID is not a single known tx or block.
func (m model) followedType() string {
	if len(m.followIDs) != 1 {
		return ""
	}
	id := m.followIDs[0]
	for _, tx := range m.txs {
		if tx.ID == id {
			return tx.Type
		}
	}
	for _, blk := range m.blocks {
		if blk.ID == id || blk.Inner == id {
			desc := "block @" + height(Block{Height: blk.Height})
			if s := txSummary(blk.Txs); s != "" {
				desc += ": " + s
			}
			return desc
		}
	}
	return ""
}

// kindsLine shows each kind's key and whether it is shown.
func (m model) kindsLine() string {
	parts := make([]string, len(Kinds))
	for i, k := range Kinds {
		label := fmt.Sprintf("[%d]%s", i+1, k)
		if m.show[k] {
			parts[i] = m.st.ok.Render(label)
		} else {
			parts[i] = m.st.dim.Render(label)
		}
	}
	return strings.Join(parts, " ")
}
