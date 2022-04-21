// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package summary

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

// emptySummary has an ad-hoc construction in that it must have an empty summary ID
func BuildEmptyProposerSummary(coreSummary block.Summary) (ProposerSummaryIntf, error) {
	return &ProposerSummary{
		StatelessSummaryIntf: &StatelessSummary{
			ProBlkID:          ids.Empty,
			ProposerSummaryID: ids.Empty,
		},
	}, nil
}

func BuildProposerSummary(proBlkID ids.ID, coreSummary block.Summary) (ProposerSummaryIntf, error) {
	statelessSummary := StatelessSummary{
		ProBlkID:     proBlkID,
		InnerSummary: coreSummary.Bytes(),
	}

	proSummaryBytes, err := cdc.Marshal(codecVersion, &statelessSummary)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal proposer summary due to: %w", err)
	}
	if err := statelessSummary.initialize(proSummaryBytes); err != nil {
		return nil, err
	}

	return &ProposerSummary{
		StatelessSummaryIntf: &statelessSummary,
		SummaryHeight:        coreSummary.Height(),
	}, nil
}
