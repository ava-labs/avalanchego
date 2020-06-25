package auth

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	jwt "github.com/dgrijalva/jwt-go"
)

// TODO: Add method to revoke a token

const (
	headerKey      = "Authorization"
	headerValStart = "Bearer "
	// Endpoint is the base of the auth URL
	Endpoint = "auth"
)

var (
	// TokenLifespan is how long a token lives before it expires
	TokenLifespan = time.Hour * 12

	// ErrNoToken is returned by GetToken if no token is provided
	ErrNoToken = errors.New("auth token not provided")
)

// Auth handles HTTP API authorization for this node
type Auth struct {
	lock     sync.RWMutex // Prevent race condition when accessing password
	Enabled  bool         // True iff API calls need auth token
	Password string       // The password. Can be changed via API call.
}

// getToken gets the JWT token from the request header
// Assumes the header is this form:
// "Authorization": "Bearer TOKEN.GOES.HERE"
func getToken(r *http.Request) (string, error) {
	rawHeader := r.Header.Get("Authorization") // Should be "Bearer AUTH.TOKEN.HERE"
	if rawHeader == "" {
		return "", ErrNoToken
	}
	if !strings.HasPrefix(rawHeader, headerValStart) {
		return "", errors.New("token is invalid format")
	}
	return rawHeader[len(headerValStart):], nil // Returns actual auth token. Slice guaranteed to not go OOB
}

// WrapHandler wraps a handler. Before passing a request to the handler, check that
// an auth token was provided (if necessary) and that it is valid/unexpired.
func (auth *Auth) WrapHandler(h http.Handler) http.Handler {
	if !auth.Enabled { // Auth tokens aren't in use. Do nothing.
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if path.Base(r.URL.Path) == Endpoint { // Don't require auth token to hit auth endpoint
			h.ServeHTTP(w, r)
			return
		}

		tokenStr, err := getToken(r) // Get the token from the header
		if err == ErrNoToken {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, err.Error())
			return
		} else if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, "couldn't parse auth token. Header \"Authorization\" should be \"Bearer TOKEN.GOES.HERE\"")
			return
		}

		token, err := jwt.Parse(tokenStr, func(*jwt.Token) (interface{}, error) { // See if token is well-formed and signature is right
			auth.lock.RLock()
			defer auth.lock.RUnlock()
			return []byte(auth.Password), nil
		})
		if err != nil { // Signature is probably wrong
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, fmt.Sprintf("invalid auth token: %s", err))
			return
		}
		if !token.Valid { // Check that token isn't expired
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, "invalid auth token. Is it expired?")
			return
		}

		h.ServeHTTP(w, r) // Authentication successful
	})
}
