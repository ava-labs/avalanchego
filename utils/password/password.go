// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package password

import (
	"errors"
	"fmt"

	"github.com/nbutton23/zxcvbn-go"
)

// Strength is the strength of a password
type Strength int

const (
	// The scoring mechanism of the zxcvbn package is defined as follows:
	// 0 # too guessable: risky password. (guesses < 10^3)
	// 1 # very guessable: protection from throttled online attacks. (guesses < 10^6)
	// 2 # somewhat guessable: protection from unthrottled online attacks. (guesses < 10^8)
	// 3 # safely unguessable: moderate protection from offline slow-hash scenario. (guesses < 10^10)
	// 4 # very unguessable: strong protection from offline slow-hash scenario. (guesses >= 10^10)
	// Note: We could use the iota keyword to define each of the below consts, but I think in this case
	// it's better to be explicit

	// VeryWeak password
	VeryWeak = 0
	// Weak password
	Weak = 1
	// Fair password
	Fair = 2
	// Strong password
	Strong = 3
	// VeryStrong password
	VeryStrong = 4

	// OK password is the recommended minimum strength for API calls
	OK = Fair

	// maxCheckedPassLen limits the length of the password that should be
	// strength checked.
	//
	// As per issue https://github.com/ava-labs/avalanchego/issues/195 it was found
	// the longer the length of password the slower zxcvbn.PasswordStrength()
	// performs. To avoid performance issues, and a DoS vector, we only strength
	// check the first 50 characters of the password.
	maxCheckedPassLen = 50

	// maxPassLen is the maximum allowed password length
	maxPassLen = 1024
)

var (
	ErrEmptyPassword = errors.New("empty password")
	ErrPassMaxLength = fmt.Errorf("password exceeds maximum length of %d chars", maxPassLen)
	ErrWeakPassword  = errors.New("password is too weak")
)

// SufficientlyStrong returns true if [password] has strength greater than or
// equal to [minimumStrength]
func SufficientlyStrong(password string, minimumStrength Strength) bool {
	if len(password) > maxCheckedPassLen {
		password = password[:maxCheckedPassLen]
	}
	return zxcvbn.PasswordStrength(password, nil).Score >= int(minimumStrength)
}

// IsValid returns nil if [password] is a reasonable length and has strength
// greater than or equal to [minimumStrength]
func IsValid(password string, minimumStrength Strength) error {
	switch {
	case len(password) == 0:
		return ErrEmptyPassword
	case len(password) > maxPassLen:
		return ErrPassMaxLength
	case !SufficientlyStrong(password, minimumStrength):
		return ErrWeakPassword
	default:
		return nil
	}
}
