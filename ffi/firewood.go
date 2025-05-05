// Package firewood provides a Go wrapper around the [Firewood] database.
//
// [Firewood]: https://github.com/ava-labs/firewood
package firewood

// // Note that -lm is required on Linux but not on Mac.
// #cgo LDFLAGS: -L${SRCDIR}/../target/release -L/usr/local/lib -lfirewood_ffi -lm
// #include <stdlib.h>
// #include "firewood.h"
import "C"

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"unsafe"
)

// These constants are used to identify errors returned by the Firewood Rust FFI.
// These must be changed if the Rust FFI changes - should be reported by tests.
const (
	rootHashNotFound = "IO error: Root hash not found"
	keyNotFound      = "key not found"
)

var dbClosedErr = errors.New("firewood database already closed")

// A Database is a handle to a Firewood database.
// It is not safe to call these methods with a nil handle.
type Database struct {
	// handle is returned and accepted by cgo functions. It MUST be treated as
	// an opaque value without special meaning.
	// https://en.wikipedia.org/wiki/Blinkenlights
	handle unsafe.Pointer
}

// Config configures the opening of a [Database].
type Config struct {
	Create            bool
	NodeCacheEntries  uint
	Revisions         uint
	ReadCacheStrategy CacheStrategy
	MetricsPort       uint16
}

// DefaultConfig returns a sensible default Config.
func DefaultConfig() *Config {
	return &Config{
		NodeCacheEntries:  1_000_000,
		Revisions:         100,
		ReadCacheStrategy: OnlyCacheWrites,
		MetricsPort:       3000,
	}
}

// A CacheStrategy represents the caching strategy used by a [Database].
type CacheStrategy uint8

const (
	OnlyCacheWrites CacheStrategy = iota
	CacheBranchReads
	CacheAllReads

	// invalidCacheStrategy MUST be the final value in the iota block to make it
	// the smallest value greater than all valid values.
	invalidCacheStrategy
)

// New opens or creates a new Firewood database with the given configuration. If
// a nil `Config` is provided [DefaultConfig] will be used instead.
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

	args := C.struct_CreateOrOpenArgs{
		path:         C.CString(filePath),
		cache_size:   C.size_t(conf.NodeCacheEntries),
		revisions:    C.size_t(conf.Revisions),
		strategy:     C.uint8_t(conf.ReadCacheStrategy),
		metrics_port: C.uint16_t(conf.MetricsPort),
	}

	var db unsafe.Pointer
	if conf.Create {
		db = C.fwd_create_db(args)
	} else {
		db = C.fwd_open_db(args)
	}

	// After creating the db, we can safely free the path string.
	C.free(unsafe.Pointer(args.path))
	return &Database{handle: db}, nil
}

// KeyValue is a key-value pair.
type KeyValue struct {
	Key   []byte
	Value []byte
}

// Batch applies a batch of updates to the database, returning the hash of the
// root node after the batch is applied.
//
// NOTE that if the `Value` is empty, the respective `Key` will be deleted as a
// prefix deletion; i.e. all children will be deleted.
//
// WARNING: a consequence of prefix deletion is that calling Batch with an empty
// key and value will delete the entire database.
func (db *Database) Batch(ops []KeyValue) ([]byte, error) {
	// TODO(arr4n) refactor this to require explicit signalling from the caller
	// that they want prefix deletion, similar to `rm --no-preserve-root`.

	values, cleanup := newValueFactory()
	defer cleanup()

	ffiOps := make([]C.struct_KeyValue, len(ops))
	for i, op := range ops {
		ffiOps[i] = C.struct_KeyValue{
			key:   values.from(op.Key),
			value: values.from(op.Value),
		}
	}

	hash := C.fwd_batch(
		db.handle,
		C.size_t(len(ffiOps)),
		(*C.struct_KeyValue)(unsafe.SliceData(ffiOps)), // implicitly pinned
	)
	return extractBytesThenFree(&hash)
}

// extractBytesThenFree converts the cgo `Value` payload to a byte slice, frees
// the `Value`, and returns the extracted slice.
// Generates error if the error term is nonnull.
func extractBytesThenFree(v *C.struct_Value) (buf []byte, err error) {
	buf = C.GoBytes(unsafe.Pointer(v.data), C.int(v.len))
	if v.len == 0 {
		errStr := C.GoString((*C.char)(unsafe.Pointer(v.data)))
		err = fmt.Errorf("firewood error: %s", errStr)
	}
	C.fwd_free_value(v)

	// Pin the returned value to prevent it from being garbage collected.
	runtime.KeepAlive(v)
	return
}

// Get retrieves the value for the given key. It always returns a nil error.
// If the key is not found, the return value will be (nil, nil).
func (db *Database) Get(key []byte) ([]byte, error) {
	if db.handle == nil {
		return nil, dbClosedErr
	}

	values, cleanup := newValueFactory()
	defer cleanup()
	val := C.fwd_get(db.handle, values.from(key))
	bytes, err := extractBytesThenFree(&val)

	// If the root hash or key is not found, return nil.
	if err != nil && (strings.Contains(err.Error(), rootHashNotFound) || strings.Contains(err.Error(), keyNotFound)) {
		return nil, nil
	}
	return bytes, err
}

// Root returns the current root hash of the trie.
// Empty trie must return common.Hash{}.
func (db *Database) Root() ([]byte, error) {
	if db.handle == nil {
		return nil, dbClosedErr
	}
	hash := C.fwd_root_hash(db.handle)
	bytes, err := extractBytesThenFree(&hash)

	// If the root hash is not found, return a zeroed slice.
	if err != nil && strings.Contains(err.Error(), rootHashNotFound) {
		bytes = make([]byte, 32)
		err = nil
	}
	return bytes, err
}

// Close closes the database and releases all held resources.
// Returns an error if already closed.
func (db *Database) Close() error {
	if db.handle == nil {
		return dbClosedErr
	}
	C.fwd_close_db(db.handle)
	db.handle = nil
	return nil
}

// newValueFactory returns a factory for converting byte slices into cgo `Value`
// structs that can be passed as arguments to cgo functions. The returned
// cleanup function MUST be called when the constructed values are no longer
// required, after which they can no longer be used as cgo arguments.
func newValueFactory() (*valueFactory, func()) {
	f := new(valueFactory)
	return f, func() { f.pin.Unpin() }
}

type valueFactory struct {
	pin runtime.Pinner
}

func (f *valueFactory) from(data []byte) C.struct_Value {
	if len(data) == 0 {
		return C.struct_Value{0, nil}
	}
	ptr := (*C.uchar)(unsafe.SliceData(data))
	f.pin.Pin(ptr)
	return C.struct_Value{C.size_t(len(data)), ptr}
}
