package main

import (
	"fmt"

	wasm "github.com/wasmerio/go-ext-wasm/wasmer"
)

const wasmFilePath = "pkg/rust_wasm_playing_bg.wasm"

func main() {
	imports, err := wasm.NewDefaultWasiImportObject().Imports()
	if err != nil {
		panic(err)
	}

	// Reads the WebAssembly module as bytes.
	bytes, err := wasm.ReadBytes(wasmFilePath)
	if err != nil {
		panic(err)
	}

	// Instantiates the WebAssembly module.
	instance, err := wasm.NewInstanceWithImports(bytes, imports)
	if err != nil {
		panic(err)
	}
	defer instance.Close()

	sum, ok := instance.Exports["sum"]
	if !ok {
		panic("couldn't find sum function")
	}
	theSum, err := sum(1, 1)
	if err != nil {
		panic(err)
	}
	fmt.Printf("sum(1,1) = %v\n", theSum)

	getCount, ok := instance.Exports["getCount"]
	if !ok {
		panic("couldn't find getCount function")
	}
	count, err := getCount()
	if err != nil {
		panic(err)
	}
	fmt.Printf("count = %v\n", count)

	inc, ok := instance.Exports["inc"]
	if !ok {
		panic("couldn't find inc function")
	}
	_, err = inc()
	if err != nil {
		panic(err)
	}
	count, err = getCount()
	if err != nil {
		panic(err)
	}
	fmt.Printf("count = %v\n", count)

}
