// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package queue

import (
	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/utils/wrappers"
)

type state struct{ jobs *Jobs }

func (s *state) SetInt(db database.Database, key []byte, size uint32) error {
	p := wrappers.Packer{Bytes: make([]byte, wrappers.IntLen)}

	p.PackInt(size)

	return db.Put(key, p.Bytes)
}

func (s *state) Int(db database.Database, key []byte) (uint32, error) {
	value, err := db.Get(key)
	if err != nil {
		return 0, err
	}

	p := wrappers.Packer{Bytes: value}
	return p.UnpackInt(), p.Err
}

func (s *state) SetJob(db database.Database, key []byte, job Job) error {
	return db.Put(key, job.Bytes())
}

func (s *state) Job(db database.Database, key []byte) (Job, error) {
	value, err := db.Get(key)
	if err != nil {
		return nil, err
	}
	return s.jobs.parser.Parse(value)
}

func (s *state) SetIDs(db database.Database, key []byte, blocking ids.Set) error {
	p := wrappers.Packer{Bytes: make([]byte, wrappers.IntLen+hashing.HashLen*blocking.Len())}

	p.PackInt(uint32(blocking.Len()))
	for _, id := range blocking.List() {
		p.PackFixedBytes(id.Bytes())
	}

	return db.Put(key, p.Bytes)
}

func (s *state) IDs(db database.Database, key []byte) (ids.Set, error) {
	bytes, err := db.Get(key)
	if err != nil {
		return nil, err
	}

	p := wrappers.Packer{Bytes: bytes}

	blocking := ids.Set{}
	for i := p.UnpackInt(); i > 0 && !p.Errored(); i-- {
		id, _ := ids.ToID(p.UnpackFixedBytes(hashing.HashLen))
		blocking.Add(id)
	}

	return blocking, p.Err
}
