// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/linkeddb"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/components/fee"
)

var calculator = &fee.ValidatorState{
	Current:  0,
	Target:   10_000,
	Capacity: 20_000,
	Excess:   0,
	MinFee:   2048,
	K:        60_480_000_000,
}

func TestAddValidator(t *testing.T) {
	var (
		require                   = require.New(t)
		validatorDB               = memdb.New()
		validatorLinkedDB         = linkeddb.New(validatorDB, 2048)
		validatorManager          = validators.NewManager()
		validatorWeightDiffsDB    = memdb.New()
		validatorPublicKeyDiffsDB = memdb.New()
		subnetOnlyValidators      = newSubnetOnlyValidators(
			calculator,
			validatorLinkedDB,
			validatorManager,
		)

		validationID = ids.GenerateTestID()
		subnetID     = ids.GenerateTestID()
		nodeID       = ids.GenerateTestNodeID()
		weight       = uint64(10000)
		balance      = uint64(100)
		endTime      = uint64(0)
		height       = uint64(2)
	)

	sk, err := bls.NewSecretKey()
	require.NoError(err)
	pk := bls.PublicFromSecretKey(sk)

	require.NoError(subnetOnlyValidators.AddValidator(validationID, subnetID, nodeID, weight, balance, endTime, pk))

	require.Equal(&subnetOnlyValidatorDiff{
		weightDiff: &ValidatorWeightDiff{
			Decrease: false,
			Amount:   weight,
		},
		status: added,
	}, subnetOnlyValidators.validatorDiffs[validationID])
	require.Equal(fee.Gas(1), subnetOnlyValidators.calculator.Current)

	require.NoError(subnetOnlyValidators.Write(
		height,
		validatorWeightDiffsDB,
		validatorPublicKeyDiffsDB,
	))

	got, err := getSubnetOnlyValidator(subnetOnlyValidators.validatorDB, validationID)
	require.NoError(err)
	require.Equal(&subnetOnlyValidator{
		ValidationID: validationID,
		SubnetID:     subnetID,
		NodeID:       nodeID,
		MinNonce:     0,
		Weight:       weight,
		Balance:      balance,
		PublicKey:    new(bls.PublicKey), // TODO: Populate

		EndTime: endTime,
	}, got)
}
