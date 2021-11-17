// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mockdb

import (
	"bytes"
	"errors"
	"testing"
)

// Assert that when no members are assigned values, every method returns nil/error
func TestDefaultError(t *testing.T) {
	db := New()

	if err := db.Close(); err == nil {
		t.Fatal("should have errored")
	}
	if _, err := db.Has([]byte{}); err == nil {
		t.Fatal("should have errored")
	}
	if _, err := db.Get([]byte{}); err == nil {
		t.Fatal("should have errored")
	}
	if err := db.Put([]byte{}, []byte{}); err == nil {
		t.Fatal("should have errored")
	}
	if err := db.Delete([]byte{}); err == nil {
		t.Fatal("should have errored")
	}
	if batch := db.NewBatch(); batch != nil {
		t.Fatal("should have been nil")
	}
	if iterator := db.NewIterator(); iterator != nil {
		t.Fatal("should have errored")
	}
	if iterator := db.NewIteratorWithPrefix([]byte{}); iterator != nil {
		t.Fatal("should have errored")
	}
	if iterator := db.NewIteratorWithStart([]byte{}); iterator != nil {
		t.Fatal("should have errored")
	}
	if iterator := db.NewIteratorWithStartAndPrefix([]byte{}, []byte{}); iterator != nil {
		t.Fatal("should have errored")
	}
	if err := db.Compact([]byte{}, []byte{}); err == nil {
		t.Fatal("should have errored")
	}
	if _, err := db.Stat(""); err == nil {
		t.Fatal("should have errored")
	}
}

// Assert that mocking works for Get
func TestGet(t *testing.T) {
	db := New()

	// Mock Has()
	db.OnHas = func(b []byte) (bool, error) {
		if bytes.Equal(b, []byte{1, 2, 3}) {
			return true, nil
		}
		return false, errors.New("")
	}

	if has, err := db.Has([]byte{1, 2, 3}); err != nil {
		t.Fatal("should not have errored")
	} else if has != true {
		t.Fatal("has should be true")
	}

	if _, err := db.Has([]byte{1, 2}); err == nil {
		t.Fatal("should have have errored")
	}
}
