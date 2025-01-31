package firewood

// #cgo LDFLAGS: -L../target/release -L/usr/local/lib -lfirewood_ffi
// #include "firewood.h"
// #include <stdlib.h>
import "C"
import (
	"runtime"
	"unsafe"
)

const (
	// The size of the node cache for firewood
	NodeCache = 1000000
	// The number of revisions to keep (must be >=2)
	Revisions = 100
)

type Firewood struct {
	Db *C.void
}

// CreateDatabase creates a new Firewood database at the given path.
// Returns a handle that can be used for subsequent database operations.
func CreateDatabase(path string) Firewood {
	db := C.fwd_create_db(C.CString(path), C.size_t(NodeCache), C.size_t(Revisions))
	ptr := (*C.void)(db)
	return Firewood{Db: ptr}
}

func OpenDatabase(path string) Firewood {
	db := C.fwd_open_db(C.CString(path), C.size_t(NodeCache), C.size_t(Revisions))
	ptr := (*C.void)(db)
	return Firewood{Db: ptr}
}

type KeyValue struct {
	Key   []byte
	Value []byte
}

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
	hash := C.fwd_batch(unsafe.Pointer(f.Db), C.size_t(len(ops)), ptr)
	hash_bytes := C.GoBytes(unsafe.Pointer(hash.data), C.int(hash.len))
	C.fwd_free_value(&hash)
	return hash_bytes
}

func (f *Firewood) Get(input_key []byte) ([]byte, error) {
	var pin runtime.Pinner
	defer pin.Unpin()
	ffi_key := make_value(&pin, input_key)

	value := C.fwd_get(unsafe.Pointer(f.Db), ffi_key)
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

func (f *Firewood) Root() []byte {
	hash := C.fwd_root_hash(unsafe.Pointer(f.Db))
	hash_bytes := C.GoBytes(unsafe.Pointer(hash.data), C.int(hash.len))
	C.fwd_free_value(&hash)
	return hash_bytes
}

func (f *Firewood) Close() error {
	C.fwd_close_db(unsafe.Pointer(f.Db))
	return nil
}
