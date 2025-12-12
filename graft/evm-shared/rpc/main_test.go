// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"fmt"
	"os"
	"testing"

	"github.com/ava-labs/avalanchego/graft/evm-shared/internal/extrastest"
	"github.com/ava-labs/avalanchego/utils"

	corethtypes "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	subnetevmtypes "github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
)

var testState utils.Atomic[extrastest.TestState]

func TestMain(m *testing.M) {
	fmt.Println("Running with coreth registrations...")
	testState.Set(extrastest.Coreth)
	corethErr := extrastest.RunWith(
		m,
		corethtypes.WithTempRegisteredExtras,
	)

	fmt.Println("Running with subnet-evm registrations...")
	testState.Set(extrastest.SubnetEVM)
	subnetevmErr := extrastest.RunWith(
		m,
		subnetevmtypes.WithTempRegisteredExtras,
	)

	if corethErr != nil || subnetevmErr != nil {
		os.Exit(1)
	}
	os.Exit(0)
}
