// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package registry

//go:generate go tool mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/vm_getter.go -mock_names=VMGetter=VMGetter . VMGetter
//go:generate go tool mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/vm_registry.go -mock_names=VMRegistry=VMRegistry . VMRegistry
