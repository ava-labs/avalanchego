// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"testing"

	"github.com/ava-labs/avalanchego/graft/evm/sync/synctest"
)

// TestSimpleSyncCases runs the standard simple sync test cases using coreth's snapshot implementation.
func TestSimpleSyncCases(t *testing.T) {
	harness := NewCorethTestHarness()
	testCases := synctest.GetSimpleSyncTestCases(250, 10)

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			synctest.RunSyncTest(t, harness, test)
		})
	}
}

// TestCancelSync tests that sync can be cancelled mid-operation.
func TestCancelSync(t *testing.T) {
	synctest.RunCancelSyncTest(t, NewCorethTestHarness())
}

// TestResumeSyncAccountsTrieInterrupted tests resuming sync after accounts trie is interrupted.
func TestResumeSyncAccountsTrieInterrupted(t *testing.T) {
	synctest.RunResumeSyncAccountsTrieInterruptedTest(t, NewCorethTestHarness())
}

// TestResumeSyncLargeStorageTrieInterrupted tests resuming sync after large storage trie is interrupted.
func TestResumeSyncLargeStorageTrieInterrupted(t *testing.T) {
	synctest.RunResumeSyncLargeStorageTrieInterruptedTest(t, NewCorethTestHarness())
}

// TestResumeSyncToNewRootAfterLargeStorageTrieInterrupted tests resuming to a new root after interruption.
func TestResumeSyncToNewRootAfterLargeStorageTrieInterrupted(t *testing.T) {
	synctest.RunResumeSyncToNewRootAfterLargeStorageTrieInterruptedTest(t, NewCorethTestHarness())
}

// TestResumeSyncLargeStorageTrieWithConsecutiveDuplicatesInterrupted tests resuming with consecutive duplicates.
func TestResumeSyncLargeStorageTrieWithConsecutiveDuplicatesInterrupted(t *testing.T) {
	synctest.RunResumeSyncLargeStorageTrieWithConsecutiveDuplicatesInterruptedTest(t, NewCorethTestHarness())
}

// TestResumeSyncLargeStorageTrieWithSpreadOutDuplicatesInterrupted tests resuming with spread out duplicates.
func TestResumeSyncLargeStorageTrieWithSpreadOutDuplicatesInterrupted(t *testing.T) {
	synctest.RunResumeSyncLargeStorageTrieWithSpreadOutDuplicatesInterruptedTest(t, NewCorethTestHarness())
}

// TestResyncNewRootAfterDeletes runs resync tests after various delete scenarios.
func TestResyncNewRootAfterDeletes(t *testing.T) {
	harness := NewCorethTestHarness()
	testCases := synctest.GetResyncNewRootAfterDeletesTestCases()

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			t.Parallel()
			synctest.RunResyncNewRootAfterDeletesTest(t, harness, testCase)
		})
	}
}
