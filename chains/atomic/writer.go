// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"github.com/ava-labs/avalanchego/database"
)

// WriteAll assumes all batches have the same underlying database. Batches
// should not be modified after being passed to this function.
func WriteAll(baseBatch database.Batch, batches ...database.Batch) error {
	baseBatch = baseBatch.Inner()
	for _, batch := range batches {
		batch = batch.Inner()
		if err := batch.Replay(baseBatch); err != nil {
			return err
		}
	}
	return baseBatch.Write()
}
