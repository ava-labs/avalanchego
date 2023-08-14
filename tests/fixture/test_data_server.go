// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fixture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
)

const (
	allocateKeysPath                  = "/allocateKeys"
	keyCountParameterName             = "count"
	requestedKeyCountExceedsAvailable = "requested key count exceeds available allocation"
)

var (
	errRequestedKeyCountExceedsAvailable = errors.New(requestedKeyCountExceedsAvailable)
	errInvalidKeyCount                   = errors.New("key count must be greater than zero")
)

type TestData struct {
	FundedKeys []*secp256k1.PrivateKey
}

// http server allocating resources to tests potentially executing in parallel
type testDataServer struct {
	// Synchronizes access to test data
	lock sync.Mutex
	TestData
}

// Type used to marshal/unmarshal a set of test keys for transmission over http.
type keysDocument struct {
	Keys []*secp256k1.PrivateKey `json:"keys"`
}

func (s *testDataServer) allocateKeys(w http.ResponseWriter, r *http.Request) {
	// Attempt to parse the count parameter
	rawKeyCount := r.URL.Query().Get(keyCountParameterName)
	if len(rawKeyCount) == 0 {
		msg := fmt.Sprintf("missing %q parameter", keyCountParameterName)
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	keyCount, err := strconv.Atoi(rawKeyCount)
	if err != nil {
		msg := fmt.Sprintf("unable to parse %q parameter: %v", keyCountParameterName, err)
		http.Error(w, msg, http.StatusBadRequest)
		return
	}

	// Ensure a key will be allocated at most once
	s.lock.Lock()
	defer s.lock.Unlock()

	// Only fulfill requests for available keys
	if keyCount > len(s.FundedKeys) {
		http.Error(w, requestedKeyCountExceedsAvailable, http.StatusInternalServerError)
		return
	}

	// Allocate the requested number of keys
	remainingKeys := len(s.FundedKeys) - keyCount
	allocatedKeys := s.FundedKeys[remainingKeys:]

	keysDoc := &keysDocument{
		Keys: allocatedKeys,
	}
	if err := json.NewEncoder(w).Encode(keysDoc); err != nil {
		msg := fmt.Sprintf("failed to encode test keys: %v", err)
		http.Error(w, msg, http.StatusInternalServerError)
		return
	}

	// Forget the allocated keys
	utils.ZeroSlice(allocatedKeys)
	s.FundedKeys = s.FundedKeys[:remainingKeys]
}

// Serve test data via http to ensure allocation is synchronized even when
// ginkgo specs are executing in parallel. Returns the URI to access the server.
func ServeTestData(testData TestData) (string, error) {
	// Listen on a dynamic port to avoid conflicting with other applications
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("failed to initialize listener for test data server: %w", err)
	}
	address := fmt.Sprintf("http://%s", listener.Addr())

	s := &testDataServer{
		TestData: testData,
	}
	mux := http.NewServeMux()
	mux.HandleFunc(allocateKeysPath, s.allocateKeys)

	httpServer := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
	}

	go func() {
		// Serve always returns a non-nil error and closes l.
		if err := httpServer.Serve(listener); err != http.ErrServerClosed {
			panic(fmt.Sprintf("unexpected error closing test data server: %v", err))
		}
	}()

	return address, nil
}

// Retrieve the specified number of funded test keys from the provided URI. A given
// key is allocated at most once during the life of the test data server.
func AllocateFundedKeys(baseURI string, count int) ([]*secp256k1.PrivateKey, error) {
	if count <= 0 {
		return nil, errInvalidKeyCount
	}

	uri, err := url.Parse(baseURI)
	if err != nil {
		return nil, fmt.Errorf("failed to parse uri: %w", err)
	}
	uri.RawQuery = url.Values{
		keyCountParameterName: {strconv.Itoa(count)},
	}.Encode()
	uri.Path = allocateKeysPath
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, uri.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to construct request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to request funded keys: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response for funded keys: %w", err)
	}
	if resp.StatusCode != 200 {
		if strings.TrimSpace(string(body)) == requestedKeyCountExceedsAvailable {
			return nil, errRequestedKeyCountExceedsAvailable
		}
		return nil, fmt.Errorf("test data server returned unexpected status code %d: %v", resp.StatusCode, body)
	}

	keysDoc := &keysDocument{}
	if err := json.Unmarshal(body, keysDoc); err != nil {
		return nil, fmt.Errorf("failed to unmarshal funded keys: %w", err)
	}
	return keysDoc.Keys, nil
}
