// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dynamicip

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"

	"github.com/ava-labs/avalanchego/utils/ips"
)

var _ Resolver = (*ifConfigResolver)(nil)

// ifConfigResolver resolves our public IP using ifconfig's format.
type ifConfigResolver struct {
	url string
}

func (r *ifConfigResolver) Resolve(ctx context.Context) (netip.Addr, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.url, nil)
	if err != nil {
		return netip.Addr{}, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return netip.Addr{}, err
	}
	defer resp.Body.Close()

	ipBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		// Drop any error to report the original error
		return netip.Addr{}, fmt.Errorf("failed to read response from %q: %w", r.url, err)
	}

	ipStr := strings.TrimSpace(string(ipBytes))
	return ips.ParseAddr(ipStr)
}
