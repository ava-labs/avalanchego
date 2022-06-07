// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"encoding/binary"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

var _ SyncETA = &syncETA{}

const updateInterval = 1 * time.Minute

type SyncETA interface {
	// NotifyProgress is called when leafs are received to estimate progress and
	// updates the overall ETA if needed, logging a message if it has been more
	// than [updateInterval] since the last update.
	NotifyProgress(root common.Hash, startTime time.Time, startKey []byte, key []byte)

	// RemoveSyncedTrie is called when a trie is done syncing, so it will no longer
	// be considered for calculating the ETA.
	RemoveSyncedTrie(root common.Hash, skipped bool)
}

type syncETA struct {
	lock         sync.Mutex
	mainTrieRoot common.Hash
	etas         map[common.Hash]time.Duration
	lastUpdate   time.Time

	// counters to display progress
	triesSynced   uint32
	triesFromDisk uint32
}

func NewSyncEta(mainTrieRoot common.Hash) SyncETA {
	return &syncETA{
		mainTrieRoot: mainTrieRoot,
		etas:         make(map[common.Hash]time.Duration),
	}
}

func (s *syncETA) NotifyProgress(root common.Hash, startTime time.Time, startKey []byte, key []byte) {
	// use first 16 bits of [startKey] and [key] to estimate progress
	startPos := bytesToUint16(startKey)
	currentPos := bytesToUint16(key)
	if currentPos <= startPos {
		// have not made enough progress, avoid division by zero
		return
	}

	timeSpent := time.Since(startTime)
	estimatedTotalDuration := float64(timeSpent) * float64(math.MaxUint16-startPos) / float64(currentPos-startPos)

	s.lock.Lock()
	defer s.lock.Unlock()
	// track eta for all tries in progress so the next large trie can be identified
	// after the largest trie completes. this is done before the following early
	// return on [lastUpdate], so if [root] becomes the largest trie after the current
	// largest trie is removed, a more accurate ETA is logged.
	s.etas[root] = time.Duration(estimatedTotalDuration) - timeSpent

	if time.Since(s.lastUpdate) < updateInterval {
		return
	}
	s.lastUpdate = time.Now()

	var maxEta time.Duration
	for _, eta := range s.etas {
		if maxEta < eta {
			maxEta = eta
		}
	}
	log.Info(
		"state sync in progress",
		"ETA", roundETA(maxEta),
		"triesComplete", s.triesSynced+s.triesFromDisk, // intended as a monotonically increasing stat the user can monitor as ETA may fluctuate
	)
	log.Debug(
		"state sync progress details",
		"root", root,
		"trieETA", s.etas[root],
		"key", common.BytesToHash(key),
		"triesSynced", s.triesSynced,
		"triesFromDisk", s.triesFromDisk,
		"isMainTrie", root == s.mainTrieRoot,
	)
}

func (s *syncETA) RemoveSyncedTrie(root common.Hash, skipped bool) {
	s.lock.Lock()
	defer s.lock.Unlock()

	delete(s.etas, root)
	if skipped {
		s.triesFromDisk += 1
	} else {
		s.triesSynced += 1
	}
}

// bytesToUint16 interprets the first 2 bytes of [key] as a uint16
// and returns that value for use in progress calculations
func bytesToUint16(key []byte) uint16 {
	if len(key) < wrappers.ShortLen {
		return 0
	}
	return binary.BigEndian.Uint16(key)
}

// roundETA rounds [d] to a minute and chops off the "0s" suffix
func roundETA(d time.Duration) string {
	str := d.Round(time.Minute).String()
	return strings.TrimSuffix(str, "0s")
}
