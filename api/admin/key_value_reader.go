// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package admin

import (
	"context"

	"github.com/ava-labs/avalanchego/database"
)

var _ database.KeyValueReader = (*KeyValueReader)(nil)

type KeyValueReader struct {
	client *Client
}

func NewKeyValueReader(client *Client) *KeyValueReader {
	return &KeyValueReader{
		client: client,
	}
}

func (r *KeyValueReader) Has(key []byte) (bool, error) {
	_, err := r.client.DBGet(context.Background(), key)
	if err == database.ErrNotFound {
		return false, nil
	}
	return err == nil, err
}

func (r *KeyValueReader) Get(key []byte) ([]byte, error) {
	return r.client.DBGet(context.Background(), key)
}
