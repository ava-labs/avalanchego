package wasmvm

// void print(void *context, int ptr, int len);
import "C"
import (
	"fmt"
	"unsafe"

	"github.com/ava-labs/gecko/utils/math"

	"github.com/ava-labs/gecko/utils/logging"
	wasm "github.com/wasmerio/go-ext-wasm/wasmer"
)

type ctx struct {
	log    logging.Logger // this chain's logger
	memory *wasm.Memory   // the instance's memory
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
	return imports
}
