//go:build !cgo || windows
// +build !cgo windows

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	// Name is the name of this database for database switches
	Name = "firewood"
)

// New returns an error indicating that Firewood requires CGO and is not available on Windows
func New(file string, configBytes []byte, log logging.Logger) (database.Database, error) {
	return nil, fmt.Errorf("firewood database requires CGO and is not supported on Windows")
}
