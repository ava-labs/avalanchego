// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	errWrongNumberOfCredentials = errors.New("should have the same number of credentials as inputs")
	errIsImmutable              = errors.New("is immutable")
	errUnauthorizedModification = errors.New("unauthorized modification")
)

// verifyPoASubnetAuthorization carries out the validation for modifying a PoA
// subnet. This is an extension of [verifySubnetAuthorization] that additionally
// verifies that the subnet being modified is currently a PoA subnet.
func verifyPoASubnetAuthorization(
	fx fx.Fx,
	chainState state.Chain,
	sTx *txs.Tx,
	subnetID ids.ID,
	subnetAuth verify.Verifiable,
) ([]verify.Verifiable, error) {
	creds, err := verifySubnetAuthorization(fx, chainState, sTx, subnetID, subnetAuth)
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

	_, err = chainState.GetSubnetToL1Conversion(subnetID)
	if err == nil {
		return nil, fmt.Errorf("%q %w", subnetID, errIsImmutable)
	}
	if err != database.ErrNotFound {
		return nil, err
	}

	return creds, nil
}

// verifySubnetAuthorization carries out the validation for modifying a subnet.
// The last credential in [tx.Creds] is used as the subnet authorization.
// Returns the remaining tx credentials that should be used to authorize the
// other operations in the tx.
func verifySubnetAuthorization(
	fx fx.Fx,
	chainState state.Chain,
	tx *txs.Tx,
	subnetID ids.ID,
	subnetAuth verify.Verifiable,
) ([]verify.Verifiable, error) {
	subnetOwner, err := chainState.GetSubnetOwner(subnetID)
	if err != nil {
		return nil, err
	}

	return verifyAuthorization(fx, tx, subnetOwner, subnetAuth)
}

// verifyAuthorization carries out the validation of an auth. The last
// credential in [tx.Creds] is used as the authorization.
// Returns the remaining tx credentials that should be used to authorize the
// other operations in the tx.
func verifyAuthorization(
	fx fx.Fx,
	tx *txs.Tx,
	owner fx.Owner,
	auth verify.Verifiable,
) ([]verify.Verifiable, error) {
	if len(tx.Creds) == 0 {
		// Ensure there is at least one credential for the subnet authorization
		return nil, errWrongNumberOfCredentials
	}

	baseTxCredsLen := len(tx.Creds) - 1
	authCred := tx.Creds[baseTxCredsLen]

	if err := fx.VerifyPermission(tx.Unsigned, auth, authCred, owner); err != nil {
		return nil, fmt.Errorf("%w: %w", errUnauthorizedModification, err)
	}

	return tx.Creds[:baseTxCredsLen], nil
}
