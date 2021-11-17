// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package choices

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

// TestDecidable is a test Decidable
type TestDecidable struct {
	IDV              ids.ID
	AcceptV, RejectV error
	StatusV          Status
}

// ID implements the Decidable interface
func (d *TestDecidable) ID() ids.ID { return d.IDV }

// Accept implements the Decidable interface
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

// Reject implements the Decidable interface
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

// Status implements the Decidable interface
func (d *TestDecidable) Status() Status { return d.StatusV }
