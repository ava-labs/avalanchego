// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

//go:generate go run go.uber.org/mock/mockgen@v0.5 -package=${GOPACKAGE}mock -source=sender.go -destination=${GOPACKAGE}mock/sender.go -mock_names=Sender=Sender -exclude_interfaces=StateSummarySender,AcceptedStateSummarySender,FrontierSender,AcceptedSender,FetchSender,AppSender,QuerySender,Gossiper
