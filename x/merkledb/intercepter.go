// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
)

var (
	_ StatelessView = (*trieViewVerifierIntercepter)(nil)
	_ TrieView      = (*trieViewProverIntercepter)(nil)

	ErrMissingProof     = errors.New("missing proof value")
	ErrMissingPathProof = errors.New("missing path proof value")
)

type trieViewVerifierIntercepter struct {
	StatelessView

	rootID ids.ID
	values map[Path]Maybe[[]byte]
	nodes  map[Path]Maybe[*Node]
}

func (i *trieViewVerifierIntercepter) GetMerkleRoot(context.Context) (ids.ID, error) {
	return i.rootID, nil
}

func (i *trieViewVerifierIntercepter) getValue(key Path, lock bool) ([]byte, error) {
	value, ok := i.values[key]
	if !ok {
		if i.StatelessView == nil {
			return nil, fmt.Errorf("%w: %q", ErrMissingProof, key)
		}
		return i.StatelessView.getValue(key, lock)
	}
	if value.IsNothing() {
		return nil, database.ErrNotFound
	}
	return value.Value(), nil
}

func (i *trieViewVerifierIntercepter) getEditableNode(key Path) (*Node, error) {
	n, ok := i.nodes[key]
	if !ok {
		if i.StatelessView == nil {
			return nil, fmt.Errorf("%w: %q", ErrMissingPathProof, key)
		}
		return i.StatelessView.getEditableNode(key)
	}
	if n.IsNothing() {
		return nil, database.ErrNotFound
	}
	return n.Value().clone(), nil
}

type trieViewProverIntercepter struct {
	TrieView

	lock   sync.Locker
	values map[Path]*Proof
	nodes  map[Path]*PathProof
}

func (i *trieViewProverIntercepter) getValue(key Path, _ bool) ([]byte, error) {
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

func (i *trieViewProverIntercepter) getEditableNode(key Path) (*Node, error) {
	p, err := i.TrieView.GetPathProof(context.TODO(), key)
	if err != nil {
		return nil, err
	}

	i.lock.Lock()
	i.nodes[key] = p
	i.lock.Unlock()

	return i.TrieView.getEditableNode(key)
}
