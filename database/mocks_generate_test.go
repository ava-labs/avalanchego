// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

//go:generate go tool mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/batch.go -mock_names=Batch=Batch . Batch
//go:generate go tool mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/iterator.go -mock_names=Iterator=Iterator . Iterator
