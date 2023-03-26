// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dynamicip

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

var _ Resolver = (*ifConfigResolver)(nil)

// ifConfigResolver resolves our public IP using ifconfig's format.
type ifConfigResolver struct {
	url string
}

func (r *ifConfigResolver) Resolve(ctx context.Context) (net.IP, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", r.url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	ipBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		// Drop any error to report the original error
		return nil, fmt.Errorf("failed to read response from %q: %w", r.url, err)
	}

	ipStr := string(ipBytes)
	ipStr = strings.TrimSpace(ipStr)
	ipResolved := net.ParseIP(ipStr)
	if ipResolved == nil {
		return nil, fmt.Errorf("couldn't parse IP from %q", ipStr)
	}
	return ipResolved, nil
}
