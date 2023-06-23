// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var (
	_ StatelessView = (*trieViewVerifierIntercepter)(nil)
	_ TrieView      = (*trieViewProverIntercepter)(nil)

	ErrMissingProof     = errors.New("missing proof value")
	ErrMissingPathProof = errors.New("missing path proof value")
)

type trieViewVerifierIntercepter struct {
	StatelessView

	log        logging.Logger
	rootID     ids.ID
	tempValues map[Path]Maybe[[]byte]
	permValues map[Path]Maybe[[]byte]
	tempNodes  map[Path]Maybe[*Node]
	permNodes  map[Path]Maybe[*Node]
}

func (i *trieViewVerifierIntercepter) GetMerkleRoot(context.Context) (ids.ID, error) {
	return i.rootID, nil
}

func (i *trieViewVerifierIntercepter) getValue(key Path, maxLookback int) ([]byte, error) {
	value, ok := i.tempValues[key]
	if ok {
		if value.IsNothing() {
			return nil, database.ErrNotFound
		}
		return value.Value(), nil
	}

	value, ok = i.permValues[key]
	if ok {
		if value.IsNothing() {
			return nil, database.ErrNotFound
		}
		return value.Value(), nil
	}

	if i.StatelessView == nil || maxLookback == 0 {
		i.log.Warn("missing proof",
			zap.Stringer("parentRootID", i.rootID),
			zap.String("key", string(key)),
			zap.Int("maxLookback", maxLookback),
			zap.Error(ErrMissingProof),
		)
		return nil, fmt.Errorf("%w: %q", ErrMissingProof, key)
	}
	i.log.Debug("looking into parent for proof",
		zap.Stringer("parentRootID", i.rootID),
		zap.String("key", string(key)),
		zap.Int("maxLookback", maxLookback),
		zap.Error(ErrMissingProof),
	)
	return i.StatelessView.getValue(key, maxLookback-1)
}

func (i *trieViewVerifierIntercepter) getEditableNode(key Path, maxLookback int) (*Node, error) {
	n, ok := i.tempNodes[key]
	if ok {
		if n.IsNothing() {
			return nil, database.ErrNotFound
		}
		return n.Value().clone(), nil
	}

	n, ok = i.permNodes[key]
	if ok {
		if n.IsNothing() {
			return nil, database.ErrNotFound
		}
		return n.Value().clone(), nil
	}

	if i.StatelessView == nil || maxLookback == 0 {
		i.log.Warn("missing proof",
			zap.Stringer("parentRootID", i.rootID),
			zap.String("key", string(key)),
			zap.Int("maxLookback", maxLookback),
			zap.Error(ErrMissingPathProof),
		)
		return nil, fmt.Errorf("%w: %q", ErrMissingPathProof, key)
	}
	i.log.Debug("looking into parent for proof",
		zap.Stringer("parentRootID", i.rootID),
		zap.String("key", string(key)),
		zap.Int("maxLookback", maxLookback),
		zap.Error(ErrMissingPathProof),
	)
	return i.StatelessView.getEditableNode(key, maxLookback-1)
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
