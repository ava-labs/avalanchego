// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/google/btree"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/maybe"
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
	nodes  map[path]*change[*node]
	values map[path]*change[maybe.Maybe[[]byte]]
}

func newChangeSummary(estimatedSize int) *changeSummary {
	return &changeSummary{
		nodes:  make(map[path]*change[*node], estimatedSize),
		values: make(map[path]*change[maybe.Maybe[[]byte]], estimatedSize),
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
func (th *trieHistory) getValueChanges(startRoot, endRoot ids.ID, start []byte, end maybe.Maybe[[]byte], maxLength int) (*changeSummary, error) {
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

	// [startRootChanges] is the last appearance of [startRoot]
	startRootChanges, ok := th.lastChanges[startRoot]
	if !ok {
		return nil, ErrStartRootNotFound
	}

	// startRootChanges is after the lastEndRootChange, but that is just the latest appearance of start root
	// there may be an earlier entry, so attempt to find an entry that comes before lastEndRootChange
	if startRootChanges.index > lastEndRootChange.index {
		th.history.DescendLessOrEqual(
			lastEndRootChange,
			func(item *changeSummaryAndIndex) bool {
				if item == lastEndRootChange {
					return true // Skip first iteration
				}
				if item.rootID == startRoot {
					startRootChanges = item
					return false
				}
				return true
			},
		)
		// There's no change resulting in [startRoot] before the latest change resulting in [endRoot].
		if startRootChanges.index > lastEndRootChange.index {
			return nil, ErrStartRootNotFound
		}
	}

	// Keep changes sorted so the largest can be removed in order to stay within the maxLength limit.
	sortedKeys := btree.NewG(
		2,
		func(a, b path) bool {
			return a.Compare(b) < 0
		},
	)

	startPath := newPath(start)
	endPath := maybe.Bind(end, newPath)

	// For each element in the history in the range between [startRoot]'s
	// last appearance (exclusive) and [endRoot]'s last appearance (inclusive),
	// add the changes to keys in [start, end] to [combinedChanges].
	// Only the key-value pairs with the greatest [maxLength] keys will be kept.
	combinedChanges := newChangeSummary(maxLength)

	// For each change after [startRootChanges] up to and including
	// [lastEndRootChange], record the change in [combinedChanges].
	th.history.AscendGreaterOrEqual(
		startRootChanges,
		func(item *changeSummaryAndIndex) bool {
			if item == startRootChanges {
				// Start from the first change after [startRootChanges].
				return true
			}
			if item.index > lastEndRootChange.index {
				// Don't go past [lastEndRootChange].
				return false
			}

			// Add the changes from this commit to [combinedChanges].
			for key, valueChange := range item.values {
				// The key is outside the range [start, end].
				if (len(startPath) > 0 && key.Compare(startPath) < 0) ||
					(end.HasValue() && key.Compare(endPath.Value()) > 0) {
					continue
				}

				// A change to this key already exists in [combinedChanges]
				// so update its before value with the earlier before value
				if existing, ok := combinedChanges.values[key]; ok {
					existing.after = valueChange.after
					if existing.before.HasValue() == existing.after.HasValue() &&
						bytes.Equal(existing.before.Value(), existing.after.Value()) {
						// The change to this key is a no-op, so remove it from [combinedChanges].
						delete(combinedChanges.values, key)
						sortedKeys.Delete(key)
					}
				} else {
					combinedChanges.values[key] = &change[maybe.Maybe[[]byte]]{
						before: valueChange.before,
						after:  valueChange.after,
					}
					sortedKeys.ReplaceOrInsert(key)
				}
			}
			// continue to next change list
			return true
		})

	// Keep only the smallest [maxLength] items in [combinedChanges.values].
	for sortedKeys.Len() > maxLength {
		if greatestKey, found := sortedKeys.DeleteMax(); found {
			delete(combinedChanges.values, greatestKey)
		}
	}

	return combinedChanges, nil
}

// Returns the changes to go from the current trie state back to the requested [rootID]
// for the keys in [start, end].
// If [start] is nil, all keys are considered > [start].
// If  [end] is nil, all keys are considered < [end].
func (th *trieHistory) getChangesToGetToRoot(rootID ids.ID, start []byte, end maybe.Maybe[[]byte]) (*changeSummary, error) {
	// [lastRootChange] is the last change in the history resulting in [rootID].
	lastRootChange, ok := th.lastChanges[rootID]
	if !ok {
		return nil, ErrRootIDNotPresent
	}

	var (
		startPath       = newPath(start)
		endPath         = maybe.Bind(end, newPath)
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
					(endPath.IsNothing() || key.Compare(endPath.Value()) <= 0) {
					if existing, ok := combinedChanges.values[key]; ok {
						existing.after = valueChange.before
					} else {
						combinedChanges.values[key] = &change[maybe.Maybe[[]byte]]{
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
