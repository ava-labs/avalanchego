// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validator

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/network/p2p/oracle"
)

func newTestMsg(t *testing.T) *oracle.OracleMessage {
	t.Helper()
	msg, err := oracle.NewOracleMessage("solana", "addr1", []byte{1, 2, 3}, 100, 1, []byte("payload"))
	require.NoError(t, err)
	return msg
}

func TestHTTPSidecarClient_Accept(t *testing.T) {
	var got verifyRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/verify", r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{}"))
	}))
	defer srv.Close()

	msg := newTestMsg(t)
	justification := []byte("sig-bytes")

	c := NewHTTPSidecarClient(srv.URL, nil)
	require.NoError(t, c.Verify(t.Context(), &oracle.OracleEvent{
		Message:       msg,
		Justification: justification,
	}))

	require.Equal(t, hex.EncodeToString(msg.Bytes()), got.MessageBytes)
	require.Equal(t, hex.EncodeToString(justification), got.Justification)
}

func TestHTTPSidecarClient_RejectWithError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"bad event"}`))
	}))
	defer srv.Close()

	c := NewHTTPSidecarClient(srv.URL, nil)
	err := c.Verify(t.Context(), &oracle.OracleEvent{
		Message:       newTestMsg(t),
		Justification: nil,
	})
	require.ErrorContains(t, err, "bad event")
}

func TestHTTPSidecarClient_RejectEmptyError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("{}"))
	}))
	defer srv.Close()

	c := NewHTTPSidecarClient(srv.URL, nil)
	err := c.Verify(t.Context(), &oracle.OracleEvent{
		Message:       newTestMsg(t),
		Justification: nil,
	})
	require.ErrorContains(t, err, "sidecar returned HTTP 400")
}

func TestHTTPSidecarClient_Unreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // close immediately so the connection is refused

	c := NewHTTPSidecarClient(srv.URL, nil)
	err := c.Verify(t.Context(), &oracle.OracleEvent{
		Message:       newTestMsg(t),
		Justification: nil,
	})
	require.ErrorContains(t, err, "sidecar unreachable")
}
