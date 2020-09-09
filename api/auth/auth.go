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

	"github.com/ava-labs/avalanche-go/utils/password"
	"github.com/ava-labs/avalanche-go/utils/timer"
)

const (
	// Endpoint is the base of the auth URL
	Endpoint = "auth"

	headerKey      = "Authorization"
	headerValStart = "Bearer "
)

var (
	// TokenLifespan is how long a token lives before it expires
	TokenLifespan = time.Hour * 12

	// ErrNoToken is returned by GetToken if no token is provided
	ErrNoToken = errors.New("auth token not provided")

	errWrongPassword      = errors.New("incorrect password")
	errInvalidTokenFormat = errors.New("token is invalid format")
	errSamePassword       = errors.New("new password can't be same as old password")
)

// Auth handles HTTP API authorization for this node
type Auth struct {
	Enabled  bool          // True iff API calls need auth token
	Password password.Hash // Hash of the password. Can be changed via API call.

	lock    sync.RWMutex // Prevent race condition when accessing password
	clock   timer.Clock  // Tells the time. Can be faked for testing
	revoked []string     // List of tokens that have been revoked
}

// Custom claim type used for API access token
type endpointClaims struct {
	jwt.StandardClaims

	// Each element is an endpoint that the token allows access to
	// If endpoints has an element "*", allows access to all API endpoints
	// In this case, "*" should be the only element of [endpoints]
	Endpoints []string
}

// getTokenKey returns the key to use when making and parsing tokens
func (auth *Auth) getTokenKey(*jwt.Token) (interface{}, error) {
	return auth.Password.Password[:], nil
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
		return "", errInvalidTokenFormat
	}
	return rawHeader[len(headerValStart):], nil // Returns actual auth token. Slice guaranteed to not go OOB
}

// Create and return a new token that allows access to each API endpoint such
// that the API's path ends with an element of [endpoints]
// If one of the elements of [endpoints] is "*", allows access to all APIs
func (auth *Auth) newToken(password string, endpoints []string) (string, error) {
	auth.lock.RLock()
	defer auth.lock.RUnlock()
	if !auth.Password.Check(password) {
		return "", errWrongPassword
	}
	canAccessAll := false
	for _, endpoint := range endpoints {
		if endpoint == "*" {
			canAccessAll = true
			break
		}
	}
	claims := endpointClaims{
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: auth.clock.Time().Add(TokenLifespan).Unix(),
		},
	}
	if canAccessAll {
		claims.Endpoints = []string{"*"}
	} else {
		claims.Endpoints = endpoints
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(auth.Password.Password[:]) // Sign the token and return its string repr.

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
	if !auth.Password.Check(password) {
		return errWrongPassword
	}

	// See if token is well-formed and signature is right
	token, err := jwt.Parse(tokenStr, auth.getTokenKey)
	if err != nil {
		return err
	}

	// Only need to revoke if the token is valid
	if token.Valid {
		auth.revoked = append(auth.revoked, tokenStr)
	}
	return nil
}

// Change the password required to create and revoke tokens.
// [oldPassword] is the current password.
// [newPassword] is the new password. It can't be the empty string and it can't
//               be unreasonably long.
// Changing the password makes tokens issued under a previous password invalid.
func (auth *Auth) changePassword(oldPassword, newPassword string) error {
	if oldPassword == newPassword {
		return errSamePassword
	}

	auth.lock.Lock()
	defer auth.lock.Unlock()

	if !auth.Password.Check(oldPassword) {
		return errWrongPassword
	}
	if err := password.IsValid(newPassword, password.OK); err != nil {
		return err
	}
	if err := auth.Password.Set(newPassword); err != nil {
		return err
	}

	// All the revoked tokens are now invalid; no need to mark specifically as
	// revoked.
	auth.revoked = nil
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
			// Error is intentionally dropped here as there is nothing left to
			// do with it.
			_, _ = io.WriteString(w, err.Error())
			return
		} else if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			// Error is intentionally dropped here as there is nothing left to
			// do with it.
			_, _ = io.WriteString(w, "couldn't parse auth token. Header \"Authorization\" should be \"Bearer TOKEN.GOES.HERE\"")
			return
		}

		auth.lock.RLock()
		token, err := jwt.ParseWithClaims(tokenStr, &endpointClaims{}, auth.getTokenKey)
		auth.lock.RUnlock()

		if err != nil { // Probably because signature wrong
			w.WriteHeader(http.StatusUnauthorized)
			// Error is intentionally dropped here as there is nothing left to
			// do with it.
			_, _ = io.WriteString(w, fmt.Sprintf("invalid auth token: %s", err))
			return
		}
		if !token.Valid { // Check that token isn't expired
			w.WriteHeader(http.StatusUnauthorized)
			// Error is intentionally dropped here as there is nothing left to
			// do with it.
			_, _ = io.WriteString(w, "invalid auth token. Is it expired?")
			return
		}

		// Make sure this token gives access to the requested endpoint
		claims, ok := token.Claims.(*endpointClaims)
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			// Error is intentionally dropped here as there is nothing left to
			// do with it.
			_, _ = io.WriteString(w, "expected auth token's claims to be type endpointClaims but is different type")
			return
		}
		canAccess := false // true iff the token authorizes access to the API
		for _, endpoint := range claims.Endpoints {
			if endpoint == "*" || strings.HasSuffix(r.URL.Path, endpoint) {
				canAccess = true
				break
			}
		}
		if !canAccess {
			w.WriteHeader(http.StatusUnauthorized)
			// Error is intentionally dropped here as there is nothing left to
			// do with it.
			_, _ = io.WriteString(w, "the provided auth token does not allow access to this endpoint")
			return
		}

		auth.lock.RLock()
		for _, revokedToken := range auth.revoked { // Make sure this token wasn't revoked
			if revokedToken == tokenStr {
				w.WriteHeader(http.StatusUnauthorized)
				// Error is intentionally dropped here as there is nothing left
				// to do with it.
				_, _ = io.WriteString(w, "the provided auth token was revoked")
				auth.lock.RUnlock()
				return
			}
		}
		auth.lock.RUnlock()

		h.ServeHTTP(w, r) // Authorization successful
	})
}
