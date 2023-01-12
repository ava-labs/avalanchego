// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"fmt"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/stretchr/testify/require"
)

const noMemo = ""

func TestSameMSigDefinitionsResultedWithSameAlias(t *testing.T) {
	require := require.New(t)
	txID := ids.Empty

	msig1, err := NewMultisigAlias(txID, []ids.ShortID{ids.ShortEmpty}, 1, noMemo)
	msig2, _ := NewMultisigAlias(txID, []ids.ShortID{ids.ShortEmpty}, 1, noMemo)

	require.NoError(err)
	require.Equal(msig1.Alias, msig2.Alias)
}

func TestMemoIsPartOfTheAliasComputation(t *testing.T) {
	require := require.New(t)
	txID := ids.Empty

	msig1, err1 := NewMultisigAlias(txID, []ids.ShortID{ids.ShortEmpty}, 1, noMemo)
	msig2, err2 := NewMultisigAlias(txID, []ids.ShortID{ids.ShortEmpty}, 1, "memo")

	require.NoError(err1)
	require.NoError(err2)
	require.NotEqual(msig1.Alias, msig2.Alias)
}

func TestTxIDIsPartOfAliasComputation(t *testing.T) {
	require := require.New(t)
	txID1, txID2 := ids.FromInt(1), ids.FromInt(2)
	addrs := []ids.ShortID{ids.ShortEmpty}

	msig1, err1 := NewMultisigAlias(txID1, addrs, 1, noMemo)
	msig2, err2 := NewMultisigAlias(txID2, addrs, 1, noMemo)

	require.NoError(err1)
	require.NoError(err2)
	require.NotEqual(msig1.Alias, msig2.Alias)
}

func TestMemoSizeShouldBeLowerThanMaxMemoSize(t *testing.T) {
	require := require.New(t)
	txID := ids.Empty

	tooLargeMemo := make([]byte, avax.MaxMemoSize+1)

	_, err := NewMultisigAlias(txID, []ids.ShortID{ids.ShortEmpty}, 1, string(tooLargeMemo))

	require.Error(err, "memo size should be lower than max memo size")
	require.Equal(err.Error(), "msig alias memo is larger (257 bytes) than max of 256 bytes")
}

func TestMSigAddresesShouldBeUnique(t *testing.T) {
	require := require.New(t)
	txID := ids.Empty
	notUniqueAddrs := []ids.ShortID{ids.ShortEmpty, ids.ShortEmpty}

	msig := MultisigAlias{
		Addresses: notUniqueAddrs,
		Threshold: 1,
	}
	msig.Alias = msig.ComputeAlias(txID)
	err := msig.Verify(txID)

	require.Error(err, "addresses should be unique")
	require.Equal(err.Error(), "duplicate addresses found in multisig alias")
}

func TestMSigShouldContainAtLeastOneAddress(t *testing.T) {
	require := require.New(t)
	txID := ids.Empty

	_, err := NewMultisigAlias(txID, []ids.ShortID{}, 1, noMemo)

	require.Error(err, "should contain at least one address")
	require.Equal(err.Error(), "msig alias threshold is greater, than the number of addresses")
}

func TestMSigThresholdShouldBeLowerThanAddressesCount(t *testing.T) {
	require := require.New(t)
	txID := ids.Empty

	_, err := NewMultisigAlias(txID, []ids.ShortID{ids.ShortEmpty}, 2, noMemo)

	require.Error(err, "threshold should be lower than addresses count")
	require.Equal(err.Error(), "msig alias threshold is greater, than the number of addresses")
}

func TestMSigThresholdShouldBePositive(t *testing.T) {
	require := require.New(t)
	txID := ids.Empty

	_, err := NewMultisigAlias(txID, []ids.ShortID{ids.ShortEmpty}, 0, noMemo)

	require.Error(err, "threshold should be positive")
	require.Equal(err.Error(), "msig alias threshold is greater, than the number of addresses")
}

func TestMSigThresholdIsComparedToUniqueAddressesCount(t *testing.T) {
	require := require.New(t)
	txID := ids.Empty

	_, err := NewMultisigAlias(txID, []ids.ShortID{ids.ShortEmpty, ids.ShortEmpty}, 2, noMemo)

	require.Error(err, "threshold should be lower than unique addresses count")
	require.Equal(err.Error(), "msig alias threshold is greater, than the number of addresses")
}

func TestMSigAddressesShouldBeSorted(t *testing.T) {
	require := require.New(t)

	txID := ids.Empty
	notSortedAddrs := []ids.ShortID{{1}, ids.ShortEmpty}
	msig := MultisigAlias{
		Addresses: notSortedAddrs,
		Threshold: 1,
	}
	msig.Alias = msig.ComputeAlias(txID)
	err := msig.Verify(txID)

	require.Error(err, "addresses should be sorted")
	require.Equal(err.Error(), "addresses must be sorted and unique")
}

func TestKnownValueAliasComputationTests(t *testing.T) {
	knownValueTests := []struct {
		txID          ids.ID
		addresses     []ids.ShortID
		threshold     uint32
		memo          string
		expectedAlias string
	}{
		{
			txID:          ids.Empty,
			addresses:     []ids.ShortID{ids.ShortEmpty},
			threshold:     1,
			memo:          noMemo,
			expectedAlias: "GaD29bC73t6v6hfMfvgFFkT2EuKdSranB",
		},
		{
			txID:          ids.ID{1},
			addresses:     []ids.ShortID{ids.ShortEmpty},
			threshold:     1,
			memo:          noMemo,
			expectedAlias: "Ku5QCiKfFu8qPzs8gdcFmkT7HXnEMReUT",
		},
		{
			txID:          ids.Empty,
			addresses:     []ids.ShortID{ids.ShortEmpty},
			threshold:     1,
			memo:          "Camino Go!",
			expectedAlias: "A2JCzPKavqD1D87YgNRZoC36rehf5EZmR",
		},
		{
			txID:          ids.Empty,
			addresses:     []ids.ShortID{ids.ShortEmpty, {1}},
			threshold:     2,
			memo:          noMemo,
			expectedAlias: "9zT6zU8VuiqcyrqfDWniTsYM2a3NHxiYh",
		},
		{
			txID:          ids.Empty,
			addresses:     []ids.ShortID{ids.ShortEmpty, {1}, {2}},
			threshold:     2,
			memo:          noMemo,
			expectedAlias: "88s5CJ4AatRWp3JEb3vDxgd5Ds6Hq2W4u",
		},
	}

	for _, test := range knownValueTests {
		t.Run(fmt.Sprintf("t-%d-%s-%s", test.threshold, test.memo, test.expectedAlias[:7]), func(t *testing.T) {
			require := require.New(t)

			ma, err := NewMultisigAlias(test.txID, test.addresses, test.threshold, test.memo)
			require.NoError(err)

			require.Equal(test.expectedAlias, ma.Alias.String())
		})
	}
}
