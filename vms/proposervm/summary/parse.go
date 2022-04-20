// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package summary

import "fmt"

func Parse(summaryBytes []byte) (StatelessSummaryIntf, error) {
	var summary StatelessSummaryIntf
	ver, err := cdc.Unmarshal(summaryBytes, &summary)
	if err != nil {
		return nil, fmt.Errorf("could not unmarshal summary due to: %w", err)
	}
	if ver != codecVersion {
		return nil, errWrongStateSyncVersion
	}

	return summary, summary.initialize(summaryBytes)
}
