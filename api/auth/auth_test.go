// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v4"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/password"
)

var (
	testPassword              = "password!@#$%$#@!"
	hashedPassword            = password.Hash{}
	unAuthorizedResponseRegex = `^{"jsonrpc":"2.0","error":{"code":-32600,"message":"(.*)"},"id":1}`
	errTest                   = errors.New("non-nil error")
)

func init() {
	if err := hashedPassword.Set(testPassword); err != nil {
		panic(err)
	}
}

// Always returns 200 (http.StatusOK)
var dummyHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

func TestNewTokenWrongPassword(t *testing.T) {
	require := require.New(t)

	auth := NewFromHash(logging.NoLog{}, "auth", hashedPassword)

	_, err := auth.NewToken("", defaultTokenLifespan, []string{"endpoint1, endpoint2"})
	require.ErrorIs(err, password.ErrEmptyPassword)

	_, err = auth.NewToken("notThePassword", defaultTokenLifespan, []string{"endpoint1, endpoint2"})
	require.ErrorIs(err, errWrongPassword)
}

func TestNewTokenHappyPath(t *testing.T) {
	require := require.New(t)

	auth := NewFromHash(logging.NoLog{}, "auth", hashedPassword).(*auth)

	now := time.Now()
	auth.clock.Set(now)

	// Make a token
	endpoints := []string{"endpoint1", "endpoint2", "endpoint3"}
	tokenStr, err := auth.NewToken(testPassword, defaultTokenLifespan, endpoints)
	require.NoError(err)

	// Parse the token
	token, err := jwt.ParseWithClaims(tokenStr, &endpointClaims{}, func(*jwt.Token) (interface{}, error) {
		auth.lock.RLock()
		defer auth.lock.RUnlock()
		return auth.password.Password[:], nil
	})
	require.NoError(err)

	require.IsType(&endpointClaims{}, token.Claims)
	claims := token.Claims.(*endpointClaims)
	require.Equal(endpoints, claims.Endpoints)

	shouldExpireAt := jwt.NewNumericDate(now.Add(defaultTokenLifespan))
	require.Equal(shouldExpireAt, claims.ExpiresAt)
}

func TestTokenHasWrongSig(t *testing.T) {
	require := require.New(t)

	auth := NewFromHash(logging.NoLog{}, "auth", hashedPassword).(*auth)

	// Make a token
	endpoints := []string{"endpoint1", "endpoint2", "endpoint3"}
	tokenStr, err := auth.NewToken(testPassword, defaultTokenLifespan, endpoints)
	require.NoError(err)

	// Try to parse the token using the wrong password
	_, err = jwt.ParseWithClaims(tokenStr, &endpointClaims{}, func(*jwt.Token) (interface{}, error) {
		auth.lock.RLock()
		defer auth.lock.RUnlock()
		return []byte(""), nil
	})
	require.ErrorIs(err, jwt.ErrSignatureInvalid)

	// Try to parse the token using the wrong password
	_, err = jwt.ParseWithClaims(tokenStr, &endpointClaims{}, func(*jwt.Token) (interface{}, error) {
		auth.lock.RLock()
		defer auth.lock.RUnlock()
		return []byte("notThePassword"), nil
	})
	require.ErrorIs(err, jwt.ErrSignatureInvalid)
}

func TestChangePassword(t *testing.T) {
	require := require.New(t)

	auth := NewFromHash(logging.NoLog{}, "auth", hashedPassword).(*auth)

	password2 := "fejhkefjhefjhefhje" // #nosec G101
	var err error

	err = auth.ChangePassword("", password2)
	require.ErrorIs(err, errWrongPassword)

	err = auth.ChangePassword("notThePassword", password2)
	require.ErrorIs(err, errWrongPassword)

	err = auth.ChangePassword(testPassword, "")
	require.ErrorIs(err, password.ErrEmptyPassword)

	require.NoError(auth.ChangePassword(testPassword, password2))
	require.True(auth.password.Check(password2))

	password3 := "ufwhwohwfohawfhwdwd" // #nosec G101

	err = auth.ChangePassword(testPassword, password3)
	require.ErrorIs(err, errWrongPassword)

	require.NoError(auth.ChangePassword(password2, password3))
}

func TestRevokeToken(t *testing.T) {
	require := require.New(t)

	auth := NewFromHash(logging.NoLog{}, "auth", hashedPassword).(*auth)

	// Make a token
	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics"}
	tokenStr, err := auth.NewToken(testPassword, defaultTokenLifespan, endpoints)
	require.NoError(err)

	require.NoError(auth.RevokeToken(tokenStr, testPassword))
	require.Len(auth.revoked, 1)
}

func TestWrapHandlerHappyPath(t *testing.T) {
	require := require.New(t)

	auth := NewFromHash(logging.NoLog{}, "auth", hashedPassword)

	// Make a token
	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics"}
	tokenStr, err := auth.NewToken(testPassword, defaultTokenLifespan, endpoints)
	require.NoError(err)

	wrappedHandler := auth.WrapHandler(dummyHandler)

	for _, endpoint := range endpoints {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9650"+endpoint, strings.NewReader(""))
		req.Header.Add("Authorization", "Bearer "+tokenStr)
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		require.Equal(http.StatusOK, rr.Code)
	}
}

func TestWrapHandlerRevokedToken(t *testing.T) {
	require := require.New(t)

	auth := NewFromHash(logging.NoLog{}, "auth", hashedPassword)

	// Make a token
	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics"}
	tokenStr, err := auth.NewToken(testPassword, defaultTokenLifespan, endpoints)
	require.NoError(err)

	require.NoError(auth.RevokeToken(tokenStr, testPassword))

	wrappedHandler := auth.WrapHandler(dummyHandler)

	for _, endpoint := range endpoints {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9650"+endpoint, strings.NewReader(""))
		req.Header.Add("Authorization", "Bearer "+tokenStr)
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		require.Equal(http.StatusUnauthorized, rr.Code)
		require.Contains(rr.Body.String(), errTokenRevoked.Error())
		require.Regexp(unAuthorizedResponseRegex, rr.Body.String())
	}
}

func TestWrapHandlerExpiredToken(t *testing.T) {
	require := require.New(t)

	auth := NewFromHash(logging.NoLog{}, "auth", hashedPassword).(*auth)

	auth.clock.Set(time.Now().Add(-2 * defaultTokenLifespan))

	// Make a token that expired well in the past
	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics"}
	tokenStr, err := auth.NewToken(testPassword, defaultTokenLifespan, endpoints)
	require.NoError(err)

	wrappedHandler := auth.WrapHandler(dummyHandler)

	for _, endpoint := range endpoints {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9650"+endpoint, strings.NewReader(""))
		req.Header.Add("Authorization", "Bearer "+tokenStr)
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		require.Equal(http.StatusUnauthorized, rr.Code)
		require.Contains(rr.Body.String(), "expired")
		require.Regexp(unAuthorizedResponseRegex, rr.Body.String())
	}
}

func TestWrapHandlerNoAuthToken(t *testing.T) {
	require := require.New(t)

	auth := NewFromHash(logging.NoLog{}, "auth", hashedPassword)

	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics"}
	wrappedHandler := auth.WrapHandler(dummyHandler)
	for _, endpoint := range endpoints {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:9650%s", endpoint), strings.NewReader(""))
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		require.Equal(http.StatusUnauthorized, rr.Code)
		require.Contains(rr.Body.String(), errNoToken.Error())
		require.Regexp(unAuthorizedResponseRegex, rr.Body.String())
	}
}

func TestWrapHandlerUnauthorizedEndpoint(t *testing.T) {
	require := require.New(t)

	auth := NewFromHash(logging.NoLog{}, "auth", hashedPassword)

	// Make a token
	endpoints := []string{"/ext/info"}
	tokenStr, err := auth.NewToken(testPassword, defaultTokenLifespan, endpoints)
	require.NoError(err)

	unauthorizedEndpoints := []string{"/ext/bc/X", "/ext/metrics", "", "/foo", "/ext/info/foo"}

	wrappedHandler := auth.WrapHandler(dummyHandler)
	for _, endpoint := range unauthorizedEndpoints {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9650"+endpoint, strings.NewReader(""))
		req.Header.Add("Authorization", "Bearer "+tokenStr)
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		require.Equal(http.StatusUnauthorized, rr.Code)
		require.Contains(rr.Body.String(), errTokenInsufficientPermission.Error())
		require.Regexp(unAuthorizedResponseRegex, rr.Body.String())
	}
}

func TestWrapHandlerAuthEndpoint(t *testing.T) {
	require := require.New(t)

	auth := NewFromHash(logging.NoLog{}, "auth", hashedPassword)

	// Make a token
	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics", "", "/foo", "/ext/info/foo"}
	tokenStr, err := auth.NewToken(testPassword, defaultTokenLifespan, endpoints)
	require.NoError(err)

	wrappedHandler := auth.WrapHandler(dummyHandler)
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9650/ext/auth", strings.NewReader(""))
	req.Header.Add("Authorization", "Bearer "+tokenStr)
	rr := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(rr, req)
	require.Equal(http.StatusOK, rr.Code)
}

func TestWrapHandlerAccessAll(t *testing.T) {
	require := require.New(t)

	auth := NewFromHash(logging.NoLog{}, "auth", hashedPassword)

	// Make a token that allows access to all endpoints
	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics", "", "/foo", "/ext/foo/info"}
	tokenStr, err := auth.NewToken(testPassword, defaultTokenLifespan, []string{"*"})
	require.NoError(err)

	wrappedHandler := auth.WrapHandler(dummyHandler)
	for _, endpoint := range endpoints {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9650"+endpoint, strings.NewReader(""))
		req.Header.Add("Authorization", "Bearer "+tokenStr)
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		require.Equal(http.StatusOK, rr.Code)
	}
}

func TestWriteUnauthorizedResponse(t *testing.T) {
	require := require.New(t)

	rr := httptest.NewRecorder()
	writeUnauthorizedResponse(rr, errTest)
	require.Equal(http.StatusUnauthorized, rr.Code)
	require.Equal(`{"jsonrpc":"2.0","error":{"code":-32600,"message":"non-nil error"},"id":1}`+"\n", rr.Body.String())
}

func TestWrapHandlerMutatedRevokedToken(t *testing.T) {
	require := require.New(t)

	auth := NewFromHash(logging.NoLog{}, "auth", hashedPassword)

	// Make a token
	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics"}
	tokenStr, err := auth.NewToken(testPassword, defaultTokenLifespan, endpoints)
	require.NoError(err)

	require.NoError(auth.RevokeToken(tokenStr, testPassword))

	wrappedHandler := auth.WrapHandler(dummyHandler)

	for _, endpoint := range endpoints {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9650"+endpoint, strings.NewReader(""))
		req.Header.Add("Authorization", fmt.Sprintf("Bearer %s=", tokenStr)) // The appended = at the end looks like padding
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		require.Equal(http.StatusUnauthorized, rr.Code)
	}
}

func TestWrapHandlerInvalidSigningMethod(t *testing.T) {
	require := require.New(t)

	auth := NewFromHash(logging.NoLog{}, "auth", hashedPassword).(*auth)

	// Make a token
	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics"}
	idBytes := [tokenIDByteLen]byte{}
	_, err := rand.Read(idBytes[:])
	require.NoError(err)
	id := base64.RawURLEncoding.EncodeToString(idBytes[:])

	claims := endpointClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(auth.clock.Time().Add(defaultTokenLifespan)),
			ID:        id,
		},
		Endpoints: endpoints,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS512, &claims)
	tokenStr, err := token.SignedString(auth.password.Password[:])
	require.NoError(err)

	wrappedHandler := auth.WrapHandler(dummyHandler)

	for _, endpoint := range endpoints {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9650"+endpoint, strings.NewReader(""))
		req.Header.Add("Authorization", "Bearer "+tokenStr)
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		require.Equal(http.StatusUnauthorized, rr.Code)
		require.Contains(rr.Body.String(), errInvalidSigningMethod.Error())
		require.Regexp(unAuthorizedResponseRegex, rr.Body.String())
	}
}
