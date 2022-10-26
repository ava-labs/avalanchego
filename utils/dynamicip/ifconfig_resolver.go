// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dynamicip

import (
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

func (r *ifConfigResolver) Resolve() (net.IP, error) {
	resp, err := http.Get(r.url)
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
