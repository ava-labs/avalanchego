// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"fmt"
	"slices"

	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/database/merkle/sync"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/buffer"
	"github.com/ava-labs/avalanchego/utils/heap"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

// stores previous trie states
type trieHistory struct {
	// Root ID --> The most recent insert number resulting in [rootID].
	lastChangesInsertNumber map[ids.ID]uint64

	// Maximum number of previous roots/changes to store in [history].
	maxHistoryLen int

	// Contains the history.
	// Sorted by increasing order of insertion.
	// Contains at most [maxHistoryLen] values.
	history buffer.Deque[*changeSummaryAndInsertNumber]
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

// Tracks all the node and value changes that resulted in the rootID.
type changeSummary struct {
	// The ID of the trie after these changes.
	rootID ids.ID
	// The root before/after this change.
	// Set in [applyValueChanges].
	rootChange change[maybe.Maybe[*node]]
	nodes      map[Key]*change[*node]

	keyChanges map[Key]*change[maybe.Maybe[[]byte]]
	sortedKeys []Key
}

func newChangeSummary(estimatedSize int) *changeSummary {
	return &changeSummary{
		nodes:      make(map[Key]*change[*node], estimatedSize),
		keyChanges: make(map[Key]*change[maybe.Maybe[[]byte]], estimatedSize),
		sortedKeys: make([]Key, 0, estimatedSize),
		rootChange: change[maybe.Maybe[*node]]{},
	}
}

func newTrieHistory(maxHistoryLookback int) *trieHistory {
	return &trieHistory{
		maxHistoryLen:           maxHistoryLookback,
		history:                 buffer.NewUnboundedDeque[*changeSummaryAndInsertNumber](maxHistoryLookback),
		lastChangesInsertNumber: make(map[ids.ID]uint64),
	}
}

func (th *trieHistory) getNextInsertNumber() uint64 {
	if oldestEntry, ok := th.history.PeekRight(); ok {
		return oldestEntry.insertNumber + 1
	}

	return 0
}

func (th *trieHistory) getRootChanges(root ids.ID) (*changeSummaryAndInsertNumber, bool) {
	insertNumber, ok := th.lastChangesInsertNumber[root]
	if !ok {
		return nil, false
	}

	mostRecentChangeInsertNumber := th.getNextInsertNumber() - 1

	// The difference between the last index in [th.history] and the index of [rootChanges].
	rootToMostRecentOffset := int(mostRecentChangeInsertNumber - insertNumber)

	mostRecentChangeIndex := th.history.Len() - 1

	// The index in [th.history] of the latest change resulting in [root].
	index := mostRecentChangeIndex - rootToMostRecentOffset

	rootChanges, ok := th.history.Index(index)
	if !ok {
		panic("root changes not found in history")
	}

	return rootChanges, true
}

type valueChange struct {
	key    Key
	change *change[maybe.Maybe[[]byte]]
}

// Returns up to [maxLength] sorted changes with keys in
// [start, end] that occurred between [startRoot] and [endRoot].
// If [start] is Nothing, there's no lower bound on the range.
// If [end] is Nothing, there's no upper bound on the range.
// Returns [sync.ErrInsufficientHistory] if the history is insufficient
// to generate the proof.
// Returns [sync.ErrNoEndRoot], if the history doesn't contain the [endRootID].
func (th *trieHistory) getValueChanges(
	startRoot ids.ID,
	endRoot ids.ID,
	start maybe.Maybe[[]byte],
	end maybe.Maybe[[]byte],
	maxLength int,
) ([]valueChange, error) {
	if maxLength <= 0 {
		return nil, fmt.Errorf("%w but was %d", ErrInvalidMaxLength, maxLength)
	}

	if startRoot == endRoot {
		return []valueChange{}, nil
	}

	// [endRootChanges] is the last change in the history resulting in [endRoot].
	endRootChanges, ok := th.getRootChanges(endRoot)
	if !ok {
		return nil, fmt.Errorf("%w: %s", sync.ErrNoEndRoot, endRoot)
	}

	// Confirm there's a change resulting in [startRoot] before
	// a change resulting in [endRoot] in the history.
	// [startRootChanges] is the last appearance of [startRoot].
	startRootChanges, ok := th.getRootChanges(startRoot)
	if !ok {
		return nil, fmt.Errorf("%w: start root %s not found", sync.ErrInsufficientHistory, startRoot)
	}

	var (
		// The insert number of the last element in [th.history].
		mostRecentChangeInsertNumber = th.getNextInsertNumber() - 1

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
					sync.ErrInsufficientHistory, startRoot, endRoot,
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
	historyChangesIndexHeap := heap.NewQueue[historyChangesIndex](func(a, b historyChangesIndex) bool {
		keyComparison := a.changes.sortedKeys[a.kvChangeIndex].Compare(b.changes.sortedKeys[b.kvChangeIndex])
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
			panic(fmt.Sprintf("missing history changes at index %d", i))
		}

		startKeyIndex := 0
		if startKey.HasValue() {
			// Binary search for [startKey] index, or the index where [startKey] would appear.
			startKeyIndex, _ = slices.BinarySearchFunc(historyChanges.sortedKeys, startKey, func(k Key, m maybe.Maybe[Key]) int {
				return k.Compare(m.Value())
			})

			if startKeyIndex >= len(historyChanges.keyChanges) {
				// [startKey] is after last key of [sortedKeyChanges].
				continue
			}
		}

		keyChange := historyChanges.sortedKeys[startKeyIndex]
		if end.HasValue() && keyChange.Greater(endKey.Value()) {
			// [keyChange] is after [endKey].
			continue
		}

		// [startKeyIndex] is the index of the first key in [startKey, endKey] from [sortedKeyChanges].
		historyChangesIndexHeap.Push(historyChangesIndex{
			changes:       historyChanges,
			kvChangeIndex: startKeyIndex,
		})
	}

	var (
		combinedKeyChanges = make([]valueChange, 0, maxLength)

		// Used for combining the changes of all the historical changes, for the current smallest key.
		currentKeyChange *valueChange
	)

	for historyChangesIndexHeap.Len() > 0 {
		historyRootChanges, _ := historyChangesIndexHeap.Pop()
		kvChangeKey := historyRootChanges.changes.sortedKeys[historyRootChanges.kvChangeIndex]
		kvChange := historyRootChanges.changes.keyChanges[kvChangeKey]

		if end.HasValue() && kvChangeKey.Greater(endKey.Value()) {
			// Skip processing the current [historyRootChanges] if we are after [endKey].
			continue
		}

		if len(historyRootChanges.changes.keyChanges) > 1+historyRootChanges.kvChangeIndex {
			// If there are remaining changes in the current [historyRootChanges], push to minheap.
			historyRootChanges.kvChangeIndex++
			historyChangesIndexHeap.Push(historyRootChanges)
		}

		if currentKeyChange != nil {
			if currentKeyChange.key.value == kvChangeKey.value {
				// Same key, update [after] value.
				currentKeyChange.change.after = kvChange.after

				continue
			}

			// New key

			// Add the last [currentKeyChange] to [combinedKeyChanges] if there is an actual change.
			if !maybe.Equal(currentKeyChange.change.before, currentKeyChange.change.after, bytes.Equal) {
				combinedKeyChanges = append(combinedKeyChanges, *currentKeyChange)

				if len(combinedKeyChanges) >= maxLength {
					// If we have [maxLength] changes, we can return the current [combinedKeyChanges].
					return combinedKeyChanges, nil
				}
			}
		}

		currentKeyChange = &valueChange{
			change: &change[maybe.Maybe[[]byte]]{
				before: kvChange.before,
				after:  kvChange.after,
			},
			key: kvChangeKey,
		}
	}

	if currentKeyChange != nil {
		// Add the last [currentKeyChange] to [combinedKeyChanges] if there is an actual change.
		if !maybe.Equal(currentKeyChange.change.before, currentKeyChange.change.after, bytes.Equal) {
			combinedKeyChanges = append(combinedKeyChanges, *currentKeyChange)
		}
	}

	return combinedKeyChanges, nil
}

// Returns the changes to go from the current trie state back to the requested [rootID]
// for the keys in [start, end].
// If [start] is Nothing, all keys are considered > [start].
// If [end] is Nothing, all keys are considered < [end].
func (th *trieHistory) getChangesToGetToRoot(rootID ids.ID, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte]) (*changeSummary, error) {
	// [lastRootChange] is the last change in the history resulting in [rootID].
	lastRootChange, ok := th.getRootChanges(rootID)
	if !ok {
		return nil, sync.ErrInsufficientHistory
	}

	var (
		startKey                     = maybe.Bind(start, ToKey)
		endKey                       = maybe.Bind(end, ToKey)
		combinedChanges              = newChangeSummary(defaultPreallocationSize)
		mostRecentChangeInsertNumber = th.getNextInsertNumber() - 1
		mostRecentChangeIndex        = th.history.Len() - 1
		offset                       = int(mostRecentChangeInsertNumber - lastRootChange.insertNumber)
		lastRootChangeIndex          = mostRecentChangeIndex - offset
		keyChanges                   = map[Key]*change[maybe.Maybe[[]byte]]{}
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
			startKeyIndex, _ = slices.BinarySearchFunc(changes.sortedKeys, startKey, func(k Key, m maybe.Maybe[Key]) int {
				return k.Compare(m.Value())
			})
		}

		for _, key := range changes.sortedKeys[startKeyIndex:] {
			if end.HasValue() && key.Greater(endKey.Value()) {
				break
			}

			if existing, ok := keyChanges[key]; ok {
				// Update existing [after] with current [before]
				existing.after = changes.keyChanges[key].before

				continue
			}

			keyChanges[key] = &change[maybe.Maybe[[]byte]]{
				before: changes.keyChanges[key].after,
				after:  changes.keyChanges[key].before,
			}
		}
	}

	sortedKeys := maps.Keys(keyChanges)
	slices.SortFunc(sortedKeys, func(a, b Key) int {
		return a.Compare(b)
	})

	for _, key := range sortedKeys {
		if maybe.Equal(keyChanges[key].before, keyChanges[key].after, bytes.Equal) {
			// Remove changed key, if there is a no-op.
			delete(keyChanges, key)

			continue
		}

		combinedChanges.keyChanges[key] = keyChanges[key]
		combinedChanges.sortedKeys = append(combinedChanges.sortedKeys, key)
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

		if th.lastChangesInsertNumber[oldestEntry.rootID] == oldestEntry.insertNumber {
			// The removed change was the most recent resulting in this root ID.
			// (Note: this if is for situations when the same root could appear twice in history)
			delete(th.lastChangesInsertNumber, oldestEntry.rootID)
		}
	}

	changesAndIndex := &changeSummaryAndInsertNumber{
		changeSummary: changes,
		insertNumber:  th.getNextInsertNumber(),
	}

	// Add [changes] to the sorted change list.
	_ = th.history.PushRight(changesAndIndex)

	// Mark that this is the most recent change resulting in [changes.rootID].
	th.lastChangesInsertNumber[changes.rootID] = changesAndIndex.insertNumber
}
