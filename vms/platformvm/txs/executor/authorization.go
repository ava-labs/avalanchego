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
// The last credential in tx.Creds is used as the subnet authorization. The
// remaining credentials, returned by [baseTxCreds], authorize the other
// operations in the tx.
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
// credential in tx.Creds is used as the authorization. The remaining
// credentials, returned by [baseTxCreds], authorize the other operations in
// the tx.
func verifyAuthorization(
	fx fx.Fx,
	tx *platform.Tx,
	owner fx.Owner,
	auth verify.Verifiable,
) error {
	if len(tx.Creds) == 0 {
		// Ensure there is at least one credential for the subnet authorization
		return errWrongNumberOfCredentials
	}

	authCred := tx.Creds[len(tx.Creds)-1]
	if err := fx.VerifyPermission(tx.Unsigned, auth, authCred, owner); err != nil {
		return fmt.Errorf("%w: %w", errUnauthorizedModification, err)
	}
	return nil
}

// baseTxCreds returns the credentials of sTx that authorize the spend of its
// inputs, which are all of its credentials except the trailing authorization
// credential consumed by verifyAuthorization.
func baseTxCreds(sTx *platform.Tx) []verify.Verifiable {
	if len(sTx.Creds) == 0 {
		return nil
	}
	return sTx.Creds[:len(sTx.Creds)-1]
}
