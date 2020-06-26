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
	Endpoint       = "auth"
	maxPasswordLen = 1024
)

var (
	// TokenLifespan is how long a token lives before it expires
	TokenLifespan = time.Hour * 12

	// ErrNoToken is returned by GetToken if no token is provided
	ErrNoToken = errors.New("auth token not provided")

	errWrongPassword = errors.New("incorrect password")
)

// Auth handles HTTP API authorization for this node
type Auth struct {
	lock     sync.RWMutex // Prevent race condition when accessing password
	Enabled  bool         // True iff API calls need auth token
	Password string       // The password. Can be changed via API call.
	revoked  []string     // List of tokens that have been revoked
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

// Create and return a new token
func (auth *Auth) newToken(password string) (string, error) {
	auth.lock.RLock()
	defer auth.lock.RUnlock()
	if password != auth.Password {
		return "", errWrongPassword
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.StandardClaims{ // Make a new token
		ExpiresAt: time.Now().Add(TokenLifespan).Unix(),
	})
	return token.SignedString([]byte(auth.Password)) // Sign it

}

// Revokes the token whose string repr. is [tokenStr]; it will not be accepted as authorization for future API calls.
// If the token is invalid, this is a no-op.
// Only currently valid tokens can be revoked
// If a token is revoked and then the password is changed, and then changed back to the current password,
// the token will be un-revoked. Don't re-use passwords before at least TokenLifespan has elapsed.
// Returns an error if the wrong password is given
func (auth *Auth) revokeToken(tokenStr string, password string) error {
	auth.lock.Lock()
	defer auth.lock.Unlock()
	if auth.Password != password {
		return errWrongPassword
	}

	token, err := jwt.Parse(tokenStr, func(*jwt.Token) (interface{}, error) { // See if token is well-formed and signature is right
		return []byte(auth.Password), nil
	})
	if err == nil && token.Valid { // Only need to revoke if the token is valid
		auth.revoked = append(auth.revoked, tokenStr)
	}
	return nil
}

// Change the password required to generate new tokens and to revoke tokens
// [oldPassword] is the current password
// [newPassword] is the new password. It can't be the empty string and it can't have length > maxPasswordLen.
// Changing the password makes tokens issued under a previous password invalid
func (auth *Auth) changePassword(oldPassword, newPassword string) error {
	auth.lock.Lock()
	defer auth.lock.Unlock()
	if auth.Password != oldPassword {
		return errWrongPassword
	} else if len(newPassword) == 0 || len(newPassword) > maxPasswordLen {
		return fmt.Errorf("password length exceeds maximum length, %d", maxPasswordLen)
	} else if oldPassword == newPassword {
		return errors.New("new password can't be same as old password")
	}
	auth.Password = newPassword
	auth.revoked = []string{} // All the revoked tokens are now invalid; no need to mark specifically as revoked
	return nil
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
		for _, revokedToken := range auth.revoked { // Make sure this token wasn't revoked
			if revokedToken == tokenStr {
				w.WriteHeader(http.StatusUnauthorized)
				io.WriteString(w, "this token was revoked")
				return
			}
		}

		h.ServeHTTP(w, r) // Authentication successful
	})
}
