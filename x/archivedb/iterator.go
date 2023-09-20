// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package archivedb

type iterator struct {
	db        *archiveDB
	start     []byte
	end       []byte
	height    uint64
	keyLength uint64
}

func placeHolderIterator(db *archiveDB, height, keyLength uint64, start, end []byte) *iterator {
	return &iterator{
		db:        db,
		height:    height,
		keyLength: keyLength,
		start:     start,
		end:       end,
	}
}

func (iterator *iterator) Error() error {
	return ErrNotImplemented
}

func (iteartor *iterator) Key() []byte {
	return nil
}

func (iteartor *iterator) Value() []byte {
	return nil
}

func (iteartor *iterator) Next() bool {
	return false
}

func (iteartor *iterator) Release() {
}
