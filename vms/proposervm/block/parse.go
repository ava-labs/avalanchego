// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"fmt"
)

func Parse(bytes []byte) (Block, bool, error) {
	var (
		block        Block
		requireBanff bool
	)
	parsedVersion, err := apricotCodec.Unmarshal(bytes, &block)
	if err != nil {
		parsedVersion, err = banffCodec.Unmarshal(bytes, &block)
		requireBanff = true
	}
	if err != nil {
		return nil, false, err
	}
	if parsedVersion != codecVersion {
		return nil, false, fmt.Errorf("expected codec version %d but got %d", codecVersion, parsedVersion)
	}
	return block, requireBanff, block.initialize(bytes)
}

func ParseHeader(bytes []byte) (Header, error) {
	header := statelessHeader{}
	parsedVersion, err := banffCodec.Unmarshal(bytes, &header)
	if err != nil {
		return nil, err
	}
	if parsedVersion != codecVersion {
		return nil, fmt.Errorf("expected codec version %d but got %d", codecVersion, parsedVersion)
	}
	header.bytes = bytes
	return &header, nil
}
