// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

// EmptyExtDataHash is the known hash of empty extdata bytes.
var EmptyExtDataHash = rlpHash([]byte(nil))
