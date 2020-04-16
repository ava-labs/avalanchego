package wasmvm

// void print(void *context, int ptr, int len);
// void dbPut(void *context, int key, int keyLen, int value, int valueLen);
// int dbGet(void *context, int key, int keyLen, int value);
import "C"
import (
	"fmt"
	"unsafe"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/utils/math"

	"github.com/ava-labs/gecko/utils/logging"
	wasm "github.com/wasmerio/go-ext-wasm/wasmer"
)

type ctx struct {
	log    logging.Logger    // this chain's logger
	db     database.Database // DB for the contract to read/write
	memory *wasm.Memory      // the instance's memory
}

// Print bytes in the smart contract's memory
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

//export dbPut
func dbPut(context unsafe.Pointer, keyPtr C.int, keyLen C.int, valuePtr C.int, valueLen C.int) {
	ctxRaw := wasm.IntoInstanceContext(context)
	ctx := ctxRaw.Data().(ctx)
	// Validate arguments
	if keyPtr < 0 || keyLen < 0 {
		ctx.log.Error("dbPut failed. Key pointer and length must be non-negative")
		return
	} else if valuePtr < 0 || valueLen < 0 {
		ctx.log.Error("dbPut failed. Value pointer and length must be non-negative")
		return
	}
	keyFinalIndex, err := math.Add32(uint32(keyPtr), uint32(keyLen))
	if err != nil {
		ctx.log.Error("dbPut failed. Key index out of bounds.")
		return
	}
	valueFinalIndex, err := math.Add32(uint32(valuePtr), uint32(valueLen))
	if err != nil {
		ctx.log.Error("dbPut failed. Value index out of bounds.")
		return
	}

	contractState := ctx.memory.Data()
	key := contractState[keyPtr:keyFinalIndex]
	value := contractState[valuePtr:valueFinalIndex]
	ctx.log.Verbo("Putting K/V pair for contract.\n  key: %v\n  value: %v", key, value)
	if err := ctx.db.Put(key, value); err != nil {
		ctx.log.Error("dbPut failed: %s")
	}
}

//export dbGet
func dbGet(context unsafe.Pointer, keyPtr C.int, keyLen C.int, valuePtr C.int) C.int {
	ctxRaw := wasm.IntoInstanceContext(context)
	ctx := ctxRaw.Data().(ctx)
	// Validate arguments
	if keyPtr < 0 || keyLen < 0 {
		ctx.log.Error("dbGet failed. Key pointer and length must be non-negative")
		return -1
	} else if valuePtr < 0 {
		ctx.log.Error("dbGet failed. Value pointermust be non-negative")
		return -1
	}
	keyFinalIndex, err := math.Add32(uint32(keyPtr), uint32(keyLen))
	if err != nil {
		ctx.log.Error("dbGet failed. Key index out of bounds.")
		return -1
	}

	contractState := ctx.memory.Data()
	key := contractState[keyPtr:keyFinalIndex]
	value, err := ctx.db.Get(key)
	if err != nil {
		panic(err)
	}
	ctx.log.Verbo("dbGet returning\n  key: %v\n value: %v\n", key, value)
	copy(contractState[valuePtr:], value)
	return C.int(len(value))
}

// Return the standard imports available by all smart contracts
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
	return imports
}
