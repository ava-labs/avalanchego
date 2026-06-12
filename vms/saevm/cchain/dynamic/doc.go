// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package dynamic implements the exponential integrators used to ramp
// C-Chain consensus parameters smoothly between blocks.
//
// Each parameter is encoded as an exponent type ([PriceExponent],
// [DelayExponent], [TargetExponent]) that exposes:
//   - a reader that decodes the exponent to its parameter value.
//   - Toward, which moves the exponent one clamped step toward a desired
//     target (nil = no change).
//   - a Desired* function that inverts the reader to find the smallest
//     exponent yielding a desired value.
package dynamic
