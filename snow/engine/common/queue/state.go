// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package queue

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type state struct {
	// parser is able to parse a job from bytes.
	parser Parser
}

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

func (s *state) SetJobID(db database.Database, key []byte, jobID ids.ID) error {
	return db.Put(key, jobID[:])
}

func (s *state) JobID(db database.Database, key []byte) (ids.ID, error) {
	jobIDBytes, err := db.Get(key)
	if err != nil {
		return ids.ID{}, err
	}

	return ids.ToID(jobIDBytes)
}

func (s *state) SetJob(db database.Database, key []byte, job Job) error {
	return db.Put(key, job.Bytes())
}

func (s *state) Job(db database.Database, key []byte) (Job, error) {
	value, err := db.Get(key)
	if err != nil {
		return nil, err
	}
	return s.parser.Parse(value)
}

// IDs returns a slice of IDs from storage
func (s *state) IDs(db database.Database, prefix []byte) ([]ids.ID, error) {
	idSlice := []ids.ID(nil)
	iter := prefixdb.NewNested(prefix, db).NewIterator()
	defer iter.Release()

	for iter.Next() {
		keyID, err := ids.ToID(iter.Key())
		if err != nil {
			return nil, err
		}

		idSlice = append(idSlice, keyID)
	}
	return idSlice, nil
}

// AddID saves an ID to the prefixed database
func (s *state) AddID(db database.Database, prefix []byte, key ids.ID) error {
	pdb := prefixdb.NewNested(prefix, db)
	return pdb.Put(key[:], nil)
}

// RemoveID removes an ID from the prefixed database
func (s *state) RemoveID(db database.Database, prefix []byte, key ids.ID) error {
	pdb := prefixdb.NewNested(prefix, db)
	return pdb.Delete(key[:])
}
