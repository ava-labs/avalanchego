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

package choices

import (
	"fmt"

	"github.com/chain4travel/caminogo/ids"
)

var _ Decidable = &TestDecidable{}

// TestDecidable is a test Decidable
type TestDecidable struct {
	IDV              ids.ID
	AcceptV, RejectV error
	StatusV          Status
}

func (d *TestDecidable) ID() ids.ID { return d.IDV }

func (d *TestDecidable) Accept() error {
	switch d.StatusV {
	case Unknown, Rejected:
		return fmt.Errorf("invalid state transaition from %s to %s",
			d.StatusV, Accepted)
	default:
		d.StatusV = Accepted
		return d.AcceptV
	}
}

func (d *TestDecidable) Reject() error {
	switch d.StatusV {
	case Unknown, Accepted:
		return fmt.Errorf("invalid state transaition from %s to %s",
			d.StatusV, Rejected)
	default:
		d.StatusV = Rejected
		return d.RejectV
	}
}

func (d *TestDecidable) Status() Status { return d.StatusV }
