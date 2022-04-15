// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"testing"

	"github.com/chain4travel/caminogo/snow/consensus/snowball"
)

func TestParametersValid(t *testing.T) {
	p := Parameters{
		Parameters: snowball.Parameters{
			K:                     1,
			Alpha:                 1,
			BetaVirtuous:          1,
			BetaRogue:             1,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   2,
		BatchSize: 1,
	}

	if err := p.Valid(); err != nil {
		t.Fatal(err)
	}
}

func TestParametersInvalidParents(t *testing.T) {
	p := Parameters{
		Parameters: snowball.Parameters{
			K:                     1,
			Alpha:                 1,
			BetaVirtuous:          1,
			BetaRogue:             1,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   1,
		BatchSize: 1,
	}

	if err := p.Valid(); err == nil {
		t.Fatalf("Should have failed due to invalid parents")
	}
}

func TestParametersInvalidBatchSize(t *testing.T) {
	p := Parameters{
		Parameters: snowball.Parameters{
			K:                     1,
			Alpha:                 1,
			BetaVirtuous:          1,
			BetaRogue:             1,
			ConcurrentRepolls:     1,
			OptimalProcessing:     1,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Parents:   2,
		BatchSize: 0,
	}

	if err := p.Valid(); err == nil {
		t.Fatalf("Should have failed due to invalid batch size")
	}
}
