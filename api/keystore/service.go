// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keystore

import (
	"fmt"
	"net/http"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/utils/formatting"
)

type service struct {
	ks *keystore
}

// CreateUser creates an empty user with the provided username and password
func (s *service) CreateUser(_ *http.Request, args *api.UserPass, reply *api.SuccessResponse) error {
	s.ks.log.Info("Keystore: CreateUser called with %.*s", maxUserLen, args.Username)

	if err := s.ks.AddUser(args.Username, args.Password); err != nil {
		return err
	}

	reply.Success = true
	return nil
}

// DeleteUser deletes user with the provided username and password.
func (s *service) DeleteUser(_ *http.Request, args *api.UserPass, reply *api.SuccessResponse) error {
	s.ks.log.Info("Keystore: DeleteUser called with %s", args.Username)

	reply.Success = true
	return s.ks.DeleteUser(args.Username, args.Password)
}

// ListUsersReply is the reply from ListUsers
type ListUsersReply struct {
	Users []string `json:"users"`
}

// ListUsers lists all the registered usernames
func (s *service) ListUsers(_ *http.Request, args *struct{}, reply *ListUsersReply) error {
	s.ks.log.Info("Keystore: ListUsers called")

	reply.Users = []string{}

	s.ks.lock.Lock()
	defer s.ks.lock.Unlock()

	it := s.ks.userDB.NewIterator()
	defer it.Release()
	for it.Next() {
		reply.Users = append(reply.Users, string(it.Key()))
	}
	return it.Error()
}

// ExportUserArgs ...
type ExportUserArgs struct {
	// The username and password
	api.UserPass
	// The encoding for the exported user ("hex" or "cb58")
	Encoding formatting.Encoding `json:"encoding"`
}

// ExportUserReply is the reply from ExportUser
type ExportUserReply struct {
	// String representation of the user
	User string `json:"user"`
	// The encoding for the exported user ("hex" or "cb58")
	Encoding formatting.Encoding `json:"encoding"`
}

// ExportUser exports a serialized encoding of a user's information complete with encrypted database values
func (s *service) ExportUser(_ *http.Request, args *ExportUserArgs, reply *ExportUserReply) error {
	s.ks.log.Info("Keystore: ExportUser called for %s", args.Username)

	s.ks.lock.Lock()
	defer s.ks.lock.Unlock()

	passwordHash, err := s.ks.getPassword(args.Username)
	if err != nil {
		return err
	}
	if !passwordHash.Check(args.Password) {
		return fmt.Errorf("incorrect password for user %q", args.Username)
	}

	userDB := prefixdb.New([]byte(args.Username), s.ks.bcDB)

	userData := user{Hash: *passwordHash}
	it := userDB.NewIterator()
	defer it.Release()
	for it.Next() {
		userData.Data = append(userData.Data, kvPair{
			Key:   it.Key(),
			Value: it.Value(),
		})
	}
	if err := it.Error(); err != nil {
		return err
	}

	// Get byte representation of user
	b, err := c.Marshal(codecVersion, &userData)
	if err != nil {
		return err
	}

	// Encode the user from bytes to string
	reply.User, err = formatting.Encode(args.Encoding, b)
	if err != nil {
		return fmt.Errorf("couldn't encode user to string: %w", err)
	}
	reply.Encoding = args.Encoding
	return nil
}

// ImportUserArgs are arguments for ImportUser
type ImportUserArgs struct {
	// The username and password of the user being imported
	api.UserPass
	// The string representation of the user
	User string `json:"user"`
	// The encoding of [User] ("hex" or "cb58")
	Encoding formatting.Encoding `json:"encoding"`
}

// ImportUser imports a serialized encoding of a user's information complete with encrypted database values,
// integrity checks the password, and adds it to the database
func (s *service) ImportUser(r *http.Request, args *ImportUserArgs, reply *api.SuccessResponse) error {
	s.ks.log.Info("Keystore: ImportUser called for %s", args.Username)

	if args.Username == "" {
		return errEmptyUsername
	}

	s.ks.lock.Lock()
	defer s.ks.lock.Unlock()

	// Decode the user from string to bytes
	userBytes, err := formatting.Decode(args.Encoding, args.User)
	if err != nil {
		return fmt.Errorf("couldn't decode 'user' to bytes: %w", err)
	}

	if passwordHash, err := s.ks.getPassword(args.Username); err == nil || passwordHash != nil {
		return fmt.Errorf("user already exists: %s", args.Username)
	}

	userData := user{}
	if _, err := c.Unmarshal(userBytes, &userData); err != nil {
		return err
	}
	if !userData.Hash.Check(args.Password) {
		return fmt.Errorf("incorrect password for user %q", args.Username)
	}

	usrBytes, err := c.Marshal(codecVersion, &userData.Hash)
	if err != nil {
		return err
	}

	userBatch := s.ks.userDB.NewBatch()
	if err := userBatch.Put([]byte(args.Username), usrBytes); err != nil {
		return err
	}

	userDataDB := prefixdb.New([]byte(args.Username), s.ks.bcDB)
	dataBatch := userDataDB.NewBatch()
	for _, kvp := range userData.Data {
		if err := dataBatch.Put(kvp.Key, kvp.Value); err != nil {
			return fmt.Errorf("error on database put: %w", err)
		}
	}

	if err := atomic.WriteAll(dataBatch, userBatch); err != nil {
		return err
	}
	s.ks.usernameToPassword[args.Username] = &userData.Hash

	reply.Success = true
	return nil
}
