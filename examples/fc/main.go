package main

import (
    "os"
    "github.com/ava-labs/coreth/cmd/geth"
)

func checkError(err error) {
    if err != nil { panic(err) }
}

func main() {
    geth.App.Run(os.Args)
}
