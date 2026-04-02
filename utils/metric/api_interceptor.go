// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metric

import (
	"context"
	"errors"
	"net/http"
	"time"

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
	requestDurationCount *prometheus.CounterVec
	requestDurationSum   *prometheus.GaugeVec
	requestErrors        *prometheus.CounterVec
}

func NewAPIInterceptor(registerer prometheus.Registerer) (APIInterceptor, error) {
	requestDurationCount := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "request_duration_count",
			Help: "Number of times this type of request was made",
		},
		[]string{"method"},
	)
	requestDurationSum := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "request_duration_sum",
			Help: "Amount of time in nanoseconds that has been spent handling this type of request",
		},
		[]string{"method"},
	)
	requestErrors := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "request_error_count",
		},
		[]string{"method"},
	)

	err := errors.Join(
		registerer.Register(requestDurationCount),
		registerer.Register(requestDurationSum),
		registerer.Register(requestErrors),
	)
	return &apiInterceptor{
		requestDurationCount: requestDurationCount,
		requestDurationSum:   requestDurationSum,
		requestErrors:        requestErrors,
	}, err
}

func (*apiInterceptor) InterceptRequest(i *rpc.RequestInfo) *http.Request {
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

	durationMetricCount := apr.requestDurationCount.With(prometheus.Labels{
		"method": i.Method,
	})
	durationMetricCount.Inc()

	duration := time.Since(timestamp)
	durationMetricSum := apr.requestDurationSum.With(prometheus.Labels{
		"method": i.Method,
	})
	durationMetricSum.Add(float64(duration))

	if i.Error != nil {
		errMetric := apr.requestErrors.With(prometheus.Labels{
			"method": i.Method,
		})
		errMetric.Inc()
	}
}
