// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"crypto/rand"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/params"
	"github.com/stretchr/testify/require"
)

func TestVerifyCode(t *testing.T) {
	code := []byte("contract bytecode")
	hash := crypto.Keccak256Hash(code)

	oversized := make([]byte, params.MaxCodeSize+1)
	oversizedHash := crypto.Keccak256Hash(oversized)

	tests := []struct {
		name    string
		hashes  []common.Hash
		data    [][]byte
		wantErr error
	}{
		{
			name:   "valid",
			hashes: []common.Hash{hash},
			data:   [][]byte{code},
		},
		{
			name:    "count mismatch",
			hashes:  []common.Hash{hash},
			data:    [][]byte{},
			wantErr: errCodeCountMismatch,
		},
		{
			name:    "hash mismatch",
			hashes:  []common.Hash{hash},
			data:    [][]byte{[]byte("tampered")},
			wantErr: errCodeHashMismatch,
		},
		{
			name:    "size exceeded",
			hashes:  []common.Hash{oversizedHash},
			data:    [][]byte{oversized},
			wantErr: errCodeSizeExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyCode(tt.hashes, tt.data)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func writeCode(t *testing.T, db ethdb.KeyValueWriter, code []byte) common.Hash {
	t.Helper()
	hash := crypto.Keccak256Hash(code)
	rawdb.WriteCode(db, hash, code)
	return hash
}

func randomCode(t *testing.T) []byte {
	t.Helper()
	code := make([]byte, 128)
	_, err := rand.Read(code)
	require.NoError(t, err)
	return code
}
