// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"fmt"
	"net/http"

	"github.com/ava-labs/avalanchego/trace"
)

var _ http.Handler = (*tracedHandler)(nil)

type tracedHandler struct {
	h         http.Handler
	serveHTTP string
	tracer    trace.Tracer
}

func TraceHandler(h http.Handler, name string, tracer trace.Tracer) http.Handler {
	return &tracedHandler{
		h:         h,
		serveHTTP: fmt.Sprintf("%s.ServeHTTP", name),
		tracer:    tracer,
	}
}

func (h *tracedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ctx, span := h.tracer.Start(ctx, h.serveHTTP)
	defer span.End()

	r = r.WithContext(ctx)
	h.h.ServeHTTP(w, r)
}
