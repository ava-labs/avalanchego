// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sharedconsensus

import (
	"time"

	"github.com/ava-labs/avalanchego/cache/timedentry"
	"github.com/ava-labs/avalanchego/ids"
)

type Entry struct {
	// When this entry was registered
	time.Time
}

// ProcessingEntries is a typed cache on top of TimedEntries cache
type ProcessingEntries struct {
	cache *timedentry.TimedEntries
}

// NewProcessingEntries returns a instanced cache
func NewProcessingEntries() *ProcessingEntries {
	return &ProcessingEntries{cache: &timedentry.TimedEntries{
		Size: 50,
	}}
}

// PutEntry formats the data into a Entry and inserts it in the cache
func (te *ProcessingEntries) PutEntry(key ids.ID, txRegisteredTime time.Time) {
	te.cache.Put(key, &Entry{
		Time: txRegisteredTime,
	})
}

// GetEntry returns an Entry from the cache
func (te *ProcessingEntries) GetEntry(key ids.ID) (*Entry, bool) {
	if val, ok := te.cache.Get(key); ok {
		return val.(*Entry), ok
	}
	return nil, false
}

// Evict removes the id from the cache
func (te *ProcessingEntries) Evict(key ids.ID) {
	te.cache.Evict(key)
}

// Flush implements the cache interface
func (te *ProcessingEntries) Flush() {
	te.cache.Flush()
}

// OldestRequest returns the oldest Entry in the cache
func (te *ProcessingEntries) OldestRequest() *Entry {
	if te.cache == nil {
		return nil
	}

	if val := te.cache.OldestRequest(); val != nil {
		return val.(*Entry)
	}

	return nil
}

// Len returns the number of elements in the cache
func (te *ProcessingEntries) Len() int {
	if te.cache == nil {
		return 0
	}
	return te.cache.Len()
}
