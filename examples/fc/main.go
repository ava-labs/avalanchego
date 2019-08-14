package main

import (
    "os"
    "github.com/Determinant/coreth/cmd/geth"
)

func checkError(err error) {
    if err != nil { panic(err) }
}

func main() {
    geth.App.Run(os.Args)
}
