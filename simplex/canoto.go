// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

//go:generate go run github.com/StephenButtolph/canoto/canoto $GOFILE

// CanotoSimplexBlock is the Canoto representation of a verified block
type CanotoSimplexBlock struct {
	Metadata   []byte `canoto:"bytes,1"`
	InnerBlock []byte `canoto:"bytes,2"`

	canotoData canotoData_CanotoSimplexBlock
}
