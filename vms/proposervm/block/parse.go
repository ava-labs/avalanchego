// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"fmt"
)

func Parse(bytes []byte) (Block, error) {
	var block Block
	parsedVersion, err := c.Unmarshal(bytes, &block)
	if err != nil {
		return nil, err
	}
	if parsedVersion != version {
		return nil, fmt.Errorf("expected codec version %d but got %d", version, parsedVersion)
	}
	return block, block.initialize(bytes)
}

func ParseHeader(bytes []byte) (Header, error) {
	header := statelessHeader{}
	parsedVersion, err := c.Unmarshal(bytes, &header)
	if err != nil {
		return nil, err
	}
	if parsedVersion != version {
		return nil, fmt.Errorf("expected codec version %d but got %d", version, parsedVersion)
	}
	header.bytes = bytes
	return &header, nil
}
