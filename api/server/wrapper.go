// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"net/http"
)

type Wrapper interface {
	// WrapHandler wraps an http.Handler.
	WrapHandler(h http.Handler) http.Handler
}
