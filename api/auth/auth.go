package auth

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	headerKey      = "Authorization"
	headerValStart = "Bearer "
)

var (
	// TokenLifespan is how long a token lives before it expires
	TokenLifespan = 1 * time.Minute

	// ErrNoToken is returned by GetToken if no token is provided
	ErrNoToken = errors.New("auth token not provided")
)

// TODO: Add method to revoke a token

// Auth ...
type Auth struct {
	lock     sync.RWMutex
	Password string
}

// GetToken gets the JWT token from the request header
// Assumes the header is this form:
// "Authorization": "Bearer TOKEN.GOES.HERE"
func GetToken(r *http.Request) (string, error) {
	rawHeader := r.Header.Get("Authorization") // Should be "Bearer AUTH.TOKEN.HERE"
	if rawHeader == "" {
		return "", ErrNoToken
	}
	if !strings.HasPrefix(rawHeader, headerValStart) {
		return "", errors.New("token is invalid format")
	}
	return rawHeader[len(headerValStart):], nil // Should be the actual auth token. Slice guaranteed to not go OOB
}
