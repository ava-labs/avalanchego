// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
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
	unAuthorizedResponseRegex = "^{\"jsonrpc\":\"2.0\",\"error\":{\"code\":-32600,\"message\":\"(.*)\"},\"id\":1}"
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
	auth := NewFromHash(logging.NoLog{}, "auth", hashedPassword)

	_, err := auth.NewToken("", defaultTokenLifespan, []string{"endpoint1, endpoint2"})
	require.Error(t, err, "should have failed because password is wrong")

	_, err = auth.NewToken("notThePassword", defaultTokenLifespan, []string{"endpoint1, endpoint2"})
	require.Error(t, err, "should have failed because password is wrong")
}

func TestNewTokenHappyPath(t *testing.T) {
	auth := NewFromHash(logging.NoLog{}, "auth", hashedPassword).(*auth)

	now := time.Now()
	auth.clock.Set(now)

	// Make a token
	endpoints := []string{"endpoint1", "endpoint2", "endpoint3"}
	tokenStr, err := auth.NewToken(testPassword, defaultTokenLifespan, endpoints)
	require.NoError(t, err)

	// Parse the token
	token, err := jwt.ParseWithClaims(tokenStr, &endpointClaims{}, func(*jwt.Token) (interface{}, error) {
		auth.lock.RLock()
		defer auth.lock.RUnlock()
		return auth.password.Password[:], nil
	})
	require.NoError(t, err, "couldn't parse new token")

	claims, ok := token.Claims.(*endpointClaims)
	require.True(t, ok, "expected auth token's claims to be type endpointClaims but is different type")
	require.ElementsMatch(t, endpoints, claims.Endpoints, "token has wrong endpoint claims")

	shouldExpireAt := jwt.NewNumericDate(now.Add(defaultTokenLifespan))
	require.Equal(t, shouldExpireAt, claims.ExpiresAt, "token expiration time is wrong")
}

func TestTokenHasWrongSig(t *testing.T) {
	auth := NewFromHash(logging.NoLog{}, "auth", hashedPassword).(*auth)

	// Make a token
	endpoints := []string{"endpoint1", "endpoint2", "endpoint3"}
	tokenStr, err := auth.NewToken(testPassword, defaultTokenLifespan, endpoints)
	require.NoError(t, err)

	// Try to parse the token using the wrong password
	_, err = jwt.ParseWithClaims(tokenStr, &endpointClaims{}, func(*jwt.Token) (interface{}, error) {
		auth.lock.RLock()
		defer auth.lock.RUnlock()
		return []byte(""), nil
	})
	require.Error(t, err, "should have failed because password is wrong")

	// Try to parse the token using the wrong password
	_, err = jwt.ParseWithClaims(tokenStr, &endpointClaims{}, func(*jwt.Token) (interface{}, error) {
		auth.lock.RLock()
		defer auth.lock.RUnlock()
		return []byte("notThePassword"), nil
	})
	require.Error(t, err, "should have failed because password is wrong")
}

func TestChangePassword(t *testing.T) {
	auth := NewFromHash(logging.NoLog{}, "auth", hashedPassword).(*auth)

	password2 := "fejhkefjhefjhefhje" // #nosec G101
	var err error

	err = auth.ChangePassword("", password2)
	require.Error(t, err, "should have failed because old password is wrong")

	err = auth.ChangePassword("notThePassword", password2)
	require.Error(t, err, "should have failed because old password is wrong")

	err = auth.ChangePassword(testPassword, "")
	require.Error(t, err, "should have failed because new password is empty")

	err = auth.ChangePassword(testPassword, password2)
	require.NoError(t, err, "should have succeeded")
	require.True(t, auth.password.Check(password2), "password should have been changed")

	password3 := "ufwhwohwfohawfhwdwd" // #nosec G101

	err = auth.ChangePassword(testPassword, password3)
	require.Error(t, err, "should have failed because old password is wrong")

	err = auth.ChangePassword(password2, password3)
	require.NoError(t, err, "should have succeeded")
}

func TestRevokeToken(t *testing.T) {
	auth := NewFromHash(logging.NoLog{}, "auth", hashedPassword).(*auth)

	// Make a token
	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics"}
	tokenStr, err := auth.NewToken(testPassword, defaultTokenLifespan, endpoints)
	require.NoError(t, err)

	err = auth.RevokeToken(tokenStr, testPassword)
	require.NoError(t, err, "should have succeeded")
	require.Len(t, auth.revoked, 1, "revoked token list is incorrect")
}

func TestWrapHandlerHappyPath(t *testing.T) {
	auth := NewFromHash(logging.NoLog{}, "auth", hashedPassword)

	// Make a token
	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics"}
	tokenStr, err := auth.NewToken(testPassword, defaultTokenLifespan, endpoints)
	require.NoError(t, err)

	wrappedHandler := auth.WrapHandler(dummyHandler)

	for _, endpoint := range endpoints {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9650"+endpoint, strings.NewReader(""))
		req.Header.Add("Authorization", "Bearer "+tokenStr)
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)
	}
}

func TestWrapHandlerRevokedToken(t *testing.T) {
	auth := NewFromHash(logging.NoLog{}, "auth", hashedPassword)

	// Make a token
	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics"}
	tokenStr, err := auth.NewToken(testPassword, defaultTokenLifespan, endpoints)
	require.NoError(t, err)

	err = auth.RevokeToken(tokenStr, testPassword)
	require.NoError(t, err)

	wrappedHandler := auth.WrapHandler(dummyHandler)

	for _, endpoint := range endpoints {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9650"+endpoint, strings.NewReader(""))
		req.Header.Add("Authorization", "Bearer "+tokenStr)
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		require.Equal(t, http.StatusUnauthorized, rr.Code)
		require.Contains(t, rr.Body.String(), errTokenRevoked.Error())
		require.Regexp(t, unAuthorizedResponseRegex, rr.Body.String())
	}
}

func TestWrapHandlerExpiredToken(t *testing.T) {
	auth := NewFromHash(logging.NoLog{}, "auth", hashedPassword).(*auth)

	auth.clock.Set(time.Now().Add(-2 * defaultTokenLifespan))

	// Make a token that expired well in the past
	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics"}
	tokenStr, err := auth.NewToken(testPassword, defaultTokenLifespan, endpoints)
	require.NoError(t, err)

	wrappedHandler := auth.WrapHandler(dummyHandler)

	for _, endpoint := range endpoints {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9650"+endpoint, strings.NewReader(""))
		req.Header.Add("Authorization", "Bearer "+tokenStr)
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		require.Equal(t, http.StatusUnauthorized, rr.Code)
		require.Contains(t, rr.Body.String(), "expired")
		require.Regexp(t, unAuthorizedResponseRegex, rr.Body.String())
	}
}

func TestWrapHandlerNoAuthToken(t *testing.T) {
	auth := NewFromHash(logging.NoLog{}, "auth", hashedPassword)

	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics"}
	wrappedHandler := auth.WrapHandler(dummyHandler)
	for _, endpoint := range endpoints {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:9650%s", endpoint), strings.NewReader(""))
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		require.Equal(t, http.StatusUnauthorized, rr.Code)
		require.Contains(t, rr.Body.String(), errNoToken.Error())
		require.Regexp(t, unAuthorizedResponseRegex, rr.Body.String())
	}
}

func TestWrapHandlerUnauthorizedEndpoint(t *testing.T) {
	auth := NewFromHash(logging.NoLog{}, "auth", hashedPassword)

	// Make a token
	endpoints := []string{"/ext/info"}
	tokenStr, err := auth.NewToken(testPassword, defaultTokenLifespan, endpoints)
	require.NoError(t, err)

	unauthorizedEndpoints := []string{"/ext/bc/X", "/ext/metrics", "", "/foo", "/ext/info/foo"}

	wrappedHandler := auth.WrapHandler(dummyHandler)
	for _, endpoint := range unauthorizedEndpoints {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9650"+endpoint, strings.NewReader(""))
		req.Header.Add("Authorization", "Bearer "+tokenStr)
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		require.Equal(t, http.StatusUnauthorized, rr.Code)
		require.Contains(t, rr.Body.String(), errTokenInsufficientPermission.Error())
		require.Regexp(t, unAuthorizedResponseRegex, rr.Body.String())
	}
}

func TestWrapHandlerAuthEndpoint(t *testing.T) {
	auth := NewFromHash(logging.NoLog{}, "auth", hashedPassword)

	// Make a token
	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics", "", "/foo", "/ext/info/foo"}
	tokenStr, err := auth.NewToken(testPassword, defaultTokenLifespan, endpoints)
	require.NoError(t, err)

	wrappedHandler := auth.WrapHandler(dummyHandler)
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9650/ext/auth", strings.NewReader(""))
	req.Header.Add("Authorization", "Bearer "+tokenStr)
	rr := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
}

func TestWrapHandlerAccessAll(t *testing.T) {
	auth := NewFromHash(logging.NoLog{}, "auth", hashedPassword)

	// Make a token that allows access to all endpoints
	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics", "", "/foo", "/ext/foo/info"}
	tokenStr, err := auth.NewToken(testPassword, defaultTokenLifespan, []string{"*"})
	require.NoError(t, err)

	wrappedHandler := auth.WrapHandler(dummyHandler)
	for _, endpoint := range endpoints {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9650"+endpoint, strings.NewReader(""))
		req.Header.Add("Authorization", "Bearer "+tokenStr)
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)
	}
}

func TestWriteUnauthorizedResponse(t *testing.T) {
	rr := httptest.NewRecorder()
	writeUnauthorizedResponse(rr, errTest)
	require.Equal(t, http.StatusUnauthorized, rr.Code)
	require.Equal(t, "{\"jsonrpc\":\"2.0\",\"error\":{\"code\":-32600,\"message\":\"non-nil error\"},\"id\":1}\n", rr.Body.String())
}

func TestWrapHandlerMutatedRevokedToken(t *testing.T) {
	auth := NewFromHash(logging.NoLog{}, "auth", hashedPassword)

	// Make a token
	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics"}
	tokenStr, err := auth.NewToken(testPassword, defaultTokenLifespan, endpoints)
	require.NoError(t, err)

	err = auth.RevokeToken(tokenStr, testPassword)
	require.NoError(t, err)

	wrappedHandler := auth.WrapHandler(dummyHandler)

	for _, endpoint := range endpoints {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9650"+endpoint, strings.NewReader(""))
		req.Header.Add("Authorization", fmt.Sprintf("Bearer %s=", tokenStr)) // The appended = at the end looks like padding
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		require.Equal(t, http.StatusUnauthorized, rr.Code)
	}
}

func TestWrapHandlerInvalidSigningMethod(t *testing.T) {
	auth := NewFromHash(logging.NoLog{}, "auth", hashedPassword).(*auth)

	// Make a token
	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics"}
	idBytes := [tokenIDByteLen]byte{}
	if _, err := rand.Read(idBytes[:]); err != nil {
		t.Fatal(err)
	}
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
	if err != nil {
		t.Fatal(err)
	}

	wrappedHandler := auth.WrapHandler(dummyHandler)

	for _, endpoint := range endpoints {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9650"+endpoint, strings.NewReader(""))
		req.Header.Add("Authorization", "Bearer "+tokenStr)
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		require.Equal(t, http.StatusUnauthorized, rr.Code)
		require.Contains(t, rr.Body.String(), errInvalidSigningMethod.Error())
		require.Regexp(t, unAuthorizedResponseRegex, rr.Body.String())
	}
}
