// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package option

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

// Build the option block
// [parentID] is the ID of this option's wrapper parent block
// [innerBytes] is the byte representation of a child option block
func Build(
	parentID ids.ID,
	innerBytes []byte,
) (Option, error) {
	opt := option{
		PrntID:     parentID,
		InnerBytes: innerBytes,
	}

	bytes, err := c.Marshal(version, &opt)
	if err != nil {
		return nil, err
	}
	opt.bytes = bytes

	opt.id = hashing.ComputeHash256Array(opt.bytes)
	return &opt, nil
}
