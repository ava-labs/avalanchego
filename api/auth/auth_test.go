package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	jwt "github.com/dgrijalva/jwt-go"

	"github.com/ava-labs/avalanchego/utils/password"
)

var (
	testPassword   = "password!@#$%$#@!"
	hashedPassword = password.Hash{}
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
	auth := Auth{
		Enabled:  true,
		Password: hashedPassword,
	}
	if _, err := auth.newToken("", []string{"endpoint1, endpoint2"}); err == nil {
		t.Fatal("should have failed because password is wrong")
	} else if _, err := auth.newToken("notThePassword", []string{"endpoint1, endpoint2"}); err == nil {
		t.Fatal("should have failed because password is wrong")
	}
}

func TestNewTokenHappyPath(t *testing.T) {
	auth := Auth{
		Enabled:  true,
		Password: hashedPassword,
	}
	now := time.Now()
	auth.clock.Set(now)

	// Make a token
	endpoints := []string{"endpoint1", "endpoint2", "endpoint3"}
	tokenStr, err := auth.newToken(testPassword, endpoints)
	if err != nil {
		t.Fatal(err)
	}

	// Parse the token
	token, err := jwt.ParseWithClaims(tokenStr, &endpointClaims{}, func(*jwt.Token) (interface{}, error) {
		auth.lock.RLock()
		defer auth.lock.RUnlock()
		return auth.Password.Password[:], nil
	})
	if err != nil {
		t.Fatalf("couldn't parse new token: %s", err)
	}

	claims, ok := token.Claims.(*endpointClaims)
	if !ok {
		t.Fatal("expected auth token's claims to be type endpointClaims but is different type")
	}
	if !reflect.DeepEqual(claims.Endpoints, endpoints) {
		t.Fatal("token has wrong endpoint claims")
	}
	if shouldExpireAt := now.Add(TokenLifespan).Unix(); shouldExpireAt != now.Add(TokenLifespan).Unix() {
		t.Fatalf("token expiration time is wrong")
	}
}

func TestTokenHasWrongSig(t *testing.T) {
	auth := Auth{
		Enabled:  true,
		Password: hashedPassword,
	}

	// Make a token
	endpoints := []string{"endpoint1", "endpoint2", "endpoint3"}
	tokenStr, err := auth.newToken(testPassword, endpoints)
	if err != nil {
		t.Fatal(err)
	}

	// Try to parse the token using the wrong password
	if _, err := jwt.ParseWithClaims(tokenStr, &endpointClaims{}, func(*jwt.Token) (interface{}, error) {
		auth.lock.RLock()
		defer auth.lock.RUnlock()
		return []byte(""), nil
	}); err == nil {
		t.Fatalf("should have failed because password is wrong")
	}

	// Try to parse the token using the wrong password
	if _, err := jwt.ParseWithClaims(tokenStr, &endpointClaims{}, func(*jwt.Token) (interface{}, error) {
		auth.lock.RLock()
		defer auth.lock.RUnlock()
		return []byte("notThePassword"), nil
	}); err == nil {
		t.Fatalf("should have failed because password is wrong")
	}
}

func TestChangePassword(t *testing.T) {
	auth := Auth{
		Enabled:  true,
		Password: hashedPassword,
	}

	password2 := "fejhkefjhefjhefhje" // #nosec G101
	if err := auth.changePassword("", password2); err == nil {
		t.Fatal("should have failed because old password is wrong")
	} else if err := auth.changePassword("notThePassword", password2); err == nil {
		t.Fatal("should have failed because old password is wrong")
	} else if err := auth.changePassword(testPassword, ""); err == nil {
		t.Fatal("should have failed because new password is empty")
	} else if err := auth.changePassword(testPassword, password2); err != nil {
		t.Fatal("should have succeeded")
	}

	if !auth.Password.Check(password2) {
		t.Fatal("password should have been changed")
	}

	password3 := "ufwhwohwfohawfhwdwd" // #nosec G101
	if err := auth.changePassword(testPassword, password3); err == nil {
		t.Fatal("should have failed because old password is wrong")
	} else if err := auth.changePassword(password2, password3); err != nil {
		t.Fatal("should have succeeded")
	}

}

func TestGetToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9650/ext/auth", strings.NewReader(""))
	if _, err := getToken(req); err == nil {
		t.Fatal("should have failed because no auth token given")
	}

	req.Header.Add("Authorization", "")
	if _, err := getToken(req); err == nil {
		t.Fatal("should have failed because auth token invalid")
	}

	req.Header.Set("Authorization", "this isn't an auth token!")
	if _, err := getToken(req); err == nil {
		t.Fatal("should have failed because auth token invalid")
	}

	wellFormedToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJFbmRwb2ludHMiOlsiKiJdLCJleHAiOjE1OTM0NzU4OTR9.Cqo7TraN_CFN13q3ae4GRJCMgd8ZOlQwBzyC29M6Aps" // #nosec G101
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", wellFormedToken))
	if token, err := getToken(req); err != nil {
		t.Fatal("should have been able to parse valid header")
	} else if token != wellFormedToken {
		t.Fatal("parsed token incorrectly")
	}
}

func TestRevokeToken(t *testing.T) {
	auth := Auth{
		Enabled:  true,
		Password: hashedPassword,
	}

	// Make a token
	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics"}
	tokenStr, err := auth.newToken(testPassword, endpoints)
	if err != nil {
		t.Fatal(err)
	}

	if err := auth.revokeToken(tokenStr, testPassword); err != nil {
		t.Fatal("should have succeeded")
	} else if len(auth.revoked) != 1 || auth.revoked[0] != tokenStr {
		t.Fatal("revoked token list is incorrect")
	}
}

func TestWrapHandlerHappyPath(t *testing.T) {
	auth := Auth{
		Enabled:  true,
		Password: hashedPassword,
	}

	// Make a token
	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics"}
	tokenStr, err := auth.newToken(testPassword, endpoints)
	if err != nil {
		t.Fatal(err)
	}

	wrappedHandler := auth.WrapHandler(dummyHandler)

	for _, endpoint := range endpoints {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:9650%s", endpoint), strings.NewReader(""))
		req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", tokenStr))
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatal("should have passed authorization")
		}
	}
}

func TestWrapHandlerRevokedToken(t *testing.T) {
	auth := Auth{
		Enabled:  true,
		Password: hashedPassword,
	}

	// Make a token
	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics"}
	tokenStr, err := auth.newToken(testPassword, endpoints)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.revokeToken(tokenStr, testPassword); err != nil {
		t.Fatalf("should have been able to revoke token but got: %s", err)
	}

	wrappedHandler := auth.WrapHandler(dummyHandler)

	for _, endpoint := range endpoints {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:9650%s", endpoint), strings.NewReader(""))
		req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", tokenStr))
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatal("should have failed authorization because token was revoked")
		}
	}
}

func TestWrapHandlerExpiredToken(t *testing.T) {
	auth := Auth{
		Enabled:  true,
		Password: hashedPassword,
	}
	auth.clock.Set(time.Now().Add(-2 * TokenLifespan))

	// Make a token that expired well in the past
	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics"}
	tokenStr, err := auth.newToken(testPassword, endpoints)
	if err != nil {
		t.Fatal(err)
	}

	wrappedHandler := auth.WrapHandler(dummyHandler)

	for _, endpoint := range endpoints {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:9650%s", endpoint), strings.NewReader(""))
		req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", tokenStr))
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatal("should have failed authorization because token is expired")
		}
	}
}

func TestWrapHandlerNoAuthToken(t *testing.T) {
	auth := Auth{
		Enabled:  true,
		Password: hashedPassword,
	}

	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics"}
	wrappedHandler := auth.WrapHandler(dummyHandler)
	for _, endpoint := range endpoints {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:9650%s", endpoint), strings.NewReader(""))
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatal("should have failed authorization since no auth token given")
		}
	}
}

func TestWrapHandlerUnauthorizedEndpoint(t *testing.T) {
	auth := Auth{
		Enabled:  true,
		Password: hashedPassword,
	}

	// Make a token
	endpoints := []string{"/ext/info"}
	tokenStr, err := auth.newToken(testPassword, endpoints)
	if err != nil {
		t.Fatal(err)
	}

	unauthorizedEndpoints := []string{"/ext/bc/X", "/ext/metrics", "", "/foo", "/ext/info/foo"}

	wrappedHandler := auth.WrapHandler(dummyHandler)
	for _, endpoint := range unauthorizedEndpoints {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:9650%s", endpoint), strings.NewReader(""))
		req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", tokenStr))
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatal("should have failed authorization since this endpoint is not allowed by the token")
		}
	}
}

func TestWrapHandlerAuthEndpoint(t *testing.T) {
	auth := Auth{
		Enabled:  true,
		Password: hashedPassword,
	}

	// Make a token
	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics", "", "/foo", "/ext/info/foo"}
	tokenStr, err := auth.newToken(testPassword, endpoints)
	if err != nil {
		t.Fatal(err)
	}

	wrappedHandler := auth.WrapHandler(dummyHandler)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:9650%s", fmt.Sprintf("/ext/%s", Endpoint)), strings.NewReader(""))
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", tokenStr))
	rr := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatal("should always allow access to the auth endpoint")
	}
}

func TestWrapHandlerAccessAll(t *testing.T) {
	auth := Auth{
		Enabled:  true,
		Password: hashedPassword,
	}

	// Make a token that allows access to all endpoints
	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics", "", "/foo", "/ext/foo/info"}
	tokenStr, err := auth.newToken(testPassword, []string{"*"})
	if err != nil {
		t.Fatal(err)
	}

	wrappedHandler := auth.WrapHandler(dummyHandler)
	for _, endpoint := range endpoints {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:9650%s", endpoint), strings.NewReader(""))
		req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", tokenStr))
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatal("* in token should have allowed access to all endpoints")
		}
	}
}

func TestWrapHandlerAuthDisabled(t *testing.T) {
	auth := Auth{
		Enabled:  false,
		Password: hashedPassword,
	}

	endpoints := []string{"/ext/info", "/ext/bc/X", "/ext/metrics", "", "/foo", "/ext/foo/info", "/ext/auth"}

	wrappedHandler := auth.WrapHandler(dummyHandler)
	for _, endpoint := range endpoints {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:9650%s", endpoint), strings.NewReader(""))
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatal("auth is disabled so should allow access to all endpoints")
		}
	}
}
