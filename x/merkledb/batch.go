// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"

	"github.com/ava-labs/avalanchego/database"
)

var _ database.Batch = (*batch)(nil)

type batch struct {
	database.BatchOps

	db *merkleDB
}

// Assumes [b.db.lock] isn't held.
func (b *batch) Write() error {
	return b.WriteContext(context.TODO())
}

// Assumes [b.db.lock] isn't held.
func (b *batch) WriteContext(ctx context.Context) error {
	return b.db.commitBatch(ctx, b.Ops)
}

func (b *batch) Inner() database.Batch {
	return b
}
