// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposer

import (
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
)

var _ utils.Sortable[validatorData] = validatorData{}

// validatorData just sorts validators.GetValidatorOutput
// in the canonical way we need in proposerVM
type validatorData struct {
	*validators.GetValidatorOutput
}

func (d validatorData) Less(other validatorData) bool {
	return d.NodeID.Less(other.NodeID)
}
