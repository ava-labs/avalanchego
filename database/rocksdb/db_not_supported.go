// +build !linux !amd64 !rocksdbenabled

// ^ Only build this file if this computer is not linux OR it's not AMD64 OR rocksdb is disabled
// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package rocksdb

import (
	"errors"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var errUnsupportedDatabase = errors.New("database isn't suppported")

// New returns an error.
func New(file string, log logging.Logger) (database.Database, error) {
	return nil, errUnsupportedDatabase
}
