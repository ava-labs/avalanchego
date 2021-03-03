// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"time"

	"github.com/ava-labs/avalanchego/cache/timedentry"

	"github.com/ava-labs/avalanchego/utils/constants"

	"github.com/ava-labs/avalanchego/ids"
)

type transaction struct {
	// When this request was registered
	time.Time
	// The type of request that was made
	constants.MsgType
}

// ProcessingTxs is a typed cache on top of TimedEntries cache
type ProcessingTxs struct {
	cache *timedentry.TimedEntries
}

// NewProcessingTxs returns a instanced cache
func NewProcessingTxs() *ProcessingTxs {
	return &ProcessingTxs{cache: &timedentry.TimedEntries{
		Size: 50,
	}}
}

// PutRequest formats the data into a Request and inserts it in the cache
func (te *ProcessingTxs) PutTx(key ids.ID, txRegisteredTime time.Time) {
	te.cache.Put(key, &transaction{
		Time: txRegisteredTime,
	})
}

// GetTx returns a transaction from the cache
func (te *ProcessingTxs) GetTx(key ids.ID) (*transaction, bool) {
	if val, ok := te.cache.Get(key); ok {
		return val.(*transaction), ok
	}
	return nil, false
}

// Evict removes the id from the cache
func (te *ProcessingTxs) Evict(key ids.ID) {
	te.cache.Evict(key)
}

// Flush implements the cache interface
func (te *ProcessingTxs) Flush() {
	te.cache.Flush()
}

// OldestRequest returns the oldest element in the cache
func (te *ProcessingTxs) OldestRequest() *transaction {
	if te.cache == nil {
		return nil
	}

	if val := te.cache.OldestRequest(); val != nil {
		return val.(*transaction)
	}

	return nil
}

// Len returns the number of elements in the cache
func (te *ProcessingTxs) Len() int {
	if te.cache == nil {
		return 0
	}
	return te.cache.Len()
}
