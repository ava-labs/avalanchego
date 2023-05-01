// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMetaDataVerifyNil(t *testing.T) {
	require := require.New(t)

	md := (*Metadata)(nil)
	require.ErrorIs(md.Verify(), errNilMetadata)
}

func TestMetaDataVerifyUninitialized(t *testing.T) {
	require := require.New(t)

	md := &Metadata{}
	require.ErrorIs(md.Verify(), errMetadataNotInitialize)
}
