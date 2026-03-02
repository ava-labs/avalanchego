// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAllowedHostsHandler_ServeHTTP(t *testing.T) {
	tests := []struct {
		name    string
		allowed []string
		host    string
		serve   bool
	}{
		{
			name:    "no host header",
			allowed: []string{"www.foobar.com"},
			host:    "",
			serve:   true,
		},
		{
			name:    "ip",
			allowed: []string{"www.foobar.com"},
			host:    "192.168.1.1",
			serve:   true,
		},
		{
			name:    "hostname not allowed",
			allowed: []string{"www.foobar.com"},
			host:    "www.evil.com",
		},
		{
			name:    "hostname allowed",
			allowed: []string{"www.foobar.com"},
			host:    "www.foobar.com",
			serve:   true,
		},
		{
			name:    "wildcard",
			allowed: []string{"*"},
			host:    "www.foobar.com",
			serve:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			baseHandler := &testHandler{}

			httpAllowedHostsHandler := filterInvalidHosts(
				baseHandler,
				test.allowed,
			)

			w := &httptest.ResponseRecorder{}
			r := httptest.NewRequest("", "/", nil)
			r.Host = test.host

			httpAllowedHostsHandler.ServeHTTP(w, r)

			if test.serve {
				require.True(baseHandler.called)
				return
			}

			require.Equal(http.StatusForbidden, w.Code)
		})
	}
}
