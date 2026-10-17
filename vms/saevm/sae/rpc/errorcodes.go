// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// A jsonrpcResponse holds the only field of a JSON-RPC response read by
// [parseErrorCodes].
type jsonrpcResponse struct {
	Error *jsonrpcError `json:"error"`
}

type jsonrpcError struct {
	Code int `json:"code"`
}

// parseErrorCodes returns the number of times each JSON-RPC error code occurs
// in body, which holds either a single message or a batch. Malformed input is
// ignored.
func parseErrorCodes(body []byte) map[int]int {
	if isBatch(body) {
		return parseBatchErrorCodes(body)
	}
	code, ok := parseMessageErrorCode(body)
	if !ok {
		return nil
	}
	return map[int]int{code: 1}
}

// isBatch reports whether body is a JSON array, using the same check of the
// first non-whitespace byte as the libevm RPC server.
func isBatch(body []byte) bool {
	for _, c := range body {
		switch c {
		case ' ', '\t', '\n', '\r':
			continue
		}
		return c == '['
	}
	return false
}

// parseBatchErrorCodes returns the number of times each error code occurs in a
// batch response. Skipping each result requires scanning it, but the server
// bounds the size of batch responses.
func parseBatchErrorCodes(body []byte) map[int]int {
	var batch []jsonrpcResponse
	if err := json.Unmarshal(body, &batch); err != nil {
		return nil
	}
	counts := make(map[int]int)
	for _, r := range batch {
		if r.Error != nil {
			counts[r.Error.Code]++
		}
	}
	return counts
}

// parseMessageErrorCode returns the error code of a single JSON-RPC message, if
// it has one.
//
// TODO(SayanKar): a response has either an error or a result, so decoding
// could stop at whichever comes first instead of scanning a potentially large
// result, if decoding large responses significantly increases latency.
func parseMessageErrorCode(body []byte) (int, bool) {
	var r jsonrpcResponse
	if err := json.Unmarshal(body, &r); err != nil || r.Error == nil {
		return 0, false
	}
	return r.Error.Code, true
}

// formatErrorCodesHeader formats counts as the value of [ErrorCodesHeader].
func formatErrorCodesHeader(counts map[int]int) string {
	items := make([]string, 0, len(counts))
	for _, code := range slices.Sorted(maps.Keys(counts)) {
		items = append(items, strconv.Itoa(code)+";count="+strconv.Itoa(counts[code]))
	}
	return strings.Join(items, ", ")
}
