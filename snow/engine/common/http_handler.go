// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"net/http"
)

// LockOption allows the vm to specify their lock option based on their endpoint
type LockOption uint32

// List of all allowed options
const (
	WriteLock = iota
	ReadLock
	NoLock
)

type HTTPHandler struct {
	LockOptions LockOption
	Handler     http.Handler
}
