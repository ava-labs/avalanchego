// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

// defaultMaxConsecutiveFailures is how many proof requests in a row, across
// all workers, may fail before the sync concludes that no peer serves the
// target root.
const defaultMaxConsecutiveFailures = 100

var errNoPeerServesRoot = errors.New("no peer serves the sync target root")

// failures tracks consecutive failed proof requests across all workers. Any
// well-formed proof response resets it, so only an unbroken run of failures
// counts.
type failures struct {
	mu       sync.Mutex
	count    int
	peers    set.Set[ids.NodeID]
	lastPeer ids.NodeID
	lastErr  error
}

// record notes a failed request and returns the length of the current run.
func (f *failures) record(nodeID ids.NodeID, err error) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.count++
	f.peers.Add(nodeID)
	f.lastPeer = nodeID
	f.lastErr = err
	return f.count
}

func (f *failures) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.count = 0
	f.peers.Clear()
	f.lastPeer = ids.EmptyNodeID
	f.lastErr = nil
}

// exceeded returns an [errNoPeerServesRoot] error describing the current run
// once it reaches limit, and nil before that. accepted is the number of
// proofs committed so far: zero means the root was never served, nonzero
// means peers stopped serving it mid-sync. peers describes the peers that
// could still be selected.
func (f *failures) exceeded(limit int, root ids.ID, accepted uint64, peers peerCounts) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.count < limit {
		return nil
	}
	across := fmt.Sprintf("%d peers", f.peers.Len())
	if peers.ok {
		across = fmt.Sprintf("%d of %d connected peers (%d responsive)", f.peers.Len(), peers.connected, peers.responsive)
	}
	return fmt.Errorf("%w %s: %d consecutive proof requests failed across %s after %d proofs were accepted; last error from %s: %w",
		errNoPeerServesRoot, root, f.count, across, accepted, f.lastPeer, f.lastErr)
}

// peerCounts is a snapshot of the peers the proof client can select from.
type peerCounts struct {
	connected  int
	responsive int
	ok         bool // false if the client cannot report counts
}
