// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMetaDataVerifyNil(t *testing.T) {
	md := (*Metadata)(nil)
	require.ErrorIs(t, md.Verify(), errNilMetadata)
}

func TestMetaDataVerifyUninitialized(t *testing.T) {
	md := &Metadata{}
	require.ErrorIs(t, md.Verify(), errMetadataNotInitialize)
}
