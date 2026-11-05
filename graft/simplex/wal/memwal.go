// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package wal

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

type InMemWAL struct {
	bb bytes.Buffer
	t  testing.TB
}

func (wal *InMemWAL) Close() error {
	return nil
}

func NewMemWAL(t testing.TB) *InMemWAL {
	return &InMemWAL{
		t: t,
	}
}

func (wal *InMemWAL) Append(b []byte) error {
	w := &wal.bb
	err := writeRecord(w, b)
	require.NoError(wal.t, err)
	return err
}

func (wal *InMemWAL) ReadAll() ([][]byte, error) {
	r := bytes.NewBuffer(wal.bb.Bytes())
	var res [][]byte
	for r.Len() > 0 {
		payload, _, err := readRecord(r, int64(r.Len()))
		if err != nil {
			return nil, fmt.Errorf("failed reading in-memory record: %w", err)
		}
		res = append(res, payload)
	}
	return res, nil
}
