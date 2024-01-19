// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMetaDataVerifyNil(t *testing.T) {
	md := (*Metadata)(nil)
	err := md.Verify()
	require.ErrorIs(t, err, errNilMetadata)
}

func TestMetaDataVerifyUninitialized(t *testing.T) {
	md := &Metadata{}
	err := md.Verify()
	require.ErrorIs(t, err, errMetadataNotInitialize)
}
