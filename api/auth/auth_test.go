package auth

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	jwt "github.com/dgrijalva/jwt-go"

	"github.com/ava-labs/avalanchego/utils/password"
)

var (
	testPassword              = "password!@#$%$#@!"
	hashedPassword            = password.Hash{}
	unAuthorizedResponseRegex = "^{\"jsonrpc\":\"2.0\",\"error\":{\"code\":-32600,\"message\":\"(.*)\"},\"id\":1}$"
)

func init() {
	if err := hashedPassword.Set(testPassword); err != nil {
		panic(err)
	}
}

var (
	// Always returns 200 (http.StatusOK)
	dummyHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
)

func TestNewTokenWrongPassword(t *testing.T) {
	auth := NewFromHash(true, hashedPassword)

	_, err := auth.newToken("", []string{"endpoint1, endpoint2"})
	assert.Error(t, err, "should have failed because password is wrong")

	_, err = auth.newToken("notThePassword", []string{"endpoint1, endpoint2"})
	assert.Error(t, err, "should have failed because password is wrong")
}

func TestNewTokenHappyPath(t *testing.T) {
	auth := NewFromHash(true, hashedPassword)

	now := time.Now()
	auth.clock.Set(now)

	// Make a token
	endpoints := []string{"endpoint1", "endpoint2", "endpoint3"}
	tokenStr, err := auth.newToken(testPassword, endpoints)
	assert.NoError(t, err)

	// Parse the token
	token, err := jwt.ParseWithClaims(tokenStr, &endpointClaims{}, func(*jwt.Token) (interface{}, error) {
		auth.lock.RLock()
		defer auth.lock.RUnlock()
		return auth.password.Password[:], nil
	})
	assert.NoError(t, err, "couldn't parse new token")

	claims, ok := token.Claims.(*endpointClaims)
	assert.True(t, ok, "expected auth token's claims to be type endpointClaims but is different type")
	assert.ElementsMatch(t, endpoints, claims.Endpoints, "token has wrong endpoint claims")

	shouldExpireAt := now.Add(TokenLifespan).Unix()
	assert.Equal(t, shouldExpireAt, claims.ExpiresAt, "token expiration time is wrong")
}

func TestTokenHasWrongSig(t *testing.T) {
	auth := NewFromHash(true, hashedPassword)

	// Make a token
	endpoints := []string{"endpoint1", "endpoint2", "endpoint3"}
	tokenStr, err := auth.newToken(testPassword, endpoints)
	assert.NoError(t, err)

	// Try to parse the token using the wrong password
	_, err = jwt.ParseWithClaims(tokenStr, &endpointClaims{}, func(*jwt.Token) (interface{}, error) {
		auth.lock.RLock()
		defer auth.lock.RUnlock()
		return []byte(""), nil
	})
	assert.Error(t, err, "should have failed because password is wrong")

	// Try to parse the token using the wrong password
	_, err = jwt.ParseWithClaims(tokenStr, &endpointClaims{}, func(*jwt.Token) (interface{}, error) {
		auth.lock.RLock()
		defer auth.lock.RUnlock()
		return []byte("notThePassword"), nil
	})
	assert.Error(t, err, "should have failed because password is wrong")
}

func TestChangePassword(t *testing.T) {
	auth := NewFromHash(true, hashedPassword)

	password2 := "fejhkefjhefjhefhje" // #nosec G101
	var err error

	err = auth.changePassword("", password2)
	assert.Error(t, err, "should have failed because old password is wrong")

	err = auth.changePassword("notThePassword", password2)
	assert.Error(t, err, "should have failed because old password is wrong")

	err = auth.changePassword(testPassword, "")
	assert.Error(t, err, "should have failed because new password is empty")

	err = auth.changePassword(testPassword, password2)
	assert.NoError(t, err, "should have succeeded")
	assert.True(t, auth.password.Check(password2), "password should have been changed")

	password3 := "ufwhwohwfohawfhwdwd" // #nosec G101

	err = auth.changePassword(testPassword, password3)
	assert.Error(t, err, "should have failed because old password is wrong")

	err = auth.changePassword(password2, password3)
	assert.NoError(t, err, "should have succeeded")
}

func TestRevokeToken(t *testing.T) {
	auth := NewFromHash(true, hashedPassword)

	// Make a token
	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics"}
	tokenStr, err := auth.newToken(testPassword, endpoints)
	assert.NoError(t, err)

	err = auth.revokeToken(tokenStr, testPassword)
	assert.NoError(t, err, "should have succeeded")
	assert.Len(t, auth.revoked, 1, "revoked token list is incorrect")
}

func TestWrapHandlerHappyPath(t *testing.T) {
	auth := NewFromHash(true, hashedPassword)

	// Make a token
	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics"}
	tokenStr, err := auth.newToken(testPassword, endpoints)
	assert.NoError(t, err)

	wrappedHandler := auth.WrapHandler(dummyHandler)

	for _, endpoint := range endpoints {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:9650%s", endpoint), strings.NewReader(""))
		req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", tokenStr))
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
	}
}

func TestWrapHandlerRevokedToken(t *testing.T) {
	auth := NewFromHash(true, hashedPassword)

	// Make a token
	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics"}
	tokenStr, err := auth.newToken(testPassword, endpoints)
	assert.NoError(t, err)

	err = auth.revokeToken(tokenStr, testPassword)
	assert.NoError(t, err)

	wrappedHandler := auth.WrapHandler(dummyHandler)

	for _, endpoint := range endpoints {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:9650%s", endpoint), strings.NewReader(""))
		req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", tokenStr))
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusUnauthorized, rr.Code)
		assert.Contains(t, rr.Body.String(), ErrTokenRevoked.Error())
		assert.Regexp(t, unAuthorizedResponseRegex, rr.Body.String())
	}
}

func TestWrapHandlerExpiredToken(t *testing.T) {
	auth := NewFromHash(true, hashedPassword)

	auth.clock.Set(time.Now().Add(-2 * TokenLifespan))

	// Make a token that expired well in the past
	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics"}
	tokenStr, err := auth.newToken(testPassword, endpoints)
	assert.NoError(t, err)

	wrappedHandler := auth.WrapHandler(dummyHandler)

	for _, endpoint := range endpoints {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:9650%s", endpoint), strings.NewReader(""))
		req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", tokenStr))
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusUnauthorized, rr.Code)
		assert.Contains(t, rr.Body.String(), ErrTokenExpired.Error())
		assert.Regexp(t, unAuthorizedResponseRegex, rr.Body.String())
	}
}

func TestWrapHandlerNoAuthToken(t *testing.T) {
	auth := NewFromHash(true, hashedPassword)

	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics"}
	wrappedHandler := auth.WrapHandler(dummyHandler)
	for _, endpoint := range endpoints {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:9650%s", endpoint), strings.NewReader(""))
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusUnauthorized, rr.Code)
		assert.Contains(t, rr.Body.String(), ErrNoToken.Error())
		assert.Regexp(t, unAuthorizedResponseRegex, rr.Body.String())
	}
}

func TestWrapHandlerUnauthorizedEndpoint(t *testing.T) {
	auth := NewFromHash(true, hashedPassword)

	// Make a token
	endpoints := []string{"/ext/info"}
	tokenStr, err := auth.newToken(testPassword, endpoints)
	assert.NoError(t, err)

	unauthorizedEndpoints := []string{"/ext/bc/X", "/ext/metrics", "", "/foo", "/ext/info/foo"}

	wrappedHandler := auth.WrapHandler(dummyHandler)
	for _, endpoint := range unauthorizedEndpoints {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:9650%s", endpoint), strings.NewReader(""))
		req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", tokenStr))
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusUnauthorized, rr.Code)
		assert.Contains(t, rr.Body.String(), ErrTokenInsufficientPermission.Error())
		assert.Regexp(t, unAuthorizedResponseRegex, rr.Body.String())
	}
}

func TestWrapHandlerAuthEndpoint(t *testing.T) {
	auth := NewFromHash(true, hashedPassword)

	// Make a token
	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics", "", "/foo", "/ext/info/foo"}
	tokenStr, err := auth.newToken(testPassword, endpoints)
	assert.NoError(t, err)

	wrappedHandler := auth.WrapHandler(dummyHandler)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:9650%s", fmt.Sprintf("/ext/%s", Endpoint)), strings.NewReader(""))
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", tokenStr))
	rr := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestWrapHandlerAccessAll(t *testing.T) {
	auth := NewFromHash(true, hashedPassword)

	// Make a token that allows access to all endpoints
	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics", "", "/foo", "/ext/foo/info"}
	tokenStr, err := auth.newToken(testPassword, []string{"*"})
	assert.NoError(t, err)

	wrappedHandler := auth.WrapHandler(dummyHandler)
	for _, endpoint := range endpoints {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:9650%s", endpoint), strings.NewReader(""))
		req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", tokenStr))
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
	}
}

func TestWrapHandlerAuthDisabled(t *testing.T) {
	auth := NewFromHash(false, hashedPassword)

	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics", "", "/foo", "/ext/foo/info", "/ext/auth"}

	wrappedHandler := auth.WrapHandler(dummyHandler)
	for _, endpoint := range endpoints {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:9650%s", endpoint), strings.NewReader(""))
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
	}
}

func TestWriteUnauthorizedResponse(t *testing.T) {
	rr := httptest.NewRecorder()
	writeUnauthorizedResponse(rr, errors.New("example err"))
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Equal(t, "{\"jsonrpc\":\"2.0\",\"error\":{\"code\":-32600,\"message\":\"example err\"},\"id\":1}", rr.Body.String())
}

func TestWrapHandlerMutatedRevokedToken(t *testing.T) {
	auth := NewFromHash(true, hashedPassword)

	// Make a token
	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics"}
	tokenStr, err := auth.newToken(testPassword, endpoints)
	assert.NoError(t, err)

	err = auth.revokeToken(tokenStr, testPassword)
	assert.NoError(t, err)

	wrappedHandler := auth.WrapHandler(dummyHandler)

	for _, endpoint := range endpoints {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:9650%s", endpoint), strings.NewReader(""))
		req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", tokenStr+"="))
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusUnauthorized, rr.Code)
		assert.Contains(t, rr.Body.String(), ErrTokenRevoked.Error())
		assert.Regexp(t, unAuthorizedResponseRegex, rr.Body.String())
	}
}
