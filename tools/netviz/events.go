// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Command netviz shows how txs and blocks move between avalanchego nodes,
// from the events.log files that nodes write with --log-events.
//
// It is its own module, so its TUI dependencies stay out of avalanchego's.
// The repo's go.work does not list it, so build it with GOWORK=off:
//
//	GOWORK=off go build -C tools/netviz -o "$PWD/build/netviz" .
//	./build/netviz path/to/network
package main

import (
	"bufio"
	"cmp"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// An Event is one line of an events.log file. Only the fields that netviz
// reads are decoded; Raw holds the whole line.
type Event struct {
	Timestamp    int64         `json:"timestamp"`
	Node         string        `json:"node"`
	Chain        string        `json:"chain"`
	Op           string        `json:"op"`
	From         string        `json:"from"`
	Gossip       string        `json:"gossip"`      // push, pull_request, pull_response, or pull_failed
	P2PHandler   string        `json:"p2p_handler"` // the app protocol, such as tx_gossip
	Served       []string      `json:"served"`      // IDs a pull request was answered with
	RequestID    *uint32       `json:"request_id"`
	Msg          string        `json:"msg"`
	Err          string        `json:"err"`
	Items        []Item        `json:"items"`
	Built        []Block       `json:"built"`
	Verified     []Block       `json:"verified"`
	Accepted     []Block       `json:"accepted"`
	Rejected     []Block       `json:"rejected"`
	AcceptedTxs  []AcceptedTxs `json:"accepted_txs"`
	Preferred    []Block       `json:"preferred"`
	LastAccepted []Block       `json:"last_accepted"`
	Vote         *Vote         `json:"vote"`
	Sent         []Sent        `json:"sent"`

	// The fuzzer's events (node "fuzzer") also have these. An op sets the
	// tx it sent, where, and how it ended; a check sets the blocks it read.
	Step      int           `json:"step"`
	TxType    string        `json:"tx_type"`
	Tx        string        `json:"tx"`
	To        *Draw         `json:"to"`
	Path      string        `json:"path"`
	Outcome   string        `json:"outcome"`
	DecidedBy string        `json:"decided_by"`
	Scanned   []AcceptedTxs `json:"scanned"`

	Raw string `json:"-"`
}

// An Item is one gossiped tx. ID is empty when the item did not parse. Err is
// empty when the node added it.
type Item struct {
	ID  string `json:"id"`
	Err string `json:"err"`
}

// A Block is a block that an input built, verified, accepted, rejected, or
// left as the node's tip. Err is set only on a failed verification.
type Block struct {
	Block  string `json:"block"`
	Height uint64 `json:"height"`
	Err    string `json:"err"`
}

// AcceptedTxs is the P-Chain block inside an accepted proposervm block, and
// its txs.
type AcceptedTxs struct {
	Block  string `json:"block"`
	Height uint64 `json:"height"`
	Txs    []Tx   `json:"txs"`
}

// A Tx is a tx in an accepted block.
type Tx struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// A Draw is a value that a fuzzer op drew: Ref names it in the fuzzer's
// model, such as "node 1", and ID is the value.
type Draw struct {
	Ref string `json:"ref"`
	ID  string `json:"id"`
}

// A Vote is what a chits message reported.
type Vote struct {
	PreferredAtHeight string `json:"preferred_at_height"`
	Accepted          string `json:"accepted"`
	AcceptedHeight    uint64 `json:"accepted_height"`
}

// Sent is a message that an input sent.
type Sent struct {
	Op        string   `json:"op"`
	RequestID *uint32  `json:"request_id"`
	To        []string `json:"to"`
}

// Load reads the events.log files at paths, and every events.log under paths
// that are directories, and returns their events sorted by timestamp.
func Load(paths []string) ([]Event, error) {
	var events []Event
	for _, path := range paths {
		files, err := logFiles(path)
		if err != nil {
			return nil, err
		}

		for _, file := range files {
			fileEvents, err := loadFile(file)
			if err != nil {
				return nil, err
			}
			events = append(events, fileEvents...)
		}
	}

	slices.SortStableFunc(events, func(a, b Event) int {
		return cmp.Compare(a.Timestamp, b.Timestamp)
	})
	return events, nil
}

// logFiles returns path if it is a file, or every events.log under it if it
// is a directory.
func logFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{path}, nil
	}

	var files []string
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == "events.log" {
			files = append(files, p)
		}
		return nil
	})
	return files, err
}

func loadFile(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var events []Event
	scanner := bufio.NewScanner(f)
	// A line lists every message its input sent, so lines can be long.
	scanner.Buffer(nil, 64<<20)
	for n := 1; scanner.Scan(); n++ {
		line := scanner.Text()
		var e Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, n, err)
		}
		e.Raw = line
		events = append(events, e)
	}
	return events, scanner.Err()
}
