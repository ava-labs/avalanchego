// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keystore

import (
	"fmt"
	"net/http"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/logging"
)

type service struct {
	ks *keystore
}

func (s *service) CreateUser(_ *http.Request, args *api.UserPass, _ *api.EmptyReply) error {
	s.ks.log.Warn("deprecated API called",
		zap.String("service", "keystore"),
		zap.String("method", "createUser"),
		logging.UserString("username", args.Username),
	)

	return s.ks.CreateUser(args.Username, args.Password)
}

func (s *service) DeleteUser(_ *http.Request, args *api.UserPass, _ *api.EmptyReply) error {
	s.ks.log.Warn("deprecated API called",
		zap.String("service", "keystore"),
		zap.String("method", "deleteUser"),
		logging.UserString("username", args.Username),
	)

	return s.ks.DeleteUser(args.Username, args.Password)
}

type ListUsersReply struct {
	Users []string `json:"users"`
}

func (s *service) ListUsers(_ *http.Request, _ *struct{}, reply *ListUsersReply) error {
	s.ks.log.Warn("deprecated API called",
		zap.String("service", "keystore"),
		zap.String("method", "listUsers"),
	)

	var err error
	reply.Users, err = s.ks.ListUsers()
	return err
}

type ImportUserArgs struct {
	// The username and password of the user being imported
	api.UserPass
	// The string representation of the user
	User string `json:"user"`
	// The encoding of [User] ("hex")
	Encoding formatting.Encoding `json:"encoding"`
}

func (s *service) ImportUser(_ *http.Request, args *ImportUserArgs, _ *api.EmptyReply) error {
	s.ks.log.Warn("deprecated API called",
		zap.String("service", "keystore"),
		zap.String("method", "importUser"),
		logging.UserString("username", args.Username),
	)

	// Decode the user from string to bytes
	user, err := formatting.Decode(args.Encoding, args.User)
	if err != nil {
		return fmt.Errorf("couldn't decode 'user' to bytes: %w", err)
	}

	return s.ks.ImportUser(args.Username, args.Password, user)
}

type ExportUserArgs struct {
	// The username and password
	api.UserPass
	// The encoding for the exported user ("hex")
	Encoding formatting.Encoding `json:"encoding"`
}

type ExportUserReply struct {
	// String representation of the user
	User string `json:"user"`
	// The encoding for the exported user ("hex")
	Encoding formatting.Encoding `json:"encoding"`
}

func (s *service) ExportUser(_ *http.Request, args *ExportUserArgs, reply *ExportUserReply) error {
	s.ks.log.Warn("deprecated API called",
		zap.String("service", "keystore"),
		zap.String("method", "exportUser"),
		logging.UserString("username", args.Username),
	)

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
