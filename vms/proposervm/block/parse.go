// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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
	if parsedVersion != codecVersion {
		return nil, fmt.Errorf("expected codec version %d but got %d", codecVersion, parsedVersion)
	}
	return block, block.initialize(bytes)
}

func ParseHeader(bytes []byte) (Header, error) {
	header := statelessHeader{}
	parsedVersion, err := c.Unmarshal(bytes, &header)
	if err != nil {
		return nil, err
	}
	if parsedVersion != codecVersion {
		return nil, fmt.Errorf("expected codec version %d but got %d", codecVersion, parsedVersion)
	}
	header.bytes = bytes
	return &header, nil
}
