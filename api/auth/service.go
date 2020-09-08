package auth

import (
	"fmt"
	"net/http"

	"github.com/gorilla/rpc/v2"

	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/utils/logging"

	cjson "github.com/ava-labs/gecko/utils/json"
)

const (
	maxEndpoints = 128
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
	log.AssertNoError(newServer.RegisterService(&Service{Auth: auth, log: log}, "auth"))
	return &common.HTTPHandler{Handler: newServer}
}

// Success ...
type Success struct {
	Success bool `json:"success"`
}

// Password ...
type Password struct {
	Password string `json:"password"` // The authorization password
}

// NewTokenArgs ...
type NewTokenArgs struct {
	Password
	// Endpoints that may be accessed with this token
	// e.g. if endpoints is ["/ext/bc/X", "/ext/admin"] then the token holder
	// can hit the X-Chain API and the admin API
	// If [Endpoints] contains an element "*" then the token
	// allows access to all API endpoints
	// [Endpoints] must have between 1 and [maxEndpoints] elements
	Endpoints []string `json:"endpoints"`
}

// Token ...
type Token struct {
	Token string `json:"token"` // The new token. Expires in [TokenLifespan].
}

// NewToken returns a new token
func (s *Service) NewToken(_ *http.Request, args *NewTokenArgs, reply *Token) error {
	s.log.Info("Auth: NewToken called")
	if args.Password.Password == "" {
		return fmt.Errorf("argument 'password' not given")
	}
	if l := len(args.Endpoints); l < 1 || l > maxEndpoints {
		return fmt.Errorf("argument 'endpoints' must have between %d and %d elements, but has %d", 1, maxEndpoints, l)
	}
	token, err := s.newToken(args.Password.Password, args.Endpoints)
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
