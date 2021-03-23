package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	jwt "github.com/dgrijalva/jwt-go"
	rpc "github.com/gorilla/rpc/v2/json2"

	"github.com/ava-labs/avalanchego/utils/password"
	"github.com/ava-labs/avalanchego/utils/timer"
)

const (
	// Endpoint is the base of the auth URL
	Endpoint = "auth"

	headerKey      = "Authorization"
	headerValStart = "Bearer "

	// number of bytes to use when generating a new random token ID
	tokenIDByteLen = 20
)

var (
	// TokenLifespan is how long a token lives before it expires
	TokenLifespan = time.Hour * 12

	// ErrNoToken is returned by GetToken if no token is provided
	ErrNoToken               = errors.New("auth token not provided")
	ErrAuthHeaderNotParsable = fmt.Errorf(
		"couldn't parse auth token. Header \"%s\" should be \"%sTOKEN.GOES.HERE\"",
		headerKey,
		headerValStart,
	)
	ErrInvalidSigningMethod        = fmt.Errorf("auth token didn't specify the HS256 signing method correctly")
	ErrTokenRevoked                = errors.New("the provided auth token was revoked")
	ErrTokenInsufficientPermission = errors.New("the provided auth token does not allow access to this endpoint")

	errWrongPassword = errors.New("incorrect password")
	errSamePassword  = errors.New("new password can't be same as old password")
)

// Auth handles HTTP API authorization for this node
type Auth struct {
	lock     sync.RWMutex        // Prevent race condition when accessing password
	enabled  bool                // True iff API calls need auth token
	password password.Hash       // Hash of the password. Can be changed via API call.
	clock    timer.Clock         // Tells the time. Can be faked for testing
	revoked  map[string]struct{} // Set of token IDs that have been revoked
}

func New(enabled bool, password string) (*Auth, error) {
	auth := &Auth{
		enabled: enabled,
		revoked: make(map[string]struct{}),
	}
	return auth, auth.password.Set(password)
}

func NewFromHash(enabled bool, password password.Hash) *Auth {
	return &Auth{
		enabled:  enabled,
		password: password,
		revoked:  make(map[string]struct{}),
	}
}

// Custom claim type used for API access token
type endpointClaims struct {
	jwt.StandardClaims

	// Each element is an endpoint that the token allows access to
	// If endpoints has an element "*", allows access to all API endpoints
	// In this case, "*" should be the only element of [endpoints]
	Endpoints []string `json:"endpoints,omitempty"`
}

// getTokenKey returns the key to use when making and parsing tokens
func (auth *Auth) getTokenKey(t *jwt.Token) (interface{}, error) {
	if t.Method != jwt.SigningMethodHS256 {
		return nil, ErrInvalidSigningMethod
	}
	return auth.password.Password[:], nil
}

// Create and return a new token that allows access to each API endpoint such
// that the API's path ends with an element of [endpoints]
// If one of the elements of [endpoints] is "*", allows access to all APIs
func (auth *Auth) newToken(password string, endpoints []string) (string, error) {
	auth.lock.RLock()
	defer auth.lock.RUnlock()

	if !auth.password.Check(password) {
		return "", errWrongPassword
	}

	canAccessAll := false
	for _, endpoint := range endpoints {
		if endpoint == "*" {
			canAccessAll = true
			break
		}
	}

	idBytes := [tokenIDByteLen]byte{}
	if _, err := rand.Read(idBytes[:]); err != nil {
		return "", fmt.Errorf("failed to generate the unique token ID due to %w", err)
	}
	id := base64.URLEncoding.EncodeToString(idBytes[:])

	claims := endpointClaims{
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: auth.clock.Time().Add(TokenLifespan).Unix(),
			Id:        id,
		},
	}
	if canAccessAll {
		claims.Endpoints = []string{"*"}
	} else {
		claims.Endpoints = endpoints
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, &claims)
	return token.SignedString(auth.password.Password[:]) // Sign the token and return its string repr.
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

	if !auth.password.Check(password) {
		return errWrongPassword
	}

	// See if token is well-formed and signature is right
	token, err := jwt.ParseWithClaims(tokenStr, &endpointClaims{}, auth.getTokenKey)
	if err != nil {
		return err
	}

	// If the token isn't valid, it has essentially already been revoked.
	if !token.Valid {
		return nil
	}

	claims, ok := token.Claims.(*endpointClaims)
	if !ok {
		return fmt.Errorf("expected auth token's claims to be type endpointClaims but is %T", token.Claims)
	}
	auth.revoked[claims.Id] = struct{}{}
	return nil
}

// Authenticates [tokenStr] for access to [url].
func (auth *Auth) authenticateToken(tokenStr, url string) error {
	auth.lock.RLock()
	defer auth.lock.RUnlock()

	token, err := jwt.ParseWithClaims(tokenStr, &endpointClaims{}, auth.getTokenKey)
	if err != nil { // Probably because signature wrong
		return err
	}

	// Make sure this token gives access to the requested endpoint
	claims, ok := token.Claims.(*endpointClaims)
	if !ok {
		// Error is intentionally dropped here as there is nothing left to do
		// with it.
		return fmt.Errorf("expected auth token's claims to be type endpointClaims but is %T", token.Claims)
	}

	_, revoked := auth.revoked[claims.Id]
	if revoked {
		return ErrTokenRevoked
	}

	for _, endpoint := range claims.Endpoints {
		if endpoint == "*" || strings.HasSuffix(url, endpoint) {
			return nil
		}
	}
	return ErrTokenInsufficientPermission
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

	if !auth.password.Check(oldPassword) {
		return errWrongPassword
	}
	if err := password.IsValid(newPassword, password.OK); err != nil {
		return err
	}
	if err := auth.password.Set(newPassword); err != nil {
		return err
	}

	// All the revoked tokens are now invalid; no need to mark specifically as
	// revoked.
	auth.revoked = make(map[string]struct{})
	return nil
}

// WrapHandler wraps a handler. Before passing a request to the handler, check that
// an auth token was provided (if necessary) and that it is valid/unexpired.
func (auth *Auth) WrapHandler(h http.Handler) http.Handler {
	if !auth.enabled { // Auth tokens aren't in use. Do nothing.
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't require auth token to hit auth endpoint
		if path.Base(r.URL.Path) == Endpoint {
			h.ServeHTTP(w, r)
			return
		}

		// Should be "Bearer AUTH.TOKEN.HERE"
		rawHeader := r.Header.Get(headerKey)
		if rawHeader == "" {
			writeUnauthorizedResponse(w, ErrNoToken)
			return
		}
		if !strings.HasPrefix(rawHeader, headerValStart) {
			// Error is intentionally dropped here as there is nothing left to
			// do with it.
			writeUnauthorizedResponse(w, ErrAuthHeaderNotParsable)
			return
		}
		// Returns actual auth token. Slice guaranteed to not go OOB
		tokenStr := rawHeader[len(headerValStart):]

		if err := auth.authenticateToken(tokenStr, r.URL.Path); err != nil {
			writeUnauthorizedResponse(w, err)
			return
		}

		h.ServeHTTP(w, r)
	})
}

// Write a JSON-RPC formatted response saying that the API call is unauthorized.
// The response has header http.StatusUnauthorized.
// Errors while marshalling or writing are ignored.
func writeUnauthorizedResponse(w http.ResponseWriter, err error) {
	body := struct {
		Version string `json:"jsonrpc"`
		Err     struct {
			Code    rpc.ErrorCode `json:"code"`
			Message string        `json:"message"`
		} `json:"error"`
		ID uint8 `json:"id"`
	}{}

	body.Version = rpc.Version
	body.Err.Code = rpc.E_INVALID_REQ
	body.Err.Message = err.Error()
	body.ID = 1

	encoded, _ := json.Marshal(body)

	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write(encoded)
}
