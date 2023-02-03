// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"errors"
	"fmt"

	"github.com/google/btree"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	ErrStartRootNotFound = errors.New("start root is not before end root in history")
	ErrRootIDNotPresent  = errors.New("root id is not present in history")
)

// stores previous trie states
type trieHistory struct {
	// Root ID --> The most recent change resulting in [rootID].
	lastChanges map[ids.ID]*changeSummaryAndIndex

	// Maximum number of previous roots/changes to store in [history].
	maxHistoryLen int

	// Contains the history.
	// Sorted by increasing order of insertion.
	// Contains at most [maxHistoryLen] values.
	history *btree.BTreeG[*changeSummaryAndIndex]

	nextIndex uint64
}

// Tracks the beginning and ending state of a value.
type change[T any] struct {
	before T
	after  T
}

// Wrapper around a changeSummary that allows comparison
// of when the change was made.
type changeSummaryAndIndex struct {
	*changeSummary
	// Another changeSummaryAndIndex with a greater
	// [index] means that change was after this one.
	index uint64
}

// Tracks all of the node and value changes that resulted in the rootID.
type changeSummary struct {
	rootID ids.ID
	// key is path prefix
	nodes map[path]*change[*node]
	// key is full path
	values map[path]*change[Maybe[[]byte]]
}

func newChangeSummary(estimatedSize int) *changeSummary {
	return &changeSummary{
		nodes:  make(map[path]*change[*node], estimatedSize),
		values: make(map[path]*change[Maybe[[]byte]], estimatedSize),
	}
}

func newTrieHistory(maxHistoryLookback int) *trieHistory {
	return &trieHistory{
		maxHistoryLen: maxHistoryLookback,
		history: btree.NewG(
			2,
			func(a, b *changeSummaryAndIndex) bool {
				return a.index < b.index
			},
		),
		lastChanges: make(map[ids.ID]*changeSummaryAndIndex),
	}
}

// Returns up to [maxLength] key-value pair changes with keys in [start, end] that
// occurred between [startRoot] and [endRoot].
func (th *trieHistory) getValueChanges(startRoot, endRoot ids.ID, start, end []byte, maxLength int) (*changeSummary, error) {
	if maxLength <= 0 {
		return nil, fmt.Errorf("%w but was %d", ErrInvalidMaxLength, maxLength)
	}

	if startRoot == endRoot {
		return newChangeSummary(maxLength), nil
	}

	// Confirm there's a change resulting in [startRoot] before
	// a change resulting in [endRoot] in the history.
	// [lastEndRootChange] is the last change in the history resulting in [endRoot].
	lastEndRootChange, ok := th.lastChanges[endRoot]
	if !ok {
		return nil, ErrRootIDNotPresent
	}

	// [lastStartRootChange] is the latest appearance of [startRoot]
	// which came before [lastEndRootChange].
	var lastStartRootChange *changeSummaryAndIndex
	th.history.DescendLessOrEqual(
		lastEndRootChange,
		func(item *changeSummaryAndIndex) bool {
			if item == lastEndRootChange {
				return true // Skip first iteration
			}
			if item.rootID == startRoot {
				lastStartRootChange = item
				return false
			}
			return true
		},
	)

	// There's no change resulting in [startRoot] before the latest change resulting in [endRoot].
	if lastStartRootChange == nil {
		return nil, ErrStartRootNotFound
	}

	// Keep changes sorted so the largest can be removed in order to stay within the maxLength limit.
	sortedKeys := btree.NewG(
		2,
		func(a, b path) bool {
			return a.Compare(b) < 0
		},
	)

	startPath := newPath(start)
	endPath := newPath(end)

	// For each element in the history in the range between [startRoot]'s
	// last appearance (exclusive) and [endRoot]'s last appearance (inclusive),
	// add the changes to keys in [start, end] to [combinedChanges].
	// Only the key-value pairs with the greatest [maxLength] keys will be kept.
	combinedChanges := newChangeSummary(maxLength)

	// For each change after [lastStartRootChange] up to and including
	// [lastEndRootChange], record the change in [combinedChanges].
	th.history.AscendGreaterOrEqual(
		lastStartRootChange,
		func(item *changeSummaryAndIndex) bool {
			if item == lastStartRootChange {
				// Start from the first change after [lastStartRootChange].
				return true
			}
			if item.index > lastEndRootChange.index {
				// Don't go past [lastEndRootChange].
				return false
			}

			for key, valueChange := range item.values {
				if (len(startPath) == 0 || key.Compare(startPath) >= 0) &&
					(len(endPath) == 0 || key.Compare(endPath) <= 0) {
					if existing, ok := combinedChanges.values[key]; ok {
						existing.after = valueChange.after
					} else {
						combinedChanges.values[key] = &change[Maybe[[]byte]]{
							before: valueChange.before,
							after:  valueChange.after,
						}
					}
					sortedKeys.ReplaceOrInsert(key)
				}
			}

			// Keep only the smallest [maxLength] items in [combinedChanges.values].
			for sortedKeys.Len() > maxLength {
				if greatestKey, found := sortedKeys.DeleteMax(); found {
					delete(combinedChanges.values, greatestKey)
				}
			}

			return true
		},
	)
	return combinedChanges, nil
}

// Returns the changes to go from the current trie state back to the requested [rootID]
// for the keys in [start, end].
// If [start] is nil, all keys are considered > [start].
// If  [end] is nil, all keys are considered < [end].
func (th *trieHistory) getChangesToGetToRoot(rootID ids.ID, start, end []byte) (*changeSummary, error) {
	// [lastRootChange] is the last change in the history resulting in [rootID].
	lastRootChange, ok := th.lastChanges[rootID]
	if !ok {
		return nil, ErrRootIDNotPresent
	}

	var (
		startPath       = newPath(start)
		endPath         = newPath(end)
		combinedChanges = newChangeSummary(defaultPreallocationSize)
	)

	// Go backward from the most recent change in the history up to but
	// not including the last change resulting in [rootID].
	// Record each change in [combinedChanges].
	th.history.Descend(
		func(item *changeSummaryAndIndex) bool {
			if item == lastRootChange {
				return false
			}
			for key, changedNode := range item.nodes {
				combinedChanges.nodes[key] = &change[*node]{
					after: changedNode.before,
				}
			}

			for key, valueChange := range item.values {
				if (len(startPath) == 0 || key.Compare(startPath) >= 0) &&
					(len(endPath) == 0 || key.Compare(endPath) <= 0) {
					if existing, ok := combinedChanges.values[key]; ok {
						existing.after = valueChange.before
					} else {
						combinedChanges.values[key] = &change[Maybe[[]byte]]{
							before: valueChange.after,
							after:  valueChange.before,
						}
					}
				}
			}
			return true
		},
	)
	return combinedChanges, nil
}

// record the provided set of changes in the history
func (th *trieHistory) record(changes *changeSummary) {
	// we aren't recording history so noop
	if th.maxHistoryLen == 0 {
		return
	}

	for th.history.Len() == th.maxHistoryLen {
		// This change causes us to go over our lookback limit.
		// Remove the oldest set of changes.
		oldestEntry, _ := th.history.DeleteMin()
		latestChange := th.lastChanges[oldestEntry.rootID]
		if latestChange == oldestEntry {
			// The removed change was the most recent resulting in this root ID.
			delete(th.lastChanges, oldestEntry.rootID)
		}
	}

	changesAndIndex := &changeSummaryAndIndex{
		changeSummary: changes,
		index:         th.nextIndex,
	}
	th.nextIndex++

	// Add [changes] to the sorted change list.
	_, _ = th.history.ReplaceOrInsert(changesAndIndex)
	// Mark that this is the most recent change resulting in [changes.rootID].
	th.lastChanges[changes.rootID] = changesAndIndex
}
