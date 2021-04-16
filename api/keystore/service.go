// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keystore

import (
	"fmt"
	"net/http"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/manager/mocks"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/password"
	"github.com/ava-labs/avalanchego/version"
)

type service struct {
	ks *keystore
}

func (s *service) CreateUser(_ *http.Request, args *api.UserPass, reply *api.SuccessResponse) error {
	s.ks.log.Info("Keystore: CreateUser called with %.*s", maxUserLen, args.Username)

	reply.Success = true
	return s.ks.CreateUser(args.Username, args.Password)
}

func (s *service) DeleteUser(_ *http.Request, args *api.UserPass, reply *api.SuccessResponse) error {
	s.ks.log.Info("Keystore: DeleteUser called with %s", args.Username)

	reply.Success = true
	return s.ks.DeleteUser(args.Username, args.Password)
}

type ListUsersReply struct {
	Users []string `json:"users"`
}

func (s *service) ListUsers(_ *http.Request, args *struct{}, reply *ListUsersReply) error {
	s.ks.log.Info("Keystore: ListUsers called")

	var err error
	reply.Users, err = s.ks.ListUsers()
	return err
}

type ImportUserArgs struct {
	// The username and password of the user being imported
	api.UserPass
	// The string representation of the user
	User string `json:"user"`
	// The encoding of [User] ("hex" or "cb58")
	Encoding formatting.Encoding `json:"encoding"`
}

func (s *service) ImportUser(r *http.Request, args *ImportUserArgs, reply *api.SuccessResponse) error {
	s.ks.log.Info("Keystore: ImportUser called for %s", args.Username)

	// Decode the user from string to bytes
	user, err := formatting.Decode(args.Encoding, args.User)
	if err != nil {
		return fmt.Errorf("couldn't decode 'user' to bytes: %w", err)
	}

	reply.Success = true
	return s.ks.ImportUser(args.Username, args.Password, user)
}

type ExportUserArgs struct {
	// The username and password
	api.UserPass
	// The encoding for the exported user ("hex" or "cb58")
	Encoding formatting.Encoding `json:"encoding"`
}

type ExportUserReply struct {
	// String representation of the user
	User string `json:"user"`
	// The encoding for the exported user ("hex" or "cb58")
	Encoding formatting.Encoding `json:"encoding"`
}

func (s *service) ExportUser(_ *http.Request, args *ExportUserArgs, reply *ExportUserReply) error {
	s.ks.log.Info("Keystore: ExportUser called for %s", args.Username)

	userBytes, err := s.ks.ExportUser(args.Username, args.Password)
	if err != nil {
		return err
	}

	// Encode the user from bytes to string
	reply.User, err = formatting.Encode(args.Encoding, userBytes)
	if err != nil {
		return fmt.Errorf("couldn't encode user to string: %w", err)
	}
	reply.Encoding = args.Encoding
	return nil
}

// CreateTestKeystore returns a new keystore that can be utilized for testing
func CreateTestKeystore() (Keystore, *mocks.Manager, error) {
	ks := &keystore{
		log:                logging.NoLog{},
		usernameToPassword: map[string]*password.Hash{},
	}
	mockDBManager := &mocks.Manager{}
	mockDBManager.On("Current").Return(
		&manager.VersionedDatabase{
			Database: memdb.New(),
			Version:  version.NewDefaultVersion(1, 1, 0),
		},
	)
	mockDBManager.On("Previous").Return(nil, false)
	return ks, mockDBManager, ks.initializeDB(mockDBManager)
}
