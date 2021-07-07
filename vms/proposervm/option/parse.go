// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package option

import (
	"errors"

	"github.com/ava-labs/avalanchego/utils/hashing"
)

var errWrongVersion = errors.New("wrong version")

func Parse(bytes []byte) (Option, error) {
	block := option{
		id:    hashing.ComputeHash256Array(bytes),
		bytes: bytes,
	}
	parsedVersion, err := c.Unmarshal(bytes, &block)
	if err != nil {
		return nil, err
	}
	if parsedVersion != version {
		return nil, errWrongVersion
	}

	return &block, nil
}
