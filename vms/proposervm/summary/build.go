// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package summary

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

func BuildProposerSummary(proBlkID ids.ID, coreSummary common.Summary) (common.Summary, error) {
	res := &ProposerSummary{
		StatelessSummary: StatelessSummary{
			ProBlkID:     proBlkID,
			InnerSummary: coreSummary.Bytes(),
		},
		key: coreSummary.Key(), // note: this is not serialized
	}

	proContent, err := cdc.Marshal(codecVersion, res)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal proposerVMKey due to: %w", err)
	}
	res.proContent = proContent

	proSummaryID, err := ids.ToID(hashing.ComputeHash256(proContent))
	if err != nil {
		return nil, fmt.Errorf("cannot compute summary ID: %w", err)
	}
	res.proSummaryID = proSummaryID
	return res, nil
}
