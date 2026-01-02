// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package password

import (
	"bytes"
	"crypto/rand"

	"golang.org/x/crypto/argon2"
)

// Hash of a password
type Hash struct {
	Password [32]byte `serialize:"true"` // The salted, hashed password
	Salt     [16]byte `serialize:"true"` // The salt
}

// Set updates the password hash to be of the provided password
func (h *Hash) Set(password string) error {
	if _, err := rand.Read(h.Salt[:]); err != nil {
		return err
	}
	// pw is the salted, hashed password
	pw := argon2.IDKey([]byte(password), h.Salt[:], 1, 64*1024, 4, 32)
	copy(h.Password[:], pw[:32])
	return nil
}

// Check returns true iff the provided password was the same as the last
// password set.
func (h *Hash) Check(password string) bool {
	pw := argon2.IDKey([]byte(password), h.Salt[:], 1, 64*1024, 4, 32)
	return bytes.Equal(pw, h.Password[:])
}
