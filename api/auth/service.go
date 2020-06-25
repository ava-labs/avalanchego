package auth

import (
	"errors"
	"net/http"
	"time"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/gorilla/rpc/v2"

	"github.com/ava-labs/gecko/snow/engine/common"
	cjson "github.com/ava-labs/gecko/utils/json"
	"github.com/ava-labs/gecko/utils/logging"
)

// Service ...
type Service struct {
	*Auth
}

// NewService returns a new auth API service
func NewService(log logging.Logger, auth *Auth) *common.HTTPHandler {
	newServer := rpc.NewServer()
	codec := cjson.NewCodec()
	newServer.RegisterCodec(codec, "application/json")
	newServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	newServer.RegisterService(&Service{Auth: auth}, "auth")
	return &common.HTTPHandler{Handler: newServer}
}

// Success ...
type Success struct {
	Success bool `json:"success"`
}

// NewTokenArgs ...
type NewTokenArgs struct {
	Password string `json:"password"`
}

// NewTokenResponse ...
type NewTokenResponse struct {
	Token string `json:"token"`
}

// NewToken ...
func (s *Service) NewToken(_ *http.Request, args *NewTokenArgs, reply *NewTokenResponse) error {
	s.lock.RLock()
	defer s.lock.RUnlock()
	if args.Password != s.Password {
		return errors.New("incorrect password")
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.StandardClaims{ // Make a new token that expires in one week
		ExpiresAt: time.Now().Add(TokenLifespan).Unix(),
	})
	var err error
	reply.Token, err = token.SignedString([]byte(s.Password))
	return err
}

// ChangePasswordArgs ...
type ChangePasswordArgs struct {
	OldPassword string `json:"oldPassword"`
	NewPassword string `json:"newPassword"`
}

// ChangePassword ...
func (s *Service) ChangePassword(_ *http.Request, args *ChangePasswordArgs, reply *Success) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	if args.OldPassword != s.Password {
		return errors.New("incorrect password")
	}
	s.Password = args.NewPassword // TODO: Add validation for password
	reply.Success = true
	return nil
}
