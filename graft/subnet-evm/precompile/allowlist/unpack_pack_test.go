// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package allowlist

import (
	"fmt"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/crypto"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/subnet-evm/precompile/contract"
)

var (
	setAdminSignature      = contract.CalculateFunctionSelector("setAdmin(address)")
	setManagerSignature    = contract.CalculateFunctionSelector("setManager(address)")
	setEnabledSignature    = contract.CalculateFunctionSelector("setEnabled(address)")
	setNoneSignature       = contract.CalculateFunctionSelector("setNone(address)")
	readAllowListSignature = contract.CalculateFunctionSelector("readAllowList(address)")
)

func TestFunctionSignatures(t *testing.T) {
	require := require.New(t)
	setAdminABI := AllowListABI.Methods["setAdmin"]
	require.Equal(setAdminSignature, setAdminABI.ID)

	setManagerABI := AllowListABI.Methods["setManager"]
	require.Equal(setManagerSignature, setManagerABI.ID)

	setEnabledABI := AllowListABI.Methods["setEnabled"]
	require.Equal(setEnabledSignature, setEnabledABI.ID)

	setNoneABI := AllowListABI.Methods["setNone"]
	require.Equal(setNoneSignature, setNoneABI.ID)

	readAllowlistABI := AllowListABI.Methods["readAllowList"]
	require.Equal(readAllowListSignature, readAllowlistABI.ID)
}

func FuzzPackReadAllowlistTest(f *testing.F) {
	f.Add(common.Address{}.Bytes())
	key, err := crypto.GenerateKey()
	require.NoError(f, err)
	addr := crypto.PubkeyToAddress(key.PublicKey)
	f.Add(addr.Bytes())
	f.Fuzz(func(t *testing.T, b []byte) {
		testPackReadAllowlistTest(t, common.BytesToAddress(b))
	})
}

func TestPackReadAllowlistTest(f *testing.T) {
	testPackReadAllowlistTest(f, common.Address{})
}

func testPackReadAllowlistTest(t *testing.T, address common.Address) {
	t.Helper()
	t.Run(fmt.Sprintf("TestPackReadAllowlistTest, address %v", address), func(t *testing.T) {
		require := require.New(t)
		input, err := PackReadAllowList(address)
		require.NoError(err)
		// exclude 4 bytes for function selector
		input = input[4:]
		unpacked, err := UnpackReadAllowListInput(input, true)
		require.NoError(err)
		require.Equal(address, unpacked)
	})
}

func FuzzPackModifyAllowListTest(f *testing.F) {
	f.Add(common.Address{}.Bytes(), uint(0))
	key, err := crypto.GenerateKey()
	require.NoError(f, err)
	addr := crypto.PubkeyToAddress(key.PublicKey)
	f.Add(addr.Bytes(), uint(0))
	f.Fuzz(func(t *testing.T, b []byte, roleIndex uint) {
		testPackModifyAllowListTest(t, common.BytesToAddress(b), getRole(roleIndex))
	})
}

func testPackModifyAllowListTest(t *testing.T, address common.Address, role Role) {
	t.Helper()
	t.Run(fmt.Sprintf("TestPackModifyAllowlistTest, address %v, role %s", address, role.String()), func(t *testing.T) {
		require := require.New(t)
		input, err := PackModifyAllowList(address, role)
		require.NoError(err)
		// exclude 4 bytes for function selector
		input = input[4:]
		unpacked, err := UnpackModifyAllowListInput(input, role, true)
		require.NoError(err)
		require.Equal(address, unpacked)
	})
}

func FuzzPackReadAllowListOutputTest(f *testing.F) {
	f.Fuzz(func(t *testing.T, roleIndex uint) {
		role := getRole(roleIndex)
		packedOutput, err := PackReadAllowListOutput(role.Big())
		require.NoError(t, err)
		require.Equal(t, packedOutput, role.Bytes())
	})
}

func getRole(roleIndex uint) Role {
	index := roleIndex % 4
	switch index {
	case 0:
		return NoRole
	case 1:
		return EnabledRole
	case 2:
		return AdminRole
	case 3:
		return ManagerRole
	default:
		panic("unknown role")
	}
}
