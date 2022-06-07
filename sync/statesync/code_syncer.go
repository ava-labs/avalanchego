// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"fmt"

	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/ethdb"
	statesyncclient "github.com/ava-labs/coreth/sync/client"
	"github.com/ethereum/go-ethereum/common"
)

// codeSyncer creates a separate goroutine with a ctx passed in to fulfill all get code requests.
// Currently, all requests are actually fulfilled synchronously, so that the ctx can be passed in
// to the client.
type codeSyncer struct {
	db     ethdb.KeyValueStore
	client statesyncclient.Client

	codeHashes  chan common.Hash
	codeResults chan codeResult
}

// codeResult defines the expected result from a fulfilled or failed code syncer request
type codeResult struct {
	codeBytes []byte
	err       error
}

func newCodeSyncer(db ethdb.KeyValueStore, client statesyncclient.Client) *codeSyncer {
	return &codeSyncer{
		db:          db,
		client:      client,
		codeHashes:  make(chan common.Hash, 1),
		codeResults: make(chan codeResult, 1),
	}
}

// start a goroutine with ctx for all of the requests to fetch code in a separate goroutine.
func (c *codeSyncer) start(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				c.codeResults <- codeResult{
					err: ctx.Err(),
				}
				return
			case codeHash, ok := <-c.codeHashes:
				if !ok {
					return
				}
				codeByteSlices, err := c.client.GetCode(ctx, []common.Hash{codeHash})
				if err != nil {
					c.codeResults <- codeResult{
						err: err,
					}
					return
				}
				// Note: GetCode returns an error if codeBytes length is not 1, so referencing codeBytes[0] is safe.
				c.codeResults <- codeResult{
					codeBytes: codeByteSlices[0],
				}
			}
		}
	}()
}

// addCode fetches the code for [codeHash] by adding it the goroutine to be retrieved and synchronously
// writing it to the database once the request is fulfilled.
// addCode should not be used concurrently (only called from main trie on leafs callback).
func (c *codeSyncer) addCode(codeHash common.Hash) error {
	c.codeHashes <- codeHash
	codeResult := <-c.codeResults

	// Note: this is considered a fatal error.
	if codeResult.err != nil {
		return fmt.Errorf("error fetching code bytes for code hash [%s] from network: %w", codeHash, codeResult.err)
	}
	rawdb.WriteCode(c.db, codeHash, codeResult.codeBytes)
	return nil
}
