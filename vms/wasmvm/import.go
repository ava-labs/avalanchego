package wasmvm

// void print(void *context, int ptr, int len);
import "C"
import (
	"fmt"
	"unsafe"

	wasm "github.com/wasmerio/go-ext-wasm/wasmer"
)

type ctx struct {
	memory *wasm.Memory // the instance's memory
}

//export print
func print(context unsafe.Pointer, ptr C.int, len C.int) {
	ctxRaw := wasm.IntoInstanceContext(context)
	ctx := ctxRaw.Data().(ctx)
	instanceMemoryData := ctx.memory.Data()
	fmt.Printf("Print from smart contract: %v\n", string(instanceMemoryData[ptr:ptr+len]))
}

// Return the standard imports needed by all smart contracts
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
