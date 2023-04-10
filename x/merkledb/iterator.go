// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import "github.com/ava-labs/avalanchego/database"

var _ database.Iterator = &iterator{}

type iterator struct {
	db       *Database
	nodeIter database.Iterator
	current  *node
	err      error
}

func (i *iterator) Error() error {
	if i.err != nil {
		return i.err
	}
	return i.nodeIter.Error()
}

func (i *iterator) Key() []byte {
	if i.current == nil {
		return nil
	}
	return i.current.key.Serialize().Value
}

func (i *iterator) Value() []byte {
	if i.current == nil {
		return nil
	}
	return i.current.value.value
}

func (i *iterator) Next() bool {
	i.current = nil
	if i.err != nil {
		return false
	}
	for i.nodeIter.Next() {
		i.db.metrics.IOKeyRead()
		n, err := parseNode(path(i.nodeIter.Key()), i.nodeIter.Value())
		if err != nil {
			i.err = err
			return false
		}
		if n.hasValue() {
			i.current = n
			return true
		}
	}
	if i.err == nil {
		i.err = i.nodeIter.Error()
	}
	return false
}

func (i *iterator) Release() {
	i.nodeIter.Release()
}
