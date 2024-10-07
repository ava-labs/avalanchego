// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

const (
	// CurrentlyValidating means that the validator corresponding to the
	// provided validationID is currently registered on the P-chain.
	CurrentlyValidating RegistrationStatus = iota
	// NotCurrentlyValidating means that the validator corresponding to the
	// provided validationID is not currently registered on the P-chain. If the
	// validationID is known to correspond to a validator that was previously
	// registered, then the P-chain guarantees that the validator will never be
	// registered again.
	NotCurrentlyValidating
	// WillNeverValidate means that the validator corresponding to the provided
	// validationID is not currently registered on the P-chain and never will
	// be. It does not imply that the validator was never registered on the
	// P-chain.
	WillNeverValidate
	// invalidRegistrationStatus is used during message verification to indicate
	// any malformed registration statuses.
	invalidRegistrationStatus
)

var ErrInvalidRegistrationStatus = errors.New("invalid registration status")

type RegistrationStatus byte

func (s RegistrationStatus) String() string {
	switch s {
	case CurrentlyValidating:
		return "CurrentlyValidating"
	case NotCurrentlyValidating:
		return "NotCurrentlyValidating"
	case WillNeverValidate:
		return "WillNeverValidate"
	default:
		return fmt.Sprintf("Unknown Registration Status: %d", s)
	}
}

func (s RegistrationStatus) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), nil
}

func (s *RegistrationStatus) UnmarshalJSON(b []byte) error {
	switch string(b) {
	case "null":
	case `"CurrentlyValidating"`:
		*s = CurrentlyValidating
	case `"NotCurrentlyValidating"`:
		*s = NotCurrentlyValidating
	case `"WillNeverValidate"`:
		*s = WillNeverValidate
	default:
		return fmt.Errorf("%w: %s", ErrInvalidRegistrationStatus, string(b))
	}
	return nil
}

// SubnetValidatorRegistration reports if a validator is registered on the
// P-chain.
type SubnetValidatorRegistration struct {
	payload

	ValidationID ids.ID `serialize:"true" json:"validationID"`
	// Registered being true means that validationID is currently a validator on
	// the P-chain.
	//
	// Registered being false means that validationID is not and can never
	// become a validator on the P-chain. It is possible that validationID was
	// previously a validator on the P-chain.
	Status RegistrationStatus `serialize:"true" json:"status"`
}

func (r *SubnetValidatorRegistration) Verify() error {
	if r.Status >= invalidRegistrationStatus {
		return fmt.Errorf("%w: %d", ErrInvalidRegistrationStatus, r.Status)
	}
	return nil
}

// NewSubnetValidatorRegistration creates a new initialized
// SubnetValidatorRegistration.
func NewSubnetValidatorRegistration(
	validationID ids.ID,
	status RegistrationStatus,
) (*SubnetValidatorRegistration, error) {
	msg := &SubnetValidatorRegistration{
		ValidationID: validationID,
		Status:       status,
	}
	return msg, Initialize(msg)
}

// ParseSubnetValidatorRegistration parses bytes into an initialized
// SubnetValidatorRegistration.
func ParseSubnetValidatorRegistration(b []byte) (*SubnetValidatorRegistration, error) {
	payloadIntf, err := Parse(b)
	if err != nil {
		return nil, err
	}
	payload, ok := payloadIntf.(*SubnetValidatorRegistration)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrWrongType, payloadIntf)
	}
	return payload, nil
}
