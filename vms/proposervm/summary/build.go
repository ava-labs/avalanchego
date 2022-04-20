// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package summary

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

func BuildProposerSummary(proBlkID ids.ID, coreSummary common.Summary) (common.Summary, error) {
	res := &ProposerSummary{
		StatelessSummary: StatelessSummary{
			ProBlkID:     proBlkID,
			InnerSummary: coreSummary.Bytes(),
		},
		SummaryKey: coreSummary.Key(), // note: this is not serialized
	}

	proSummaryBytes, err := cdc.Marshal(codecVersion, res)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal proposer summary due to: %w", err)
	}
	if err := res.StatelessSummary.initialize(proSummaryBytes); err != nil {
		return nil, err
	}

	return res, nil
}
