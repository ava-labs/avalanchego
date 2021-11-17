// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

// Fx is the interface a feature extension must implement to support the
// Platform Chain.
type Fx interface {
	// Initialize this feature extension to be running under this VM. Should
	// return an error if the VM is incompatible.
	Initialize(vm interface{}) error

	// Notify this Fx that the VM is in bootstrapping
	Bootstrapping() error

	// Notify this Fx that the VM is bootstrapped
	Bootstrapped() error

	// VerifyTransfer verifies that the specified transaction can spend the
	// provided utxo with no restrictions on the destination. If the transaction
	// can't spend the output based on the input and credential, a non-nil error
	// should be returned.
	VerifyTransfer(tx, in, cred, utxo interface{}) error

	// VerifyPermission returns nil iff [cred] proves that [controlGroup]
	// assents to [tx]
	VerifyPermission(tx, in, cred, controlGroup interface{}) error

	// CreateOutput creates a new output with the provided control group worth
	// the specified amount
	CreateOutput(amount uint64, controlGroup interface{}) (interface{}, error)
}

type Owner interface {
	verify.Verifiable
	snow.ContextInitializable
}

type Owned interface {
	Owners() interface{}
}
