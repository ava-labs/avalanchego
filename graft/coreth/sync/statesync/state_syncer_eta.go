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

const updateInterval = 1 * time.Minute

type syncETA struct {
	lock            sync.Mutex
	largestTrieEta  time.Duration
	largestTrieRoot common.Hash
	mainTrieRoot    common.Hash

	lastUpdate time.Time

	// counters to display progress
	triesSynced   uint32
	triesFromDisk uint32
}

// notifyProgress is called when leafs are received to estimate progress and
// updates the overall ETA if needed.
func (s *syncETA) notifyProgress(root common.Hash, startTime time.Time, startKey []byte, key []byte) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if time.Since(s.lastUpdate) < updateInterval {
		return
	}

	// use first 16 bits of [startKey] and [key] to estimate progress
	startPos := bytesToUint16(startKey)
	currentPos := bytesToUint16(key)
	if currentPos <= startPos {
		// have not made enough progress, avoid division by zero
		return
	}

	timeSpent := time.Since(startTime)
	estimatedTotalDuration := float64(timeSpent) * float64(math.MaxUint16-startPos) / float64(currentPos-startPos)
	eta := time.Duration(estimatedTotalDuration) - timeSpent

	if eta > s.largestTrieEta || s.largestTrieRoot == root || s.largestTrieRoot == (common.Hash{}) {
		s.largestTrieEta = eta
		s.largestTrieRoot = root

		s.lastUpdate = time.Now()
		log.Info(
			"state sync in progress",
			"eta", roundETA(eta),
			"triesComplete", s.triesSynced+s.triesFromDisk, // intended as a monotonically increasing stat the user can monitor as ETA may fluctuate
		)
		log.Debug(
			"state sync progress details",
			"root", root,
			"key", common.BytesToHash(key),
			"triesSynced", s.triesSynced,
			"triesFromDisk", s.triesFromDisk,
			"mainTrie", root == s.mainTrieRoot,
		)
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

func (s *syncETA) notifyTrieSynced(root common.Hash, skipped bool) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if root == s.largestTrieRoot {
		s.largestTrieRoot = common.Hash{}
	}

	if skipped {
		s.triesFromDisk += 1
	} else {
		s.triesSynced += 1
	}
}

// roundETA rounds [d] to a minute and chops off the "0s" suffix
func roundETA(d time.Duration) string {
	str := d.Round(time.Minute).String()
	return strings.TrimSuffix(str, "0s")
}
