package contracts // so compiler will shut up

/*
package main

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

const wasmFilePath = "basic/counter.wasm"

func main() {
	imports, err := wasm.NewDefaultWasiImportObject().Imports()
	if err != nil {
		panic(err)
	}
	imports, err = imports.AppendFunction("externalDec", externalDec, C.externalDec)
	if err != nil {
		panic(err)
	}
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

	inc, ok := instance.Exports["inc"]
	if !ok {
		panic("couldn't find inc function")
	}
	getCount, ok := instance.Exports["getCount"]
	if !ok {
		panic("couldn't find getCount function")
	}
	add, ok := instance.Exports["add"]
	if !ok {
		panic("couldn't find add function")
	}
	dec, ok := instance.Exports["dec"]
	if !ok {
		panic("couldn't find dec function")
	}

	for i := 0; i < 5; i++ {
		count, err := getCount()
		if err != nil {
			panic(err)
		}
		fmt.Printf("count: %v\n", count)

		_, err = inc()
		if err != nil {
			panic(err)
		}
		fmt.Printf("incremented by 1\n")

		_, err = add(9)
		if err != nil {
			panic(err)
		}
		fmt.Printf("added 9\n")

		_, err = dec()
		if err != nil {
			panic(err)
		}
		fmt.Printf("decremented\n")
	}
}

/*
func main() {
	runtime := wasm3.NewRuntime(&wasm3.Config{
		Environment: wasm3.NewEnvironment(),
		StackSize:   64 * 1024,
	})
	fmt.Println("Created runtime")

	wasmBytes, err := ioutil.ReadFile(wasmFilePath)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Read WASM module\n")

	module, err := runtime.ParseModule(wasmBytes)
	if err != nil {
		panic(err)
	}
	module, err = runtime.LoadModule(module)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Loaded module\n")

	inc, err := runtime.FindFunction("inc")
	if err != nil {
		panic(err)
	}
	fmt.Printf("Found inc function\n")

	getCount, err := runtime.FindFunction("getCount")
	if err != nil {
		panic(err)
	}
	fmt.Printf("Found getCount function\n")

	for i := 0; i < 4; i++ {
		count, err := getCount()
		if err != nil {
			panic(err)
		}
		fmt.Printf("count: %v\n", count)
		_, err = inc()
		if err != nil {
			panic(err)
		}
		fmt.Printf("incremented\n")
	}
}
*/
