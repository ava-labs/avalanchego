// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type echoService struct{}

func (echoService) Echo(_ *http.Request, args *string, reply *string) error {
	*reply = *args
	return nil
}

func TestNewHandler(t *testing.T) {
	handler, err := NewHandler("echo", echoService{})
	require.NoError(t, err, "NewHandler()")

	const msg = "austin, ive been trying to reach you about your car's extended warranty"

	// The lowercase method name verifies the avalanchego codec is used.
	body := fmt.Sprintf(`{"jsonrpc":"2.0","method":"echo.echo","params":%q,"id":1}`, msg)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "ServeHTTP() status code")
	want := fmt.Sprintf(`{"jsonrpc":"2.0","result":%q,"id":1}`, msg)
	require.JSONEq(t, want, rec.Body.String(), "ServeHTTP() body")
}
