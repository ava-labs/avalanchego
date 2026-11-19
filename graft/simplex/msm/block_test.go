// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metadata

import (
	"testing"

	"github.com/ava-labs/simplex/common"
	"github.com/stretchr/testify/require"
)

func TestIdentifyBlockType(t *testing.T) {
	bvd := &BlockValidationDescriptor{}
	prevSealingBlockHash := common.Digest{1}

	for _, tc := range []struct {
		name     string
		sei      SimplexEpochInfo
		expected BlockType
	}{
		{
			name: "zero block: descriptor set, empty prev sealing block hash",
			sei: SimplexEpochInfo{
				BlockValidationDescriptor: bvd,
			},
			expected: BlockTypeZero,
		},
		{
			name: "sealing block: descriptor set, prev sealing block hash set",
			sei: SimplexEpochInfo{
				NextPChainReferenceHeight: 200,
				BlockValidationDescriptor: bvd,
				PrevSealingBlockHash:      prevSealingBlockHash,
			},
			expected: BlockTypeSealing,
		},
		{
			name: "telock: sealing block seq set, no descriptor",
			sei: SimplexEpochInfo{
				NextPChainReferenceHeight: 200,
				SealingBlockSeq:           8,
			},
			expected: BlockTypeTelock,
		},
		{
			name: "transitioning block: next p-chain reference height set, epoch not yet sealed",
			sei: SimplexEpochInfo{
				NextPChainReferenceHeight: 200,
			},
			expected: BlockTypeTransitioning,
		},
		{
			name:     "normal block in the middle of an epoch",
			sei:      SimplexEpochInfo{},
			expected: BlockTypeNormal,
		},
		{
			name: "first block of a new epoch is a normal block",
			sei: SimplexEpochInfo{
				PChainReferenceHeight: 200,
			},
			expected: BlockTypeNormal,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			smb := &StateMachineBlock{
				Metadata: StateMachineMetadata{SimplexEpochInfo: tc.sei},
			}
			require.Equal(t, tc.expected, smb.Type())
		})
	}
}
