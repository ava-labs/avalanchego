// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ghttp

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	httppb "github.com/ava-labs/avalanchego/proto/pb/http"
)

func TestConvertWriteResponse(t *testing.T) {
	scenerios := map[string]struct {
		resp *httppb.HandleSimpleHTTPResponse
	}{
		"empty response": {
			resp: &httppb.HandleSimpleHTTPResponse{},
		},
		"response header value empty": {
			resp: &httppb.HandleSimpleHTTPResponse{
				Code: 500,
				Body: []byte("foo"),
				Headers: []*httppb.Element{
					{
						Key: "foo",
					},
				},
			},
		},
		"response header key empty": {
			resp: &httppb.HandleSimpleHTTPResponse{
				Code: 200,
				Body: []byte("foo"),
				Headers: []*httppb.Element{
					{
						Values: []string{"foo"},
					},
				},
			},
		},
	}
	for testName, scenerio := range scenerios {
		t.Run(testName, func(t *testing.T) {
			w := httptest.NewRecorder()
			require.NoError(t, convertWriteResponse(w, scenerio.resp))
		})
	}
}
