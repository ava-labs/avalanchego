// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewooddb

import (
	"github.com/ava-labs/avalanchego/merkledb/merkledbtest"
	"testing"
	"os"
	"github.com/stretchr/testify/require"
)

func TestInterface(t *testing.T) {
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

//// Test superset of prefixes
//func TestPrefixes(t *testing.T) {
//	tests := []struct {
//		name     string
//		changes  merkledb.Changes
//		wantKeys [][]byte
//		wantVals [][]byte
//	}{
//		{
//			name: "unique prefixes unique keys",
//			changes: merkledb.Changes{
//				Keys: [][]byte{
//					Prefix([]byte("foo"), []byte("key")),
//					Prefix([]byte("bar"), []byte("key")),
//				},
//				Vals: [][]byte{
//					[]byte("val-1"),
//					[]byte("val-2"),
//				},
//			},
//			wantKeys: [][]byte{
//				Prefix([]byte("foo"), []byte("key")),
//				Prefix([]byte("bar"), []byte("key")),
//			},
//			wantVals: [][]byte{
//				[]byte("val-1"),
//				[]byte("val-2"),
//			},
//		},
//		{
//			name: "key collides with prefix",
//			changes: merkledb.Changes{
//				Keys: [][]byte{
//					Prefix([]byte("foo"), []byte("key")),
//					[]byte("fookey"),
//				},
//				Vals: [][]byte{
//					[]byte("val-1"),
//					[]byte("val-2"),
//				},
//			},
//			wantKeys: [][]byte{
//				Prefix([]byte("foo"), []byte("key")),
//				[]byte("fookey"),
//			},
//			wantVals: [][]byte{
//				[]byte("val-1"),
//				[]byte("val-2"),
//			},
//		},
//	}
//
//	for _, tt := range tests {
//		t.Run(tt.name, func(t *testing.T) {
//			require := require.New(t)
//
//			tmpFile, err := os.CreateTemp("", "firewood-test-*.db")
//			require.NoError(err)
//			db, err := New(tmpFile.Name())
//			require.NoError(err)
//
//			proposal, err := db.Propose(tt.changes)
//			require.NoError(err)
//			require.NoError(proposal.Commit())
//
//			for i, k := range tt.wantKeys {
//				got, err := db.Get(k)
//				require.NoError(err)
//				require.Equal(got, tt.wantVals[i])
//			}
//		})
//	}
//}
