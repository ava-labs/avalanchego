// (c) 2019-2021, Ava Labs, Inte. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"time"

	"github.com/ava-labs/avalanchego/cache/timedentry"

	"github.com/ava-labs/avalanchego/utils/constants"

	"github.com/ava-labs/avalanchego/ids"
)

type RequestEntry struct {
	// When this request was registered
	time.Time
	// The type of request that was made
	constants.MsgType
}

// ProcessingRequests is a typed cache on top of TimedEntries cache
type ProcessingRequests struct {
	cache *timedentry.TimedEntries
}

// NewTimedRequests returns a instanced cache
func NewTimedRequests() *ProcessingRequests {
	return &ProcessingRequests{cache: &timedentry.TimedEntries{
		Size: 50,
	}}
}

// PutRequest formats the data into a Request and inserts it in the cache
func (te *ProcessingRequests) PutRequest(key ids.ID, reqRegisteredTime time.Time, msgType constants.MsgType) {
	te.cache.Put(key, &RequestEntry{
		Time:    reqRegisteredTime,
		MsgType: msgType,
	})
}

// GetRequest returns a request from the cache
func (te *ProcessingRequests) GetRequest(key ids.ID) (*RequestEntry, bool) {
	if val, ok := te.cache.Get(key); ok {
		return val.(*RequestEntry), ok
	}
	return nil, false
}

// Evict removes the id from the cache
func (te *ProcessingRequests) Evict(key ids.ID) {
	te.cache.Evict(key)
}

// Flush implements the cache interface
func (te *ProcessingRequests) Flush() {
	te.cache.Flush()
}

// OldestRequest returns the oldest element in the cache
func (te *ProcessingRequests) OldestRequest() *RequestEntry {
	if te.cache == nil {
		return nil
	}

	if val := te.cache.OldestRequest(); val != nil {
		return val.(*RequestEntry)
	}

	return nil
}

// Len returns the number of elements in the cache
func (te *ProcessingRequests) Len() int {
	if te.cache == nil {
		return 0
	}
	return te.cache.Len()
}
