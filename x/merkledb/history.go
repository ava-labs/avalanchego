// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/buffer"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/utils/set"
)

var ErrInsufficientHistory = errors.New("insufficient history to generate proof")

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
		history:       buffer.NewUnboundedDeque[*changeSummaryAndInsertNumber](maxHistoryLookback),
		lastChanges:   make(map[ids.ID]*changeSummaryAndInsertNumber),
	}
}

// Returns up to [maxLength] key-value pair changes with keys in
// [start, end] that occurred between [startRoot] and [endRoot].
// Returns [ErrInsufficientHistory] if the history is insufficient
// to generate the proof.
func (th *trieHistory) getValueChanges(
	startRoot ids.ID,
	endRoot ids.ID,
	start []byte,
	end maybe.Maybe[[]byte],
	maxLength int,
) (*changeSummary, error) {
	if maxLength <= 0 {
		return nil, fmt.Errorf("%w but was %d", ErrInvalidMaxLength, maxLength)
	}

	if startRoot == endRoot {
		return newChangeSummary(maxLength), nil
	}

	// [endRootChanges] is the last change in the history resulting in [endRoot].
	// TODO when we update to minimum go version 1.20.X, make this return another
	// wrapped error ErrNoEndRoot. In NetworkServer.HandleChangeProofRequest, if we return
	// that error, we know we shouldn't try to generate a range proof since we
	// lack the necessary history.
	endRootChanges, ok := th.lastChanges[endRoot]
	if !ok {
		return nil, fmt.Errorf("%w: end root %s not found", ErrInsufficientHistory, endRoot)
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

	var (
		// Keep track of changed keys so the largest can be removed
		// in order to stay within the [maxLength] limit if necessary.
		changedKeys = set.Set[path]{}

		startPath = newPath(start)
		endPath   = maybe.Bind(end, newPath)

		// For each element in the history in the range between [startRoot]'s
		// last appearance (exclusive) and [endRoot]'s last appearance (inclusive),
		// add the changes to keys in [start, end] to [combinedChanges].
		// Only the key-value pairs with the greatest [maxLength] keys will be kept.
		combinedChanges = newChangeSummary(maxLength)

		// The difference between the index of [startRootChanges] and [endRootChanges] in [th.history].
		startToEndOffset = int(endRootChanges.insertNumber - startRootChanges.insertNumber)

		// The index of the last change resulting in [startRoot]
		// which occurs before [endRootChanges].
		startRootIndex = endRootIndex - startToEndOffset
	)

	// For each change after [startRootChanges] up to and including
	// [endRootChanges], record the change in [combinedChanges].
	for i := startRootIndex + 1; i <= endRootIndex; i++ {
		changes, _ := th.history.Index(i)

		// Add the changes from this commit to [combinedChanges].
		for key, valueChange := range changes.values {
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
					changedKeys.Remove(key)
				}
			} else {
				combinedChanges.values[key] = &change[maybe.Maybe[[]byte]]{
					before: valueChange.before,
					after:  valueChange.after,
				}
				changedKeys.Add(key)
			}
		}
	}

	// If we have <= [maxLength] elements, we're done.
	if changedKeys.Len() <= maxLength {
		return combinedChanges, nil
	}

	// Keep only the smallest [maxLength] items in [combinedChanges.values].
	sortedChangedKeys := changedKeys.List()
	utils.Sort(sortedChangedKeys)
	for len(sortedChangedKeys) > maxLength {
		greatestKey := sortedChangedKeys[len(sortedChangedKeys)-1]
		sortedChangedKeys = sortedChangedKeys[:len(sortedChangedKeys)-1]
		delete(combinedChanges.values, greatestKey)
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
		return nil, ErrInsufficientHistory
	}

	var (
		startPath                    = newPath(start)
		endPath                      = maybe.Bind(end, newPath)
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

		for key, changedNode := range changes.nodes {
			combinedChanges.nodes[key] = &change[*node]{
				after: changedNode.before,
			}
		}

		for key, valueChange := range changes.values {
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
