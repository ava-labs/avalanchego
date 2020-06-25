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

// NewTokenArgs ...
type NewTokenArgs struct {
	Password string `json:"password"` // The authotization password
}

// NewTokenResponse ...
type NewTokenResponse struct {
	Token string `json:"token"` // The new token. Expires in [TokenLifespan].
}

// NewToken returns a new token
func (s *Service) NewToken(_ *http.Request, args *NewTokenArgs, reply *NewTokenResponse) error {
	s.log.Info("Auth: NewToken called")
	s.lock.RLock()
	defer s.lock.RUnlock()
	if args.Password != s.Password {
		return errors.New("incorrect password")
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.StandardClaims{ // Make a new token
		ExpiresAt: time.Now().Add(TokenLifespan).Unix(),
	})
	var err error
	reply.Token, err = token.SignedString([]byte(s.Password))
	return err
}

// ChangePasswordArgs ...
type ChangePasswordArgs struct {
	OldPassword string `json:"oldPassword"` // Current authorization password
	NewPassword string `json:"newPassword"` // New authorization password
}

// ChangePassword ...
func (s *Service) ChangePassword(_ *http.Request, args *ChangePasswordArgs, reply *Success) error {
	s.log.Info("Auth: ChangePassword called")
	s.lock.Lock()
	defer s.lock.Unlock()
	if args.OldPassword != s.Password {
		return errors.New("incorrect password")
	} else if len(args.NewPassword) == 0 {
		return errors.New("new password can't be empty")
	}
	s.Password = args.NewPassword
	reply.Success = true
	return nil
}
