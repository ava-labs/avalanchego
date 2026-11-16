// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package random_network

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateNewTX(t *testing.T) {
	tx := CreateNewTX()
	require.NotNil(t, tx)

	var zero txID
	require.NotEqual(t, zero, tx.ID)
}

func TestTXSerialize(t *testing.T) {
	tx := CreateNewTX()

	b, err := tx.Bytes()
	require.NoError(t, err)
	require.NotEmpty(t, b)

	tx2, err := TxFromBytes(b)
	require.NoError(t, err)
	require.Equal(t, tx, tx2)
}

func TestTXSerializeWithShouldFailVerification(t *testing.T) {
	tx := CreateNewTX()
	tx.SetShouldFailVerification()

	b, err := tx.Bytes()
	require.NoError(t, err)
	require.NotEmpty(t, b)

	tx2, err := TxFromBytes(b)
	require.NoError(t, err)
	require.True(t, tx2.shouldFailVerification)
	require.Equal(t, tx, tx2)
}

func TestTxFromBytesInvalid(t *testing.T) {
	_, err := TxFromBytes([]byte{0x01, 0x02, 0x03})
	require.Error(t, err)
}
