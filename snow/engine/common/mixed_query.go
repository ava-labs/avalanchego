// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

// Send a query composed partially of push queries and partially of pull queries.
// The validators in [vdrs] will be queried.
// This function sends at most [numPushTo] push queries. The rest are pull queries.
// If [numPushTo] > len(vdrs), len(vdrs) push queries are sent.
// [containerID] and [container] are the ID and body of the container being queried.
// [sender] is used to actually send the queries.
func SendMixedQuery(
	ctx context.Context,
	sender Sender,
	vdrs []ids.NodeID,
	numPushTo int,
	reqID uint32,
	containerID ids.ID,
	container []byte,
) {
	if numPushTo > len(vdrs) {
		numPushTo = len(vdrs)
	}
	if numPushTo > 0 {
		sendPushQueryTo := set.NewSet[ids.NodeID](numPushTo)
		sendPushQueryTo.Add(vdrs[:numPushTo]...)
		sender.SendPushQuery(ctx, sendPushQueryTo, reqID, container)
	}
	if numPullTo := len(vdrs) - numPushTo; numPullTo > 0 {
		sendPullQueryTo := set.NewSet[ids.NodeID](numPullTo)
		sendPullQueryTo.Add(vdrs[numPushTo:]...)
		sender.SendPullQuery(ctx, sendPullQueryTo, reqID, containerID)
	}
}
