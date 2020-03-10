// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keystore

import (
	"bytes"
	"crypto/rand"

	"golang.org/x/crypto/argon2"
)

// User describes a user of the keystore
type User struct {
	Password [32]byte `serialize:"true"` // The salted, hashed password
	Salt     [16]byte `serialize:"true"` // The salt
}

// Initialize ...
func (usr *User) Initialize(password string) error {
	_, err := rand.Read(usr.Salt[:])
	if err != nil {
		return err
	}
	// pw is the salted, hashed password
	pw := argon2.IDKey([]byte(password), usr.Salt[:], 1, 64*1024, 4, 32)
	copy(usr.Password[:], pw[:32])
	return nil
}

// CheckPassword ...
func (usr *User) CheckPassword(password string) bool {
	pw := argon2.IDKey([]byte(password), usr.Salt[:], 1, 64*1024, 4, 32)
	return bytes.Equal(pw, usr.Password[:])
}
