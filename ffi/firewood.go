// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

// Package ffi provides a Go wrapper around the [Firewood] database.
//
// [Firewood]: https://github.com/ava-labs/firewood
package ffi

//go:generate go run generate_cgo.go

// // Note that -lm is required on Linux but not on Mac.
// // FIREWOOD_CGO_BEGIN_STATIC_LIBS
// // #cgo linux,amd64 LDFLAGS: -L${SRCDIR}/libs/x86_64-unknown-linux-gnu
// // #cgo linux,arm64 LDFLAGS: -L${SRCDIR}/libs/aarch64-unknown-linux-gnu
// // #cgo darwin,amd64 LDFLAGS: -L${SRCDIR}/libs/x86_64-apple-darwin
// // #cgo darwin,arm64 LDFLAGS: -L${SRCDIR}/libs/aarch64-apple-darwin
// // FIREWOOD_CGO_END_STATIC_LIBS
// // FIREWOOD_CGO_BEGIN_LOCAL_LIBS
// #cgo LDFLAGS: -L${SRCDIR}/../target/debug
// #cgo LDFLAGS: -L${SRCDIR}/../target/release
// #cgo LDFLAGS: -L${SRCDIR}/../target/maxperf
// // FIREWOOD_CGO_END_LOCAL_LIBS
// #cgo LDFLAGS: -lfirewood_ffi -lm
// #include <stdlib.h>
// #include "firewood.h"
import "C"

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
)

// RootLength is the hash length for all Firewood hashes.
const RootLength = C.sizeof_HashKey

// Hash is the type used for all firewood hashes.
type Hash [RootLength]byte

var (
	// EmptyRoot is the zero value for [Hash]
	EmptyRoot Hash
	// ErrActiveKeepAliveHandles is returned when attempting to close a database with unfreed memory.
	ErrActiveKeepAliveHandles = errors.New("cannot close database with active keep-alive handles")

	errDBClosed = errors.New("firewood database already closed")
)

// Database is an FFI wrapper for the Rust Firewood database.
// All functions rely on CGO to call into the underlying Rust implementation.
// Instances are created via [New] and must be closed with [Database.Close]
// when no longer needed.
//
// A Database can have outstanding [Revision] and [Proposal], which
// access the database's memory. These must be released before closing the
// database. See [Database.Close] for more details.
//
// Database supports two hashing modes: Firewood hashing and Ethereum-compatible
// hashing. Ethereum-compatible hashing is distributed, but you can use the more efficient
// Firewood hashing by compiling from source. See the Firewood repository for more details.
//
// For concurrent use cases, see each type and method's documentation for thread-safety.
type Database struct {
	// handle is returned and accepted by cgo functions. It MUST be treated as
	// an opaque value without special meaning.
	// https://en.wikipedia.org/wiki/Blinkenlights
	handle             *C.DatabaseHandle
	outstandingHandles sync.WaitGroup
}

// Config defines the configuration parameters used when opening a [Database].
type Config struct {
	// Truncate indicates whether to clear the database file if it already exists.
	Truncate bool
	// NodeCacheEntries is the number of entries in the cache.
	// Must be non-zero.
	NodeCacheEntries uint
	// FreeListCacheEntries is the number of entries in the freelist cache.
	// Must be non-zero.
	FreeListCacheEntries uint
	// Revisions is the maximum number of historical revisions to keep in memory.
	// If RootStoreDir is set, then any revisions removed from memory will still be kept on disk.
	// Otherwise, any revisions removed from memory will no longer be kept on disk.
	// Must be >= 2.
	Revisions uint
	// ReadCacheStrategy is the caching strategy used for the node cache.
	ReadCacheStrategy CacheStrategy
	// RootStoreDir defines a path to store all historical roots on disk.
	RootStoreDir string
}

// DefaultConfig returns a [*Config] with sensible defaults:
//   - NodeCacheEntries:     1_000_000
//   - FreeListCacheEntries: 40_000
//   - Revisions:            100
//   - ReadCacheStrategy:    OnlyCacheWrites
func DefaultConfig() *Config {
	return &Config{
		NodeCacheEntries:     1_000_000,
		FreeListCacheEntries: 40_000,
		Revisions:            100,
		ReadCacheStrategy:    OnlyCacheWrites,
	}
}

// A CacheStrategy represents the caching strategy used by a [Database].
type CacheStrategy uint8

const (
	// OnlyCacheWrites caches only writes.
	OnlyCacheWrites CacheStrategy = iota
	// CacheBranchReads caches intermediate reads and writes.
	CacheBranchReads
	// CacheAllReads caches all reads and writes.
	CacheAllReads

	// invalidCacheStrategy MUST be the final value in the iota block to make it
	// the smallest value greater than all valid values.
	invalidCacheStrategy
)

// New opens or creates a new Firewood database with the given configuration. If
// a nil config is provided, [DefaultConfig] will be used instead.
// The database file will be created at the provided file path if it does not
// already exist.
//
// It is the caller's responsibility to call [Database.Close] when the database
// is no longer needed. No other [Database] in this process should be opened with
// the same file path until the database is closed.
func New(filePath string, conf *Config) (*Database, error) {
	if conf == nil {
		conf = DefaultConfig()
	}
	if conf.ReadCacheStrategy >= invalidCacheStrategy {
		return nil, fmt.Errorf("invalid %T (%[1]d)", conf.ReadCacheStrategy)
	}
	if conf.Revisions < 2 {
		return nil, fmt.Errorf("%T.Revisions must be >= 2", conf)
	}
	if conf.NodeCacheEntries < 1 {
		return nil, fmt.Errorf("%T.NodeCacheEntries must be >= 1", conf)
	}
	if conf.FreeListCacheEntries < 1 {
		return nil, fmt.Errorf("%T.FreeListCacheEntries must be >= 1", conf)
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()

	args := C.struct_DatabaseHandleArgs{
		path:                 newBorrowedBytes([]byte(filePath), &pinner),
		cache_size:           C.size_t(conf.NodeCacheEntries),
		free_list_cache_size: C.size_t(conf.FreeListCacheEntries),
		revisions:            C.size_t(conf.Revisions),
		strategy:             C.uint8_t(conf.ReadCacheStrategy),
		truncate:             C.bool(conf.Truncate),
		root_store_path:      newBorrowedBytes([]byte(conf.RootStoreDir), &pinner),
	}

	return getDatabaseFromHandleResult(C.fwd_open_db(args))
}

// Update applies a batch of updates to the database, returning the hash of the
// root node after the batch is applied. This is equilalent to creating a proposal
// with [Database.Propose], then committing it with [Proposal.Commit].
//
// Value Semantics:
//   - nil value (vals[i] == nil): Performs a DeleteRange operation using the key as a prefix
//   - empty slice (vals[i] != nil && len(vals[i]) == 0): Inserts/updates the key with an empty value
//   - non-empty value: Inserts/updates the key with the provided value
//
// WARNING: Calling Update with an empty key and nil value will delete the entire database
// due to prefix deletion semantics.
//
// This function is not thread-safe with respect to other calls that reference the latest
// state of the database, nor any calls that mutate the state of the database.
func (db *Database) Update(keys, vals [][]byte) (Hash, error) {
	if db.handle == nil {
		return EmptyRoot, errDBClosed
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()

	kvp, err := newKeyValuePairs(keys, vals, &pinner)
	if err != nil {
		return EmptyRoot, err
	}

	return getHashKeyFromHashResult(C.fwd_batch(db.handle, kvp))
}

// Propose creates a new proposal with the given keys and values. The proposal
// is not committed until [Proposal.Commit] is called. See [Database.Close] regarding
// freeing proposals. All proposals should be freed before closing the database.
//
// Value Semantics:
//   - nil value (vals[i] == nil): Performs a DeleteRange operation using the key as a prefix
//   - empty slice (vals[i] != nil && len(vals[i]) == 0): Inserts/updates the key with an empty value
//   - non-empty value: Inserts/updates the key with the provided value
//
// This function is not thread-safe with respect to other calls that reference the latest
// state of the database, nor any calls that mutate the latest state of the database.
func (db *Database) Propose(keys, vals [][]byte) (*Proposal, error) {
	if db.handle == nil {
		return nil, errDBClosed
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()

	kvp, err := newKeyValuePairs(keys, vals, &pinner)
	if err != nil {
		return nil, err
	}
	return getProposalFromProposalResult(C.fwd_propose_on_db(db.handle, kvp), &db.outstandingHandles)
}

// Get retrieves the value for the given key from the most recent revision.
// If the key is not found, the return value will be nil.
//
// This function is thread-safe with all other read operations, but is not thread-safe
// with respect to other calls that mutate the latest state of the database.
func (db *Database) Get(key []byte) ([]byte, error) {
	if db.handle == nil {
		return nil, errDBClosed
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()

	val, err := getValueFromValueResult(C.fwd_get_latest(db.handle, newBorrowedBytes(key, &pinner)))
	// The revision won't be found if the database is empty.
	// This is valid, but should be treated as a non-existent key
	if errors.Is(err, errRevisionNotFound) {
		return nil, nil
	}

	return val, err
}

// GetFromRoot retrieves the value for the given key from a specific root hash.
// If the root is not found, it returns an error.
// If key is not found, it returns nil.
//
// GetFromRoot caches a handle to the revision associated with the provided root hash, allowing
// subsequent calls with the same root to be more efficient.
//
// This function is thread-safe with all other operations.
func (db *Database) GetFromRoot(root Hash, key []byte) ([]byte, error) {
	if db.handle == nil {
		return nil, errDBClosed
	}

	// If the root is empty, the database is empty.
	if root == EmptyRoot {
		return nil, nil
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()

	return getValueFromValueResult(C.fwd_get_from_root(
		db.handle,
		newCHashKey(root),
		newBorrowedBytes(key, &pinner),
	))
}

// Root returns the current root hash of the trie.
// With Firewood hashing, the empty trie must return [EmptyRoot].
//
// This function is thread-safe with all other operations, except those that mutate
// the latest state of the database.
func (db *Database) Root() (Hash, error) {
	if db.handle == nil {
		return EmptyRoot, errDBClosed
	}

	return getHashKeyFromHashResult(C.fwd_root_hash(db.handle))
}

// LatestRevision returns a [Revision] representing the latest state of the database.
// If the latest revision has root [EmptyRoot], it returns an error. The [Revision] must
// be dropped prior to closing the database.
//
// This function is thread-safe with all other operations, except those that mutate
// the latest state of the database.
func (db *Database) LatestRevision() (*Revision, error) {
	root, err := db.Root()
	if err != nil {
		return nil, err
	}
	if root == EmptyRoot {
		return nil, errRevisionNotFound
	}
	return db.Revision(root)
}

// Revision returns a historical revision of the database.
// If the provided root does not exist (or is the [EmptyRoot]), it returns an error.
// The [Revision] must be dropped prior to closing the database.
//
// This function is thread-safe with all other operations.
func (db *Database) Revision(root Hash) (*Revision, error) {
	rev, err := getRevisionFromResult(C.fwd_get_revision(
		db.handle,
		newCHashKey(root),
	), &db.outstandingHandles)
	if err != nil {
		return nil, err
	}

	return rev, nil
}

// Close releases the memory associated with the Database.
//
// This blocks until all outstanding keep-alive handles are disowned or the
// [context.Context] is cancelled. That is, until all Revisions and Proposals
// created from this Database are either unreachable or one of
// [Proposal.Commit], [Proposal.Drop], or [Revision.Drop] has been called on
// them. Unreachable objects will be automatically dropped before Close returns,
// unless an alternate GC finalizer is set on them.
//
// This is safe to call multiple times; subsequent calls after the first will do
// nothing. However, it is not safe to call this method concurrently from multiple
// goroutines.
func (db *Database) Close(ctx context.Context) error {
	if db.handle == nil {
		return nil
	}

	go runtime.GC()

	done := make(chan struct{})
	go func() {
		db.outstandingHandles.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		return fmt.Errorf("%w: %w", ctx.Err(), ErrActiveKeepAliveHandles)
	}

	if err := getErrorFromVoidResult(C.fwd_close_db(db.handle)); err != nil {
		return fmt.Errorf("unexpected error when closing database: %w", err)
	}

	db.handle = nil // Prevent double free

	return nil
}
