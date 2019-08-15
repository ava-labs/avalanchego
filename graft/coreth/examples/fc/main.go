package main

import (
	"github.com/ava-labs/coreth/cmd/geth"
	"os"
)

func checkError(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	geth.App.Run(os.Args)
}
