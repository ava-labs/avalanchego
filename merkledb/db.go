// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"iter"
	"errors"
)

var ErrNotFound = errors.New("not found")

type Changes struct {
	Keys [][]byte
	Vals [][]byte
}

func (c *Changes) Append(key []byte, val []byte) {
	c.Keys = append(c.Keys, key)
	c.Vals = append(c.Vals, val)
}

func (c *Changes) Seq2() iter.Seq2[[]byte, []byte] {
	return func(yield func([]byte, []byte) bool) {
		for i, k := range c.Keys {
			if !yield(k, c.Vals[i]) {
				return
			}
		}
	}
}

type Proposal interface {
	Propose(cs Changes) (Proposal, error)
	Get(key []byte) ([]byte, error)
	Commit() error
}

type Database interface {
	Proposal
	Close() error
}
