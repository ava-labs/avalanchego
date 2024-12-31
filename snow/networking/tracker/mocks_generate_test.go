// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

//go:generate go run go.uber.org/mock/mockgen@v0.5 -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/targeter.go -mock_names=Targeter=Targeter . Targeter
//go:generate go run go.uber.org/mock/mockgen@v0.5 -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/tracker.go -mock_names=Tracker=Tracker . Tracker
