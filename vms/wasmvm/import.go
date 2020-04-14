package wasmvm

// int externalDec(void *context, int x);
import "C"
import (
	"fmt"

	wasm "github.com/wasmerio/go-ext-wasm/wasmer"
)

// Return the standard imports needed by all smart contracts
func standardImports() *wasm.Imports {
	imports, err := wasm.NewImportObject().Imports()
	if err != nil {
		panic(fmt.Sprintf("couldn't append wasi imports to imports: %v", err))
	}
	return imports
}
