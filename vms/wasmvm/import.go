package wasmvm

// void print(void *context, int ptr, int len);
// int dbPut(void *context, int key, int keyLen, int value, int valueLen);
// int dbGet(void *context, int key, int keyLen, int value);
// int returnValue(void *context, int valuePtr, int valueLen);
// int dbGetValueLen(void *context, int keyPtr, int keyLen);
import "C"
import (
	"fmt"
	"unsafe"

	"github.com/ava-labs/gecko/ids"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/utils/math"

	"github.com/ava-labs/gecko/utils/logging"
	wasm "github.com/wasmerio/go-ext-wasm/wasmer"
)

type ctx struct {
	log    logging.Logger    // this chain's logger
	db     database.Database // DB for the contract to read/write
	memory *wasm.Memory      // the instance's memory
	txID   ids.ID            // ID of transaction that triggered current SC method invocation
}

// Print bytes in the smart contract's memory
// The bytes are interpreted as a string
//export print
func print(context unsafe.Pointer, ptr C.int, strLen C.int) {
	ctxRaw := wasm.IntoInstanceContext(context)
	ctx := ctxRaw.Data().(ctx)
	instanceMemory := ctx.memory.Data()
	finalIndex, err := math.Add32(uint32(ptr), uint32(strLen))
	if err != nil || int(finalIndex) > len(instanceMemory) {
		ctx.log.Error("Print from smart contract failed. Index out of bounds.")
		return
	}
	ctx.log.Info("Print from smart contract: %v", string(instanceMemory[ptr:finalIndex]))
}

// Put a KV pair where the key/value are defined by a pointer to the first byte
// and the length of the key/value.
// Returns 0 if successful, otherwise unsuccessful
//export dbPut
func dbPut(context unsafe.Pointer, keyPtr C.int, keyLen C.int, valuePtr C.int, valueLen C.int) C.int {
	// Get the context
	ctxRaw := wasm.IntoInstanceContext(context)
	ctx := ctxRaw.Data().(ctx)

	// Validate arguments
	if keyPtr < 0 || keyLen < 0 {
		ctx.log.Error("dbPut failed. Key pointer and length must be non-negative")
		return 1
	} else if valuePtr < 0 || valueLen < 0 {
		ctx.log.Error("dbPut failed. Value pointer and length must be non-negative")
		return 1
	}
	contractState := ctx.memory.Data()
	keyFinalIndex, err := math.Add32(uint32(keyPtr), uint32(keyLen))
	if err != nil || int(keyFinalIndex) > len(contractState) {
		ctx.log.Error("dbPut failed. Key index out of bounds.")
		return 1
	}
	valueFinalIndex, err := math.Add32(uint32(valuePtr), uint32(valueLen))
	if err != nil || int(valueFinalIndex) > len(contractState) {
		ctx.log.Error("dbPut failed. Value index out of bounds.")
		return 1
	}

	// Do the put
	key := contractState[keyPtr:keyFinalIndex]
	value := contractState[valuePtr:valueFinalIndex]
	ctx.log.Verbo("Putting K/V pair for contract.\n  key: %v\n  value: %v", key, value)
	if err := ctx.db.Put(key, value); err != nil {
		ctx.log.Error("dbPut failed: %s", err)
		return 1
	}
	return 0
}

// Get a value from the database. The key is in the contract's memory.
// It starts at [keyPtr] and is [keyLen] bytes long.
// The value is written to the contract's memory starting at [valuePtr]
// Returns the length of the returned value, or -1 if the get failed.
//export dbGet
func dbGet(context unsafe.Pointer, keyPtr C.int, keyLen C.int, valuePtr C.int) C.int {
	// Get the context
	ctxRaw := wasm.IntoInstanceContext(context)
	ctx := ctxRaw.Data().(ctx)

	// Validate arguments
	if keyPtr < 0 || keyLen < 0 {
		ctx.log.Error("dbGet failed. Key pointer and length must be non-negative")
		return -1
	} else if valuePtr < 0 {
		ctx.log.Error("dbGet failed. Value pointer must be non-negative")
		return -1
	}
	contractState := ctx.memory.Data()
	keyFinalIndex, err := math.Add32(uint32(keyPtr), uint32(keyLen))
	if err != nil || int(keyFinalIndex) > len(contractState) {
		ctx.log.Error("dbGet failed. Key index out of bounds")
		return -1
	}

	key := contractState[keyPtr:keyFinalIndex]
	value, err := ctx.db.Get(key)
	if err != nil {
		ctx.log.Error("dbGet failed: %s", err)
		return -1
	}
	ctx.log.Verbo("dbGet returning\n  key: %v\n value: %v\n", key, value)
	copy(contractState[valuePtr:], value)
	return C.int(len(value))
}

// Get the length in bytes of the value associated with a key in the database.
// The key is in the contract's memory at [keyPtr, keyLen]
// Returns -1 if the value corresponding to the key couldn't be found
//export dbGetValueLen
func dbGetValueLen(context unsafe.Pointer, keyPtr C.int, keyLen C.int) C.int {
	// Get the context
	ctxRaw := wasm.IntoInstanceContext(context)
	ctx := ctxRaw.Data().(ctx)

	// Validate arguments
	if keyPtr < 0 || keyLen < 0 {
		ctx.log.Error("dbGetValueLen failed. Key pointer and length must be non-negative")
		return -1
	}
	contractState := ctx.memory.Data()
	keyFinalIndex, err := math.Add32(uint32(keyPtr), uint32(keyLen))
	if err != nil || int(keyFinalIndex) > len(contractState) {
		ctx.log.Error("dbGetValueLen failed. Key index out of bounds")
		return -1
	}

	key := contractState[keyPtr:keyFinalIndex]
	value, err := ctx.db.Get(key)
	if err != nil {
		ctx.log.Error("dbGetValueLen failed: %s", err)
		return -1
	}
	ctx.log.Verbo("dbGetValueLen returning %v", len(value))
	return C.int(len(value))
}

// Smart contract methods call this method to return a value
// Creates a mapping:
//   Key: Tx ID of tx that invoked smart contract
//   Value: contract's memory in [ptr,ptr+len)
// Can only called at most once by a smart contract
// Returns 0 on success, other return value indicates failure
//export returnValue
func returnValue(context unsafe.Pointer, valuePtr C.int, valueLen C.int) C.int {
	// Get the context
	ctxRaw := wasm.IntoInstanceContext(context)
	ctx := ctxRaw.Data().(ctx)

	// Validate arguments
	if valuePtr < 0 || valueLen < 0 {
		ctx.log.Error("returnValue failed. Value pointer and length must be non-negative")
		return -1
	}
	contractState := ctx.memory.Data()
	finalIndex, err := math.Add32(uint32(valuePtr), uint32(valueLen))
	if err != nil || int(finalIndex) > len(contractState) {
		ctx.log.Error("returnValue failed. Index out of bounds")
		return -1
	}

	value := contractState[valuePtr:finalIndex] // TODO do something with the returned value
	ctx.log.Verbo("returned value: %v", value)
	return 0
}

// Return the imports (host methods) available to smart contracts
func standardImports() *wasm.Imports {
	imports, err := wasm.NewImportObject().Imports()
	if err != nil {
		panic(fmt.Sprintf("couldn't create wasm imports: %v", err))
	}
	imports, err = imports.AppendFunction("print", print, C.print)
	if err != nil {
		panic(fmt.Sprintf("couldn't add print import: %v", err))
	}
	imports, err = imports.AppendFunction("dbPut", dbPut, C.dbPut)
	if err != nil {
		panic(fmt.Sprintf("couldn't add dbPut import: %v", err))
	}
	imports, err = imports.AppendFunction("dbGet", dbGet, C.dbGet)
	if err != nil {
		panic(fmt.Sprintf("couldn't add dbGet import: %v", err))
	}
	imports, err = imports.AppendFunction("dbGetValueLen", dbGetValueLen, C.dbGetValueLen)
	if err != nil {
		panic(fmt.Sprintf("couldn't add dbGetValueLen import: %v", err))
	}
	return imports
}
