// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

//go:build windows
// +build windows

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package socket

import (
	"net"

	"github.com/Microsoft/go-winio"
	"github.com/chain4travel/caminogo/utils/constants"
)

// listen creates a net.Listen backed by a Windows named pipe
func listen(addr string) (net.Listener, error) {
	return winio.ListenPipe(windowsPipeName(addr), nil)
}

// Dial creates a new *Client connected to a Windows named pipe
func Dial(addr string) (*Client, error) {
	c, err := winio.DialPipe(windowsPipeName(addr), nil)
	if err != nil {
		return nil, err
	}
	return &Client{Conn: c, maxMessageSize: int64(constants.DefaultMaxMessageSize)}, nil
}

// windowsPipeName turns an address into a valid Windows named pipes name
func windowsPipeName(addr string) string {
	return `\\.\pipe\` + addr
}
