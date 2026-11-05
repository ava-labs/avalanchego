// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package wal

import (
	"encoding/base64"
	"fmt"

	"github.com/ava-labs/simplex/common"
)

const (
	defaultMaxWALSize = 100 * 1024 * 1024 // 100 MB
)

// Creator creates a DeletableWAL. Returns an error upon failure.
type Creator func() (DeletableWAL, error)

// Reader reads the round of a WAL payload.
// Returns an error upon failure.
type Reader interface {
	Round([]byte) (uint64, error)
}

// DeletableWAL is a WAL that can be deleted.
type DeletableWAL interface {
	common.WriteAheadLog
	// Delete deletes the WAL file and after it is called,
	// it should no longer be used.
	Delete() error
}

type garbageCollectedWAL struct {
	round uint64
	size  int
	DeletableWAL
}

type GarbageCollectedWAL struct {
	wals       []garbageCollectedWAL
	createWAL  Creator
	maxWalSize int
	reader     Reader
}

// NewGarbageCollectedWAL creates a new GarbageCollectedWAL.
// It takes in a list of existing WALs to load, a Creator to create new WALs,
// a Reader to read the round of entries, and a maximum WAL size.
// A new WAL is created when the given maximum WAL size of a WAL is reached.
func NewGarbageCollectedWAL(WALs []DeletableWAL, creator Creator, reader Reader, maxWALSize int) (*GarbageCollectedWAL, error) {
	wals := make([]garbageCollectedWAL, 0, len(WALs))
	for _, wal := range WALs {
		wal, err := loadGarbageCollectedWAL(wal, reader)
		if err != nil {
			return nil, err
		}
		wals = append(wals, wal)
	}

	if maxWALSize == 0 {
		maxWALSize = defaultMaxWALSize
	}

	return &GarbageCollectedWAL{
		maxWalSize: maxWALSize,
		reader:     reader,
		createWAL:  creator,
		wals:       wals,
	}, nil
}

func loadGarbageCollectedWAL(wal DeletableWAL, reader Reader) (garbageCollectedWAL, error) {
	entries, err := wal.ReadAll()
	if err != nil {
		return garbageCollectedWAL{}, fmt.Errorf("error reading entries from WAL: %v", err)
	}

	var highestRound uint64
	var size int

	for _, entry := range entries {
		size += len(entry)
		round, err := reader.Round(entry)
		if err != nil {
			data := base64.StdEncoding.EncodeToString(entry)
			return garbageCollectedWAL{}, fmt.Errorf("error reading round of entry %s: %v", data, err)
		}
		if round > highestRound {
			highestRound = round
		}
	}
	return garbageCollectedWAL{
		round:        highestRound,
		DeletableWAL: wal,
		size:         size,
	}, nil
}

func (gcw *GarbageCollectedWAL) Append(payload []byte) error {
	round, err := gcw.reader.Round(payload)
	if err != nil {
		data := base64.StdEncoding.EncodeToString(payload)
		return fmt.Errorf("error reading round of entry %s: %v", data, err)
	}

	if gcw.shouldWeCreateNewWAL(payload) {
		// Close the last WAL in use because we won't be needing it anymore
		if len(gcw.wals) > 0 {
			if err := gcw.wals[len(gcw.wals)-1].Close(); err != nil {
				return fmt.Errorf("error closing last WAL: %v", err)
			}
		}
		newWAL, err := gcw.createWAL()
		if err != nil {
			return fmt.Errorf("error creating new WAL: %v", err)
		}
		gcw.wals = append(gcw.wals, garbageCollectedWAL{
			DeletableWAL: newWAL,
			round:        round,
			size:         len(payload),
		})
	} else {
		// We're still using the last WAL, so we need to update its size
		gcw.wals[len(gcw.wals)-1].size += len(payload)
	}

	last := len(gcw.wals) - 1
	if round > gcw.wals[last].round {
		gcw.wals[last].round = round
	}

	return gcw.wals[last].Append(payload)
}

func (gcw *GarbageCollectedWAL) shouldWeCreateNewWAL(payload []byte) bool {
	var shouldCreateNewWAL bool

	if len(gcw.wals) == 0 {
		shouldCreateNewWAL = true
	} else {
		lastWALSize := gcw.wals[len(gcw.wals)-1].size
		shouldCreateNewWAL = lastWALSize+len(payload) > gcw.maxWalSize
	}
	return shouldCreateNewWAL
}

func (gcw *GarbageCollectedWAL) ReadAll() ([][]byte, error) {
	var allEntries [][]byte

	for i, wal := range gcw.wals {
		entries, err := wal.ReadAll()
		if err != nil {
			return nil, fmt.Errorf("error reading entries from WAL %d: %v", i, err)
		}
		if err := wal.Close(); err != nil {
			return nil, fmt.Errorf("error closing WAL %d: %v", i, err)
		}

		allEntries = append(allEntries, entries...)
	}

	return allEntries, nil
}

func (gcw *GarbageCollectedWAL) GarbageCollect(round uint64) error {
	newWALs := make([]garbageCollectedWAL, 0, len(gcw.wals))

	for i, wal := range gcw.wals {
		if wal.round < round {
			// This WAL is below the round, we can delete it
			if err := wal.Delete(); err != nil {
				return fmt.Errorf("error deleting WAL %d: %v", i, err)
			}
			continue
		}
		// Else, this WAL is still needed
		newWALs = append(newWALs, wal)
	}

	gcw.wals = newWALs

	return nil
}

func (gcw *GarbageCollectedWAL) Close() error {
	for i, wal := range gcw.wals {
		if err := wal.Close(); err != nil {
			return fmt.Errorf("error closing WAL %d: %v", i, err)
		}
	}
	gcw.wals = nil
	return nil
}
