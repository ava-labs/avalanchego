// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewooddb

import (
	"github.com/ava-labs/avalanchego/merkledb/merkledbtest"
	"testing"
	"os"
	"github.com/stretchr/testify/require"
)

func Test(t *testing.T) {
	for name, fn := range merkledbtest.TestSeq2() {
		t.Run(name, func(t *testing.T) {
			tmpFile, err := os.CreateTemp("", "firewood-test-*.db")
			require.NoError(t, err)
			db, err := New(tmpFile.Name())
			require.NoError(t, err)

			fn(t, db)
		})
	}
}
