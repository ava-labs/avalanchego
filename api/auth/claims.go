// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package auth

import (
	jwt "github.com/golang-jwt/jwt/v4"
)

// Custom claim type used for API access token
type endpointClaims struct {
	jwt.RegisteredClaims

	// Each element is an endpoint that the token allows access to
	// If endpoints has an element "*", allows access to all API endpoints
	// In this case, "*" should be the only element of [endpoints]
	Endpoints []string `json:"endpoints,omitempty"`
}
