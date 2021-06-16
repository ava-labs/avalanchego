// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metric

import (
	"context"
	"net/http"
	"time"

	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/gorilla/rpc/v2"
	"github.com/prometheus/client_golang/prometheus"
)

type APIInterceptor interface {
	InterceptRequest(i *rpc.RequestInfo) *http.Request
	AfterRequest(i *rpc.RequestInfo)
}

type contextKey int

const requestTimestampKey contextKey = iota

type apiInterceptor struct {
	requestDuration *prometheus.HistogramVec
	requestErrors   *prometheus.CounterVec
}

func NewAPIInterceptor(namespace string, registerer prometheus.Registerer) (APIInterceptor, error) {
	requestDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "request_duration_ms",
			Buckets:   MillisecondsHTTPBuckets,
		},
		[]string{"method"},
	)
	requestErrors := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "request_error_count",
		},
		[]string{"method"},
	)

	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(requestDuration),
		registerer.Register(requestErrors),
	)
	return &apiInterceptor{
		requestDuration: requestDuration,
		requestErrors:   requestErrors,
	}, errs.Err
}

func (apr *apiInterceptor) InterceptRequest(i *rpc.RequestInfo) *http.Request {
	ctx := i.Request.Context()
	ctx = context.WithValue(ctx, requestTimestampKey, time.Now())
	return i.Request.WithContext(ctx)
}

func (apr *apiInterceptor) AfterRequest(i *rpc.RequestInfo) {
	timestampIntf := i.Request.Context().Value(requestTimestampKey)
	timestamp, ok := timestampIntf.(time.Time)
	if !ok {
		return
	}

	duration := time.Since(timestamp)
	durationMetric := apr.requestDuration.With(prometheus.Labels{
		"method": i.Method,
	})
	durationMetric.Observe(float64(duration.Milliseconds()))

	if i.Error != nil {
		errMetric := apr.requestErrors.With(prometheus.Labels{
			"method": i.Method,
		})
		errMetric.Inc()
	}
}
