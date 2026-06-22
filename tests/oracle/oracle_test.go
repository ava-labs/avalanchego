// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package oracle_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/oracle"
	"github.com/ava-labs/avalanchego/network/p2p/oracle/validator"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// TestSignatureRequestHandlerID asserts that oracle attestation is registered at
// handler ID 4, distinct from the native warp handler ID (2). This constant is
// the shared contract between validators and icm-services' OracleSignatureAggregator.
func TestSignatureRequestHandlerID(t *testing.T) {
	require.Equal(t, uint64(4), validator.SignatureRequestHandlerID)
}

func buildWarpMsg(t *testing.T, msg *oracle.OracleMessage) *warp.UnsignedMessage {
	t.Helper()
	um, err := warp.NewUnsignedMessage(1, ids.GenerateTestID(), msg.Bytes())
	require.NoError(t, err)
	return um
}

// TestValidatorPipeline_Accept exercises the full happy path:
// OracleMessage → warp payload → OracleVerifier (parse + allowlist) →
// HTTPSidecarClient → test server accepts → nil AppError.
func TestValidatorPipeline_Accept(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{}"))
	}))
	defer srv.Close()

	msg, err := oracle.NewOracleMessage("solana", "program1", []byte{1, 2, 3}, 500, 7, []byte("data"))
	require.NoError(t, err)

	sidecar := validator.NewHTTPSidecarClient(srv.URL, nil)
	verifier := validator.NewOracleVerifier(sidecar, validator.AllowedSources{
		"solana": {"program1": {}},
	})

	appErr := verifier.Verify(t.Context(), buildWarpMsg(t, msg), []byte("justification"))
	require.Nil(t, appErr)
	require.True(t, called, "sidecar must be contacted on the happy path")
}

// TestValidatorPipeline_AllowlistBlock ensures that a message from a source type
// not in the allowlist is rejected before the sidecar is ever contacted.
func TestValidatorPipeline_AllowlistBlock(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	msg, err := oracle.NewOracleMessage("bitcoin", "addr1", []byte{1, 2, 3}, 1, 1, nil)
	require.NoError(t, err)

	sidecar := validator.NewHTTPSidecarClient(srv.URL, nil)
	verifier := validator.NewOracleVerifier(sidecar, validator.AllowedSources{
		"solana": {}, // only solana is permitted
	})

	appErr := verifier.Verify(t.Context(), buildWarpMsg(t, msg), nil)
	require.NotNil(t, appErr)
	require.False(t, called, "sidecar must not be contacted when allowlist rejects")
}

// TestValidatorPipeline_SidecarReject verifies that a sidecar 400 response
// propagates back through OracleVerifier as a non-nil AppError.
func TestValidatorPipeline_SidecarReject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"tx not found on solana"}`))
	}))
	defer srv.Close()

	msg, err := oracle.NewOracleMessage("solana", "program1", []byte{1, 2, 3}, 1, 1, nil)
	require.NoError(t, err)

	sidecar := validator.NewHTTPSidecarClient(srv.URL, nil)
	verifier := validator.NewOracleVerifier(sidecar, validator.AllowedSources{
		"solana": {},
	})

	appErr := verifier.Verify(t.Context(), buildWarpMsg(t, msg), nil)
	require.NotNil(t, appErr)
	require.Contains(t, appErr.Message, "sidecar verification failed")
}
