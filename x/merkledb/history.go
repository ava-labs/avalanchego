// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/ava-labs/avalanchego/utils/heap"
	"golang.org/x/exp/maps"
	"slices"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/buffer"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

var (
	ErrInsufficientHistory = errors.New("insufficient history to generate proof")
	ErrNoEndRoot           = fmt.Errorf("%w: end root not found", ErrInsufficientHistory)
)

// stores previous trie states
type trieHistory struct {
	// Root ID --> The most recent change resulting in [rootID].
	lastChanges map[ids.ID]*changeSummaryAndInsertNumber

	// Maximum number of previous roots/changes to store in [history].
	maxHistoryLen int

	// Contains the history.
	// Sorted by increasing order of insertion.
	// Contains at most [maxHistoryLen] values.
	history buffer.Deque[*changeSummaryAndInsertNumber]

	// Each change is tagged with this monotonic increasing number.
	nextInsertNumber uint64
}

// Tracks the beginning and ending state of a value.
type change[T any] struct {
	before T
	after  T
}

// Wrapper around a changeSummary that allows comparison
// of when the change was made.
type changeSummaryAndInsertNumber struct {
	*changeSummary
	// Another changeSummaryAndInsertNumber with a greater
	// [insertNumber] means that change was after this one.
	insertNumber uint64
}

type keyChange struct {
	*change[maybe.Maybe[[]byte]]
	key Key
}

// Tracks all the node and value changes that resulted in the rootID.
type changeSummary struct {
	// The ID of the trie after these changes.
	rootID ids.ID
	// The root before/after this change.
	// Set in [applyValueChanges].
	rootChange       change[maybe.Maybe[*node]]
	nodes            map[Key]*change[*node]
	values           map[Key]*keyChange
	sortedKeyChanges []*keyChange
}

func newChangeSummary(estimatedSize int) *changeSummary {
	return &changeSummary{
		nodes:            make(map[Key]*change[*node], estimatedSize),
		values:           make(map[Key]*keyChange, estimatedSize),
		sortedKeyChanges: make([]*keyChange, 0, estimatedSize),
		rootChange:       change[maybe.Maybe[*node]]{},
	}
}

func newTrieHistory(maxHistoryLookback int) *trieHistory {
	return &trieHistory{
		maxHistoryLen: maxHistoryLookback,
		history:       buffer.NewUnboundedDeque[*changeSummaryAndInsertNumber](maxHistoryLookback),
		lastChanges:   make(map[ids.ID]*changeSummaryAndInsertNumber),
	}
}

// Returns up to [maxLength] sorted changes with keys in
// [start, end] that occurred between [startRoot] and [endRoot].
// If [start] is Nothing, there's no lower bound on the range.
// If [end] is Nothing, there's no upper bound on the range.
// Returns [ErrInsufficientHistory] if the history is insufficient
// to generate the proof.
// Returns [ErrNoEndRoot], which wraps [ErrInsufficientHistory], if
// the [endRoot] isn't in the history.
func (th *trieHistory) getValueChanges(
	startRoot ids.ID,
	endRoot ids.ID,
	start maybe.Maybe[[]byte],
	end maybe.Maybe[[]byte],
	maxLength int,
) ([]*keyChange, error) {
	if maxLength <= 0 {
		return nil, fmt.Errorf("%w but was %d", ErrInvalidMaxLength, maxLength)
	}

	if startRoot == endRoot {
		return []*keyChange{}, nil
	}

	// [endRootChanges] is the last change in the history resulting in [endRoot].
	endRootChanges, ok := th.lastChanges[endRoot]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoEndRoot, endRoot)
	}

	// Confirm there's a change resulting in [startRoot] before
	// a change resulting in [endRoot] in the history.
	// [startRootChanges] is the last appearance of [startRoot].
	startRootChanges, ok := th.lastChanges[startRoot]
	if !ok {
		return nil, fmt.Errorf("%w: start root %s not found", ErrInsufficientHistory, startRoot)
	}

	var (
		// The insert number of the last element in [th.history].
		mostRecentChangeInsertNumber = th.nextInsertNumber - 1

		// The index within [th.history] of its last element.
		mostRecentChangeIndex = th.history.Len() - 1

		// The difference between the last index in [th.history] and the index of [endRootChanges].
		endToMostRecentOffset = int(mostRecentChangeInsertNumber - endRootChanges.insertNumber)

		// The index in [th.history] of the latest change resulting in [endRoot].
		endRootIndex = mostRecentChangeIndex - endToMostRecentOffset
	)

	if startRootChanges.insertNumber > endRootChanges.insertNumber {
		// [startRootChanges] happened after [endRootChanges].
		// However, that is just the *latest* change resulting in [startRoot].
		// Attempt to find a change resulting in [startRoot] before [endRootChanges].
		//
		// Translate the insert number to the index in [th.history] so we can iterate
		// backward from [endRootChanges].
		for i := endRootIndex - 1; i >= 0; i-- {
			changes, _ := th.history.Index(i)

			if changes.rootID == startRoot {
				// [startRootChanges] is now the last change resulting in
				// [startRoot] before [endRootChanges].
				startRootChanges = changes
				break
			}

			if i == 0 {
				return nil, fmt.Errorf(
					"%w: start root %s not found before end root %s",
					ErrInsufficientHistory, startRoot, endRoot,
				)
			}
		}
	}

	// historyChangesIndex is used for tracking keyChanges index from each historical root.
	type historyChangesIndex struct {
		changes       *changeSummaryAndInsertNumber
		kvChangeIndex int
	}

	// historyChangesIndexHeap is used to traverse the changes sorted by ASC [key] and ASC [insertNumber].
	historyChangesIndexHeap := heap.NewQueue[*historyChangesIndex](func(a, b *historyChangesIndex) bool {
		keyComparison := a.changes.sortedKeyChanges[a.kvChangeIndex].key.Compare(b.changes.sortedKeyChanges[b.kvChangeIndex].key)
		if keyComparison != 0 {
			return keyComparison < 0
		}

		return a.changes.insertNumber < b.changes.insertNumber
	})

	var (
		startKey = maybe.Bind(start, ToKey)
		endKey   = maybe.Bind(end, ToKey)

		// For each element in the history in the range between [startRoot]'s
		// last appearance (exclusive) and [endRoot]'s last appearance (inclusive),
		// add the changes to keys in [start, end] to [combinedChanges].
		// Only the key-value pairs with the greatest [maxLength] keys will be kept.
		// The difference between the index of [startRootChanges] and [endRootChanges] in [th.history].
		startToEndOffset = int(endRootChanges.insertNumber - startRootChanges.insertNumber)

		// The index of the last change resulting in [startRoot]
		// which occurs before [endRootChanges].
		startRootIndex = endRootIndex - startToEndOffset
	)

	// Push in the heap first key in [startKey, endKey] for each historical root.
	for i := startRootIndex + 1; i <= endRootIndex; i++ {
		historyChanges, ok := th.history.Index(i)
		if !ok {
			return nil, fmt.Errorf("missing history changes at index %d", i)
		}

		startKeyIndex := 0
		if startKey.HasValue() {
			// Binary search for [startKey] index.
			startKeyIndex, _ = slices.BinarySearchFunc(historyChanges.sortedKeyChanges, startKey, func(k *keyChange, m maybe.Maybe[Key]) int {
				return k.key.Compare(m.Value())
			})

			if startKeyIndex >= len(historyChanges.sortedKeyChanges) {
				// [startKey] is after last key of [sortedKeyChanges].
				continue
			}
		}

		keyChange := historyChanges.sortedKeyChanges[startKeyIndex]
		if end.HasValue() && keyChange.key.Greater(endKey.Value()) {
			// [keyChange] is after [endKey].
			continue
		}

		// [startKeyIndex] is the index of the first key in [startKey, endKey] from [sortedKeyChanges].
		historyChangesIndexHeap.Push(&historyChangesIndex{
			changes:       historyChanges,
			kvChangeIndex: startKeyIndex,
		})
	}

	var (
		combinedKeyChanges = make([]*keyChange, 0, maxLength)

		// Used for combining the changes of all the historical changes, for the current smallest key.
		currentKeyChange *keyChange
	)

	for historyChangesIndexHeap.Len() > 0 {
		historyRootChanges, _ := historyChangesIndexHeap.Pop()
		kvChange := historyRootChanges.changes.sortedKeyChanges[historyRootChanges.kvChangeIndex]

		if end.HasValue() && kvChange.key.Greater(endKey.Value()) {
			// Skip processing the current [historyRootChanges] if we are after [endKey].
			continue
		}

		if len(historyRootChanges.changes.sortedKeyChanges) > 1+historyRootChanges.kvChangeIndex {
			// If there are remaining changes in the current [historyRootChanges], push to minheap.
			historyRootChanges.kvChangeIndex++
			historyChangesIndexHeap.Push(historyRootChanges)
		}

		if currentKeyChange != nil {
			if currentKeyChange.key.value == kvChange.key.value {
				// Same key, update [after] value.
				currentKeyChange.after = kvChange.after

				continue
			}

			// New key.
			if !bytes.Equal(currentKeyChange.before.Value(), currentKeyChange.after.Value()) {
				// If the value has changed, add [currentKeyChange] to [combinedKeyChanges].
				combinedKeyChanges = append(combinedKeyChanges, currentKeyChange)

				if len(combinedKeyChanges) >= maxLength {
					// If we have [maxLength] changes, we can return the current [combinedKeyChanges].
					return combinedKeyChanges, nil
				}
			}
		}

		currentKeyChange = &keyChange{
			change: &change[maybe.Maybe[[]byte]]{
				before: kvChange.before,
				after:  kvChange.after,
			},
			key: kvChange.key,
		}
	}

	// Add the last [currentKeyChange] to [valueChanges] if needed.
	if currentKeyChange != nil && !bytes.Equal(currentKeyChange.before.Value(), currentKeyChange.after.Value()) {
		combinedKeyChanges = append(combinedKeyChanges, currentKeyChange)
	}

	return combinedKeyChanges, nil
}

// Returns the changes to go from the current trie state back to the requested [rootID]
// for the keys in [start, end].
// If [start] is Nothing, all keys are considered > [start].
// If [end] is Nothing, all keys are considered < [end].
func (th *trieHistory) getChangesToGetToRoot(rootID ids.ID, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte]) (*changeSummary, error) {
	// [lastRootChange] is the last change in the historyChanges resulting in [rootID].
	lastRootChange, ok := th.lastChanges[rootID]
	if !ok {
		return nil, ErrInsufficientHistory
	}

	var (
		startKey                     = maybe.Bind(start, ToKey)
		endKey                       = maybe.Bind(end, ToKey)
		combinedChanges              = newChangeSummary(defaultPreallocationSize)
		mostRecentChangeInsertNumber = th.nextInsertNumber - 1
		mostRecentChangeIndex        = th.history.Len() - 1
		offset                       = int(mostRecentChangeInsertNumber - lastRootChange.insertNumber)
		lastRootChangeIndex          = mostRecentChangeIndex - offset
	)

	// Go backward from the most recent change in the history up to but
	// not including the last change resulting in [rootID].
	// Record each change in [combinedChanges].
	for i := mostRecentChangeIndex; i > lastRootChangeIndex; i-- {
		changes, _ := th.history.Index(i)

		if i == mostRecentChangeIndex {
			combinedChanges.rootChange.before = changes.rootChange.after
		}
		if i == lastRootChangeIndex+1 {
			combinedChanges.rootChange.after = changes.rootChange.before
		}

		for key, changedNode := range changes.nodes {
			combinedChanges.nodes[key] = &change[*node]{
				after: changedNode.before,
			}
		}

		startKeyIndex := 0
		if startKey.HasValue() {
			// Binary search for [startKey] index.
			startKeyIndex, _ = slices.BinarySearchFunc(changes.sortedKeyChanges, startKey, func(k *keyChange, m maybe.Maybe[Key]) int {
				return k.key.Compare(m.Value())
			})
		}

		for _, kc := range changes.sortedKeyChanges[startKeyIndex:] {
			if end.HasValue() && kc.key.Greater(endKey.Value()) {
				break
			}

			if existing, ok := combinedChanges.values[kc.key]; ok {
				existing.after = kc.before
			} else {
				keyChange := keyChange{
					change: &change[maybe.Maybe[[]byte]]{
						before: kc.after,
						after:  kc.before,
					},
					key: kc.key,
				}

				combinedChanges.values[kc.key] = &keyChange
			}
		}
	}

	sortedKeys := maps.Keys(combinedChanges.values)
	slices.SortFunc(sortedKeys, func(a, b Key) int {
		return a.Compare(b)
	})

	for _, key := range sortedKeys {
		combinedChanges.sortedKeyChanges = append(combinedChanges.sortedKeyChanges, combinedChanges.values[key])
	}

	return combinedChanges, nil
}

// record the provided set of changes in the history
func (th *trieHistory) record(changes *changeSummary) {
	// we aren't recording history so noop
	if th.maxHistoryLen == 0 {
		return
	}

	if th.history.Len() == th.maxHistoryLen {
		// This change causes us to go over our lookback limit.
		// Remove the oldest set of changes.
		oldestEntry, _ := th.history.PopLeft()

		latestChange := th.lastChanges[oldestEntry.rootID]
		if latestChange == oldestEntry {
			// The removed change was the most recent resulting in this root ID.
			delete(th.lastChanges, oldestEntry.rootID)
		}
	}

	changesAndIndex := &changeSummaryAndInsertNumber{
		changeSummary: changes,
		insertNumber:  th.nextInsertNumber,
	}
	th.nextInsertNumber++

	// Add [changes] to the sorted change list.
	_ = th.history.PushRight(changesAndIndex)

	// Mark that this is the most recent change resulting in [changes.rootID].
	th.lastChanges[changes.rootID] = changesAndIndex
}
