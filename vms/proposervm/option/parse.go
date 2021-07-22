// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package option

import (
	"fmt"

	"github.com/ava-labs/avalanchego/utils/hashing"
)

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
		return nil, fmt.Errorf("expected codec version %d but got %d", version, parsedVersion)
	}

	return &block, nil
}
