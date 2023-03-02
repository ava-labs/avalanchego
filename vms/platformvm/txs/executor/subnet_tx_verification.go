// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	errWrongNumberOfCredentials       = errors.New("should have the same number of credentials as inputs")
	errCantFindSubnet                 = errors.New("couldn't find subnet")
	errIsNotSubnet                    = errors.New("is not a subnet")
	errIsImmutable                    = errors.New("is immutable")
	errUnauthorizedSubnetModification = errors.New("unauthorized subnet modification")
)

// verifyPoASubnetAuthorization carries out the validation for modifying a PoA
// subnet. This is an extension of [verifySubnetAuthorization] that additionally
// verifies that the subnet being modified is currently a PoA subnet.
func verifyPoASubnetAuthorization(
	backend *Backend,
	chainState state.Chain,
	sTx *txs.Tx,
	subnetID ids.ID,
	subnetAuth verify.Verifiable,
) ([]verify.Verifiable, error) {
	creds, err := verifySubnetAuthorization(backend, chainState, sTx, subnetID, subnetAuth)
	if err != nil {
		return nil, err
	}

	_, err = chainState.GetSubnetTransformation(subnetID)
	if err == nil {
		return nil, fmt.Errorf("%q %w", subnetID, errIsImmutable)
	}
	if err != database.ErrNotFound {
		return nil, err
	}

	return creds, nil
}

// verifySubnetAuthorization carries out the validation for modifying a subnet.
// The last credential in [sTx.Creds] is used as the subnet authorization.
// Returns the remaining tx credentials that should be used to authorize the
// other operations in the tx.
func verifySubnetAuthorization(
	backend *Backend,
	chainState state.Chain,
	sTx *txs.Tx,
	subnetID ids.ID,
	subnetAuth verify.Verifiable,
) ([]verify.Verifiable, error) {
	if len(sTx.Creds) == 0 {
		// Ensure there is at least one credential for the subnet authorization
		return nil, errWrongNumberOfCredentials
	}

	baseTxCredsLen := len(sTx.Creds) - 1
	subnetCred := sTx.Creds[baseTxCredsLen]

	subnetIntf, _, err := chainState.GetTx(subnetID)
	if err != nil {
		return nil, fmt.Errorf(
			"%w %q: %v",
			errCantFindSubnet,
			subnetID,
			err,
		)
	}

	subnet, ok := subnetIntf.Unsigned.(*txs.CreateSubnetTx)
	if !ok {
		return nil, fmt.Errorf("%q %w", subnetID, errIsNotSubnet)
	}

	if err := backend.Fx.VerifyPermission(sTx.Unsigned, subnetAuth, subnetCred, subnet.Owner); err != nil {
		return nil, fmt.Errorf("%w: %v", errUnauthorizedSubnetModification, err)
	}

	return sTx.Creds[:baseTxCredsLen], nil
}
