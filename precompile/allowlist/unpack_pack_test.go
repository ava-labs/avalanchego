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

func FuzzPackReadAllowlistTestSkipCheck(f *testing.F) {
	f.Fuzz(func(t *testing.T, b []byte) {
		require := require.New(t)
		res, err := UnpackReadAllowListInput(b, true)
		oldRes, oldErr := OldUnpackReadAllowList(b)
		if oldErr != nil {
			require.ErrorContains(err, oldErr.Error())
		} else {
			require.NoError(err)
		}
		require.Equal(oldRes, res)
	})
}

func TestPackReadAllowlistTest(f *testing.T) {
	testPackReadAllowlistTest(f, common.Address{})
}

func testPackReadAllowlistTest(t *testing.T, address common.Address) {
	t.Helper()
	t.Run(fmt.Sprintf("TestPackReadAllowlistTest, address %v", address), func(t *testing.T) {
		require := require.New(t)
		// use new Pack/Unpack methods
		input, err := PackReadAllowList(address)
		require.NoError(err)
		// exclude 4 bytes for function selector
		input = input[4:]
		unpacked, err := UnpackReadAllowListInput(input, true)
		require.NoError(err)
		require.Equal(address, unpacked)

		// use old Pack/Unpack methods
		input = OldPackReadAllowList(address)
		// exclude 4 bytes for function selector
		input = input[4:]
		require.NoError(err)
		unpacked, err = OldUnpackReadAllowList(input)
		require.NoError(err)
		require.Equal(address, unpacked)

		// now mix and match old and new methods
		input, err = PackReadAllowList(address)
		require.NoError(err)
		// exclude 4 bytes for function selector
		input = input[4:]
		input2 := OldPackReadAllowList(address)
		// exclude 4 bytes for function selector
		input2 = input2[4:]
		require.Equal(input, input2)
		unpacked, err = UnpackReadAllowListInput(input2, true)
		require.NoError(err)
		unpacked2, err := OldUnpackReadAllowList(input)
		require.NoError(err)
		require.Equal(unpacked, unpacked2)
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

func FuzzPackModifyAllowlistTestSkipCheck(f *testing.F) {
	f.Fuzz(func(t *testing.T, b []byte) {
		require := require.New(t)
		res, err := UnpackModifyAllowListInput(b, AdminRole, true)
		oldRes, oldErr := OldUnpackModifyAllowList(b, AdminRole)
		if oldErr != nil {
			require.ErrorContains(err, oldErr.Error())
		} else {
			require.NoError(err)
		}
		require.Equal(oldRes, res)
	})
}

func testPackModifyAllowListTest(t *testing.T, address common.Address, role Role) {
	t.Helper()
	t.Run(fmt.Sprintf("TestPackModifyAllowlistTest, address %v, role %s", address, role.String()), func(t *testing.T) {
		require := require.New(t)
		// use new Pack/Unpack methods
		input, err := PackModifyAllowList(address, role)
		require.NoError(err)
		// exclude 4 bytes for function selector
		input = input[4:]
		unpacked, err := UnpackModifyAllowListInput(input, role, true)
		require.NoError(err)
		require.Equal(address, unpacked)

		// use old Pack/Unpack methods
		input, err = OldPackModifyAllowList(address, role)
		require.NoError(err)
		// exclude 4 bytes for function selector
		input = input[4:]
		require.NoError(err)

		unpacked, err = OldUnpackModifyAllowList(input, role)
		require.NoError(err)

		require.Equal(address, unpacked)

		// now mix and match new and old methods
		input, err = PackModifyAllowList(address, role)
		require.NoError(err)
		// exclude 4 bytes for function selector
		input = input[4:]
		input2, err := OldPackModifyAllowList(address, role)
		require.NoError(err)
		// exclude 4 bytes for function selector
		input2 = input2[4:]
		require.Equal(input, input2)
		unpacked, err = UnpackModifyAllowListInput(input2, role, true)
		require.NoError(err)
		unpacked2, err := OldUnpackModifyAllowList(input, role)
		require.NoError(err)
		require.Equal(unpacked, unpacked2)
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

func OldPackReadAllowList(address common.Address) []byte {
	input := make([]byte, 0, contract.SelectorLen+common.HashLength)
	input = append(input, readAllowListSignature...)
	input = append(input, common.BytesToHash(address[:]).Bytes()...)
	return input
}

func OldUnpackReadAllowList(input []byte) (common.Address, error) {
	if len(input) != allowListInputLen {
		return common.Address{}, fmt.Errorf("invalid input length for read allow list: %d", len(input))
	}
	return common.BytesToAddress(input), nil
}

func OldPackModifyAllowList(address common.Address, role Role) ([]byte, error) {
	// function selector (4 bytes) + hash for address
	input := make([]byte, 0, contract.SelectorLen+common.HashLength)

	switch role {
	case AdminRole:
		input = append(input, setAdminSignature...)
	case ManagerRole:
		input = append(input, setManagerSignature...)
	case EnabledRole:
		input = append(input, setEnabledSignature...)
	case NoRole:
		input = append(input, setNoneSignature...)
	default:
		return nil, fmt.Errorf("cannot pack modify list input with invalid role: %s", role)
	}

	input = append(input, common.BytesToHash(address[:]).Bytes()...)
	return input, nil
}

func OldUnpackModifyAllowList(input []byte, _ Role) (common.Address, error) {
	if len(input) != allowListInputLen {
		return common.Address{}, fmt.Errorf("invalid input length for modifying allow list: %d", len(input))
	}
	return common.BytesToAddress(input), nil
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
