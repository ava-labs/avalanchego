// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const usage = `netviz shows how txs and blocks move between avalanchego nodes, from the
events.log files that nodes write with --log-events. A fuzz run's root dir
also holds the fuzzer's events.log, so each op shows beside its effects on
every node, in the fuzzer column.

Usage:

	netviz [flags] [path ...]

Each path is an events.log file or a directory to search for them. The
default is the current directory.

Without -print, netviz opens a TUI that lists every tx. Pick one to follow
its gossip, the blocks that hold it, and their verification and acceptance on
each node.

Flags:
`

func main() {
	flag.Usage = func() {
		fmt.Fprint(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}
	var (
		follow    = flag.String("follow", "", "comma-separated tx or block IDs to follow")
		printOnly = flag.Bool("print", false, "print the steps of -follow, or the tx list, instead of opening the TUI")
		blocks    = flag.Bool("blocks", false, "with -print and no -follow, list blocks instead of txs")
		typ       = flag.String("type", "", "with -print and no -follow, list only txs of this type, such as CreateSubnetTx, or with -blocks, only blocks that hold one")
		kinds     = flag.String("kinds", "", "comma-separated kinds to show: accept, reject, build, verify, gossip, tip, vote, fuzzer (default all but vote)")
		window    = flag.Duration("window", 20*time.Millisecond, "events of the same op and kind this close to a step's first event join that step")
	)
	flag.Parse()

	paths := flag.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}
	if err := run(paths, *follow, *printOnly, *blocks, *typ, *kinds, *window); err != nil {
		fmt.Fprintln(os.Stderr, "netviz:", err)
		os.Exit(1)
	}
}

func run(paths []string, follow string, printOnly, blocks bool, typ, kinds string, window time.Duration) error {
	events, err := Load(paths)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		return fmt.Errorf("no events found under %s", strings.Join(paths, " "))
	}

	show, err := parseKinds(kinds)
	if err != nil {
		return err
	}

	if !printOnly {
		m := newModel(events, window.Nanoseconds())
		m.show = show
		if follow != "" {
			m.follow(strings.Split(follow, ","))
		}
		_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
		return err
	}

	nodes := Nodes(events)
	switch {
	case follow == "" && blocks:
		fmt.Print(renderBlocks(Blocks(events), nodes, typ, -1, plain))
		return nil
	case follow == "":
		fmt.Print(renderTxs(Txs(events), nodes, typ, -1, plain))
		return nil
	}

	steps := Follow(events, Expand(events, strings.Split(follow, ",")), show, window.Nanoseconds())
	fmt.Print(renderSteps(steps, nodes, -1, plain))
	return nil
}

// parseKinds parses the -kinds flag. Empty means defaultKinds.
func parseKinds(s string) (map[Kind]bool, error) {
	if s == "" {
		return defaultKinds(), nil
	}

	show := make(map[Kind]bool)
	for _, name := range strings.Split(s, ",") {
		found := false
		for _, k := range Kinds {
			if k.String() == name {
				show[k] = true
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown kind %q", name)
		}
	}
	return show, nil
}
