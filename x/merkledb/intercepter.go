// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
)

var (
	_ TrieView = (*trieViewVerifierIntercepter)(nil)
	_ TrieView = (*trieViewProverIntercepter)(nil)
)

type trieViewVerifierIntercepter struct {
	TrieView

	rootID ids.ID
	values map[path]Maybe[[]byte]
	nodes  map[path]Maybe[*Node]
}

func (i *trieViewVerifierIntercepter) GetMerkleRoot(context.Context) (ids.ID, error) {
	return i.rootID, nil
}

func (i *trieViewVerifierIntercepter) getValue(key path, lock bool) ([]byte, error) {
	value, ok := i.values[key]
	if !ok {
		return i.TrieView.getValue(key, lock)
	}
	if value.IsNothing() {
		return nil, database.ErrNotFound
	}
	return value.Value(), nil
}

func (i *trieViewVerifierIntercepter) getEditableNode(key path) (*Node, error) {
	n, ok := i.nodes[key]
	if !ok {
		return i.TrieView.getEditableNode(key)
	}
	if n.IsNothing() {
		return nil, database.ErrNotFound
	}
	return n.Value().clone(), nil
}

type trieViewProverIntercepter struct {
	TrieView

	lock   sync.Locker
	values map[path]*Proof
	nodes  map[path]*PathProof
}

func (i *trieViewProverIntercepter) getValue(key path, _ bool) ([]byte, error) {
	p, err := i.TrieView.GetProof(context.TODO(), key.Serialize().Value)
	if err != nil {
		return nil, err
	}

	i.lock.Lock()
	i.values[key] = p
	i.lock.Unlock()

	if p.Value.hasValue {
		return p.Value.value, nil
	}
	return nil, database.ErrNotFound
}

func (i *trieViewProverIntercepter) getEditableNode(key path) (*Node, error) {
	p, err := i.TrieView.GetPathProof(context.TODO(), key)
	if err != nil {
		return nil, err
	}

	i.lock.Lock()
	i.nodes[key] = p
	i.lock.Unlock()

	return i.TrieView.getEditableNode(key)
}
