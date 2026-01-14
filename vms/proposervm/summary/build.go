// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package summary

import (
	"fmt"

	"github.com/ava-labs/avalanchego/utils/hashing"
)

func Build(
	forkHeight uint64,
	block []byte,
	coreSummary []byte,
) (StateSummary, error) {
	summary := stateSummary{
		Height:       forkHeight,
		Block:        block,
		InnerSummary: coreSummary,
	}

	bytes, err := Codec.Marshal(CodecVersion, &summary)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal proposer summary due to: %w", err)
	}

	summary.id = hashing.ComputeHash256Array(bytes)
	summary.bytes = bytes
	return &summary, nil
}
