// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
)

func MakeStatefulTx(tx *signed.Tx) (StatefulTx, error) {
	switch utx := tx.Unsigned.(type) {
	case *unsigned.AddDelegatorTx:
		return &StatefulAddDelegatorTx{
			AddDelegatorTx: utx,
			txID:           tx.ID(),
		}, nil

	case *unsigned.AddSubnetValidatorTx:
		return &StatefulAddSubnetValidatorTx{
			AddSubnetValidatorTx: utx,
			txID:                 tx.ID(),
		}, nil

	case *unsigned.AddValidatorTx:
		return &StatefulAddValidatorTx{
			AddValidatorTx: utx,
			txID:           tx.ID(),
		}, nil
	case *unsigned.AdvanceTimeTx:
		return &StatefulAdvanceTimeTx{
			AdvanceTimeTx: utx,
		}, nil
	case *unsigned.RewardValidatorTx:
		return &StatefulRewardValidatorTx{
			RewardValidatorTx: utx,
		}, nil
	case *unsigned.CreateChainTx:
		return &StatefulCreateChainTx{
			CreateChainTx: utx,
			txID:          tx.ID(),
		}, nil
	case *unsigned.CreateSubnetTx:
		return &StatefulCreateSubnetTx{
			CreateSubnetTx: utx,
			txID:           tx.ID(),
		}, nil
	case *unsigned.ImportTx:
		return &StatefulImportTx{
			ImportTx: utx,
		}, nil
	case *unsigned.ExportTx:
		return &StatefulExportTx{
			ExportTx: utx,
		}, nil

	default:
		return nil, fmt.Errorf("unverifiable tx type")
	}
}
