// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

type TxIssueStatus struct {
	Tx        *Tx
	TxID      ids.ID
	StartTime time.Time
}

func (s *TxIssueStatus) GetMessage() (string, error) {
	txJSON, err := json.MarshalIndent(s.Tx, "", "  ")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("issued tx %s in %s\n%s\n", s.TxID, time.Since(s.StartTime), string(txJSON)), nil
}
