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
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
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
	sTx *platform.Tx,
	subnetID ids.ID,
	subnetAuth verify.Verifiable,
) error {
	if err := verifySubnetAuthorization(fx, chainState, sTx, subnetID, subnetAuth); err != nil {
		return err
	}

	_, err := chainState.GetSubnetTransformation(subnetID)
	if err == nil {
		return fmt.Errorf("%q %w", subnetID, errIsImmutable)
	}
	if err != database.ErrNotFound {
		return err
	}

	_, err = chainState.GetSubnetToL1Conversion(subnetID)
	if err == nil {
		return fmt.Errorf("%q %w", subnetID, errIsImmutable)
	}
	if err != database.ErrNotFound {
		return err
	}

	return nil
}

// verifySubnetAuthorization carries out the validation for modifying a subnet.
// The last credential in tx.Creds is used as the subnet authorization.
func verifySubnetAuthorization(
	fx fx.Fx,
	chainState state.Chain,
	tx *platform.Tx,
	subnetID ids.ID,
	subnetAuth verify.Verifiable,
) error {
	subnetOwner, err := chainState.GetSubnetOwner(subnetID)
	if err != nil {
		return err
	}

	return verifyAuthorization(fx, tx, subnetOwner, subnetAuth)
}

// verifyAuthorization carries out the validation of an auth. The last
// credential in tx.Creds is used as the authorization, see [splitCreds].
func verifyAuthorization(
	fx fx.Fx,
	tx *platform.Tx,
	owner fx.Owner,
	auth verify.Verifiable,
) error {
	_, authCred, err := splitCreds(tx)
	if err != nil {
		return err
	}

	if err := fx.VerifyPermission(tx.Unsigned, auth, authCred, owner); err != nil {
		return fmt.Errorf("%w: %w", errUnauthorizedModification, err)
	}
	return nil
}

// baseTxCreds returns the credentials of sTx that authorize the spend of its
// inputs, see [splitCreds]. It returns nil if sTx has no credentials, which
// [verifyAuthorization] rejects.
func baseTxCreds(sTx *platform.Tx) []verify.Verifiable {
	creds, _, _ := splitCreds(sTx)
	return creds
}

// splitCreds splits the credentials of a tx that carries an authorization into
// the credentials that authorize the spend of its inputs and the trailing
// credential that authorizes the tx-specific operation.
func splitCreds(sTx *platform.Tx) ([]verify.Verifiable, verify.Verifiable, error) {
	if len(sTx.Creds) == 0 {
		// Ensure there is at least one credential for the authorization
		return nil, nil, errWrongNumberOfCredentials
	}

	authCredIndex := len(sTx.Creds) - 1
	return sTx.Creds[:authCredIndex], sTx.Creds[authCredIndex], nil
}
