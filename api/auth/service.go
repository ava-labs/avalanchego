package auth

import (
	"fmt"
	"net/http"

	"github.com/gorilla/rpc/v2"

	"github.com/ava-labs/gecko/snow/engine/common"
	cjson "github.com/ava-labs/gecko/utils/json"
	"github.com/ava-labs/gecko/utils/logging"
)

// Service ...
type Service struct {
	log   logging.Logger
	*Auth // has to be a reference to the same Auth inside the API sever
}

// NewService returns a new auth API service
func NewService(log logging.Logger, auth *Auth) *common.HTTPHandler {
	newServer := rpc.NewServer()
	codec := cjson.NewCodec()
	newServer.RegisterCodec(codec, "application/json")
	newServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	newServer.RegisterService(&Service{Auth: auth, log: log}, "auth")
	return &common.HTTPHandler{Handler: newServer}
}

// Success ...
type Success struct {
	Success bool `json:"success"`
}

// Password ...
type Password struct {
	Password string `json:"password"` // The authotization password
}

// Token ...
type Token struct {
	Token string `json:"token"` // The new token. Expires in [TokenLifespan].
}

// NewToken returns a new token
func (s *Service) NewToken(_ *http.Request, args *Password, reply *Token) error {
	s.log.Info("Auth: NewToken called")
	if args.Password == "" {
		return fmt.Errorf("password not given")
	}
	token, err := s.newToken(args.Password)
	reply.Token = token
	return err
}

// RevokeTokenArgs ...
type RevokeTokenArgs struct {
	Password
	Token
}

// RevokeToken revokes a token
func (s *Service) RevokeToken(_ *http.Request, args *RevokeTokenArgs, reply *Success) error {
	s.log.Info("Auth: RevokeToken called")
	if args.Password.Password == "" {
		return fmt.Errorf("password not given")
	} else if args.Token.Token == "" {
		return fmt.Errorf("token not given")
	}
	reply.Success = true
	return s.revokeToken(args.Token.Token, args.Password.Password)
}

// ChangePasswordArgs ...
type ChangePasswordArgs struct {
	OldPassword string `json:"oldPassword"` // Current authorization password
	NewPassword string `json:"newPassword"` // New authorization password
}

// ChangePassword changes the password required to create and revoke tokens
// Changing the password makes tokens issued under a previous password invalid
func (s *Service) ChangePassword(_ *http.Request, args *ChangePasswordArgs, reply *Success) error {
	s.log.Info("Auth: ChangePassword called")
	if err := s.changePassword(args.OldPassword, args.NewPassword); err != nil {
		return err
	}
	reply.Success = true
	return nil
}
