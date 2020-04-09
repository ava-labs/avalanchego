package wasmvm

// int externalDec(void *context, int x);
import "C"
import (
	"fmt"
	"unsafe"

	wasm "github.com/wasmerio/go-ext-wasm/wasmer"
)

//export externalDec
func externalDec(context unsafe.Pointer, x C.int) C.int {
	return x - 1
}

// Return the standard imports needed by all smart contracts
func standardImports() *wasm.Imports {
	imports, err := wasm.NewDefaultWasiImportObject().Imports()
	if err != nil {
		panic(fmt.Sprintf("couldn't append wasi imports to imports: %v", err))
	}
	imports, err = imports.AppendFunction("externalDec", externalDec, C.externalDec)
	if err != nil {
		panic(fmt.Sprintf("couldn't append externalDec to imports: %v", err))
	}
	return imports
}
