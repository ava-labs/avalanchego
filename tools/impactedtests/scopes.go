// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"errors"
	"strings"
)

func scopeExpression(scopes []string) (string, error) {
	if len(scopes) == 0 {
		return "", errors.New("at least one --scope is required")
	}

	positives := make([]string, 0, len(scopes))
	negatives := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			return "", errors.New("scope must not be empty")
		}
		if strings.HasPrefix(scope, "-") {
			negatives = append(negatives, strings.TrimPrefix(scope, "-"))
			continue
		}
		positives = append(positives, scope)
	}
	if len(positives) == 0 {
		return "", errors.New("at least one positive --scope is required")
	}

	expr := unionExpression(positives)
	if len(negatives) > 0 {
		expr += " except " + unionExpression(negatives)
	}
	return expr, nil
}

func unionExpression(scopes []string) string {
	var expr strings.Builder
	expr.WriteString(scopes[0])
	for _, scope := range scopes[1:] {
		expr.WriteString(" union ")
		expr.WriteString(scope)
	}
	return expr.String()
}
