// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
)

func main() {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}

	if err := os.WriteFile(os.Args[1], []byte(fmt.Sprintf("http://%s", listener.Addr())), 0o600); err != nil {
		panic(err)
	}

	http.HandleFunc("/ok", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	if err := http.Serve(listener, nil); err != nil {
		panic(err)
	}
}
