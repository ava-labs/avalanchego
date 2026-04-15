// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pendingreview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProxyHandlerRejectsVersionMismatch(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := newProxyHTTPRequest(t, proxyRequestEnvelope{
		Version: proxyAPIVersion + 1,
		Command: "version",
		Payload: mustProxyPayloadForTest(t, struct{}{}),
	})

	proxyHandler{allowedRepo: defaultRepo}.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusConflict, recorder.Code)
	response := decodeProxyResponseForTest(t, recorder)
	require.Equal(t, "incompatible pending-review proxy version: client=2 server=1\n", response.Stderr)
	require.Equal(t, 1, response.ExitCode)
}

func TestProxyHandlerRejectsDisallowedRepo(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := newProxyHTTPRequest(t, proxyRequestEnvelope{
		Version: proxyAPIVersion,
		Command: "get",
		Payload: mustProxyPayloadForTest(t, proxyGetPayload{
			Repo:     "octo/example",
			PRNumber: 5168,
		}),
	})

	proxyHandler{
		allowedRepo: defaultRepo,
		execute: func(context.Context, command, io.Writer, io.Writer) error {
			t.Fatal("execute should not be called")
			return nil
		},
	}.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	response := decodeProxyResponseForTest(t, recorder)
	require.Equal(t, "proxy only permits repo ava-labs/avalanchego\n", response.Stderr)
	require.Equal(t, 1, response.ExitCode)
}

func TestProxyHandlerRejectsMalformedPayload(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := newProxyHTTPRequest(t, proxyRequestEnvelope{
		Version: proxyAPIVersion,
		Command: "get",
		Payload: json.RawMessage(`{"repo":"ava-labs/avalanchego","pr_number":"bad"}`),
	})

	proxyHandler{allowedRepo: defaultRepo}.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	response := decodeProxyResponseForTest(t, recorder)
	require.Contains(t, response.Stderr, "invalid proxy payload for get:")
	require.Equal(t, 1, response.ExitCode)
}

func TestProxyHandlerRejectsUnknownCommand(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := newProxyHTTPRequest(t, proxyRequestEnvelope{
		Version: proxyAPIVersion,
		Command: "nope",
		Payload: mustProxyPayloadForTest(t, struct{}{}),
	})

	proxyHandler{allowedRepo: defaultRepo}.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	response := decodeProxyResponseForTest(t, recorder)
	require.Equal(t, "unsupported proxy command \"nope\"\n", response.Stderr)
	require.Equal(t, 1, response.ExitCode)
}

func TestProxyHandlerReturnsExecutorOutput(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := newProxyHTTPRequest(t, proxyRequestEnvelope{
		Version: proxyAPIVersion,
		Command: "version",
		Payload: mustProxyPayloadForTest(t, struct{}{}),
	})

	proxyHandler{
		allowedRepo: defaultRepo,
		execute: func(_ context.Context, cmd command, stdout, stderr io.Writer) error {
			_, err := stdout.Write([]byte("hello\n"))
			require.NoError(t, err)
			_, err = stderr.Write([]byte("note\n"))
			require.NoError(t, err)
			_, ok := cmd.(versionCommand)
			require.True(t, ok)
			return nil
		},
	}.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	response := decodeProxyResponseForTest(t, recorder)
	require.Equal(t, "hello\n", response.Stdout)
	require.Equal(t, "note\n", response.Stderr)
	require.Zero(t, response.ExitCode)
}

func TestProxyHandlerReturnsExecutorErrorEnvelope(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := newProxyHTTPRequest(t, proxyRequestEnvelope{
		Version: proxyAPIVersion,
		Command: "version",
		Payload: mustProxyPayloadForTest(t, struct{}{}),
	})

	proxyHandler{
		allowedRepo: defaultRepo,
		execute: func(context.Context, command, io.Writer, io.Writer) error {
			return errors.New("boom")
		},
	}.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	response := decodeProxyResponseForTest(t, recorder)
	require.Contains(t, response.Stderr, "boom")
	require.Equal(t, 1, response.ExitCode)
}

func TestValidateProxyListenAddrRejectsNonLoopback(t *testing.T) {
	err := validateProxyListenAddr("0.0.0.0:18080")
	require.EqualError(t, err, "proxy listen address must bind to loopback, got \"0.0.0.0:18080\"")
}

func TestStartProxyServerBindsLoopback(t *testing.T) {
	server, err := StartProxyServer("127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, server.Stop(stopCtx))
	})

	require.Contains(t, server.Address(), "127.0.0.1:")
}

func newProxyHTTPRequest(t *testing.T, envelope proxyRequestEnvelope) *http.Request {
	t.Helper()

	body, err := json.Marshal(envelope)
	require.NoError(t, err)

	request := httptest.NewRequest(http.MethodPost, proxyEndpointPath, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}

func decodeProxyResponseForTest(t *testing.T, recorder *httptest.ResponseRecorder) proxyResponseEnvelope {
	t.Helper()

	var response proxyResponseEnvelope
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func mustProxyPayloadForTest(t *testing.T, payload any) json.RawMessage {
	t.Helper()

	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	return json.RawMessage(encoded)
}
