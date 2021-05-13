package metricutils

import (
	"context"
	"net/http"
	"time"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/gorilla/rpc/v2"
	"github.com/prometheus/client_golang/prometheus"
)

type APIRequestMetrics interface {
	InterceptAPIRequest(i *rpc.RequestInfo) *http.Request
	AfterAPIRequest(i *rpc.RequestInfo)
	Register(registerer prometheus.Registerer) error
}

type contextKey int

const requestTimestampKey contextKey = iota

type apiRequest struct {
	requestDuration *prometheus.HistogramVec
	requestErrors   *prometheus.CounterVec
}

func NewAPIMetrics(namespace string) APIRequestMetrics {
	return &apiRequest{
		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "request_duration_ms",
			Buckets:   utils.MillisecondsHTTPBuckets,
		}, []string{"method"}),
		requestErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "request_error_count",
		}, []string{"method"}),
	}
}

func (apr *apiRequest) Register(registerer prometheus.Registerer) error {
	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(apr.requestDuration),
		registerer.Register(apr.requestErrors),
	)
	return errs.Err
}

func (apr *apiRequest) InterceptAPIRequest(i *rpc.RequestInfo) *http.Request {
	return i.Request.WithContext(context.WithValue(i.Request.Context(), requestTimestampKey, time.Now()))
}

func (apr *apiRequest) AfterAPIRequest(i *rpc.RequestInfo) {
	timestamp := i.Request.Context().Value(requestTimestampKey)
	timeCast, ok := timestamp.(time.Time)
	if !ok {
		return
	}
	totalTime := time.Since(timeCast)
	apr.requestDuration.With(prometheus.Labels{"method": i.Method}).Observe(float64(totalTime.Milliseconds()))

	if i.Error != nil {
		apr.requestErrors.With(prometheus.Labels{"method": i.Method}).Inc()
	}
}
