// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowtest

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
)

var (
	_ snow.Decidable = (*Decidable)(nil)

	ErrInvalidStateTransition = errors.New("invalid state transition")
)

type Decidable struct {
	IDV     ids.ID
	AcceptV error
	RejectV error
	Status  Status
}

func (d *Decidable) ID() ids.ID {
	return d.IDV
}

func (d *Decidable) Accept(context.Context) error {
	if d.Status == Rejected {
		return fmt.Errorf("%w from %s to %s",
			ErrInvalidStateTransition,
			Rejected,
			Accepted,
		)
	}

	d.Status = Accepted
	return d.AcceptV
}

func (d *Decidable) Reject(context.Context) error {
	if d.Status == Accepted {
		return fmt.Errorf("%w from %s to %s",
			ErrInvalidStateTransition,
			Accepted,
			Rejected,
		)
	}

	d.Status = Rejected
	return d.RejectV
}
