package firewood

// #cgo LDFLAGS: -L../target/release -L/usr/local/lib -lfirewood_ffi
// #include "firewood.h"
// #include <stdlib.h>
import "C"
import (
	"runtime"
	"strconv"
	"unsafe"
)

// Firewood is a handle to a Firewood database
type Firewood struct {
	db *C.void
}

// openConfig is used to configure the database at open time
type openConfig struct {
	path              string
	nodeCacheEntries  uintptr
	revisions         uintptr
	readCacheStrategy int8
	create            bool
}

// OpenOption is a function that configures the database at open time
type OpenOption func(*openConfig)

// WithPath sets the path for the database
func WithPath(path string) OpenOption {
	return func(o *openConfig) {
		o.path = path
	}
}

// WithNodeCacheEntries sets the number of node cache entries
func WithNodeCacheEntries(entries uintptr) OpenOption {
	if entries < 1 {
		panic("Node cache entries must be >= 1")
	}
	return func(o *openConfig) {
		o.nodeCacheEntries = entries
	}
}

// WithRevisions sets the number of revisions to keep
func WithRevisions(revisions uintptr) OpenOption {
	if revisions < 2 {
		panic("Revisions must be >= 2")
	}
	return func(o *openConfig) {
		o.revisions = revisions
	}
}

// WithReadCacheStrategy sets the read cache strategy
// 0: Only writes are cached
// 1: Branch reads are cached
// 2: All reads are cached
func WithReadCacheStrategy(strategy int8) OpenOption {
	if (strategy < 0) || (strategy > 2) {
		panic("Invalid read cache strategy " + strconv.Itoa(int(strategy)))
	}
	return func(o *openConfig) {
		o.readCacheStrategy = strategy
	}
}

// WithCreate sets whether to create a new database
// If false, the database will be opened
// If true, the database will be created
func WithCreate(create bool) OpenOption {
	return func(o *openConfig) {
		o.create = create
	}
}

// NewDatabase opens or creates a new Firewood database with the given options.
// Returns a handle that can be used for subsequent database operations.
func NewDatabase(options ...OpenOption) Firewood {
	opts := &openConfig{
		nodeCacheEntries:  1_000_000,
		revisions:         100,
		readCacheStrategy: 0,
		path:              "firewood.db",
	}

	for _, opt := range options {
		opt(opts)
	}
	var db unsafe.Pointer
	if opts.create {
		db = C.fwd_create_db(C.CString(opts.path), C.size_t(opts.nodeCacheEntries), C.size_t(opts.revisions), C.uint8_t(opts.readCacheStrategy))
	} else {
		db = C.fwd_open_db(C.CString(opts.path), C.size_t(opts.nodeCacheEntries), C.size_t(opts.revisions), C.uint8_t(opts.readCacheStrategy))
	}

	ptr := (*C.void)(db)
	return Firewood{db: ptr}
}

// KeyValue is a key-value pair
type KeyValue struct {
	Key   []byte
	Value []byte
}

// Apply a batch of updates to the database
// Returns the hash of the root node after the batch is applied
// Note that if the Value is empty, the key will be deleted as a prefix
// delete (that is, all children will be deleted)
// WARNING: Calling it with an empty key and value will delete the entire database

func (f *Firewood) Batch(ops []KeyValue) []byte {
	var pin runtime.Pinner
	defer pin.Unpin()

	ffi_ops := make([]C.struct_KeyValue, len(ops))
	for i, op := range ops {
		ffi_ops[i] = C.struct_KeyValue{
			key:   make_value(&pin, op.Key),
			value: make_value(&pin, op.Value),
		}
	}
	ptr := (*C.struct_KeyValue)(unsafe.Pointer(&ffi_ops[0]))
	hash := C.fwd_batch(unsafe.Pointer(f.db), C.size_t(len(ops)), ptr)
	hash_bytes := C.GoBytes(unsafe.Pointer(hash.data), C.int(hash.len))
	C.fwd_free_value(&hash)
	return hash_bytes
}

// Get retrieves the value for the given key.
func (f *Firewood) Get(input_key []byte) ([]byte, error) {
	var pin runtime.Pinner
	defer pin.Unpin()
	ffi_key := make_value(&pin, input_key)

	value := C.fwd_get(unsafe.Pointer(f.db), ffi_key)
	ffi_bytes := C.GoBytes(unsafe.Pointer(value.data), C.int(value.len))
	C.fwd_free_value(&value)
	if len(ffi_bytes) == 0 {
		return nil, nil
	}
	return ffi_bytes, nil
}

func make_value(pin *runtime.Pinner, data []byte) C.struct_Value {
	if len(data) == 0 {
		return C.struct_Value{0, nil}
	}
	ptr := (*C.uchar)(unsafe.Pointer(&data[0]))
	pin.Pin(ptr)
	return C.struct_Value{C.size_t(len(data)), ptr}
}

// Root returns the current root hash of the trie.
func (f *Firewood) Root() []byte {
	hash := C.fwd_root_hash(unsafe.Pointer(f.db))
	hash_bytes := C.GoBytes(unsafe.Pointer(hash.data), C.int(hash.len))
	C.fwd_free_value(&hash)
	return hash_bytes
}

// Close closes the database and releases all held resources.
func (f *Firewood) Close() error {
	C.fwd_close_db(unsafe.Pointer(f.db))
	return nil
}
