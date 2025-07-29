// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
)

type getCountFunc func() (int, error)

// waitForCount waits until the provided function returns greater than zero.
func waitForCount(ctx context.Context, log logging.Logger, name string, getCount getCountFunc) error {
	err := pollUntilContextCancel(
		ctx,
		func(_ context.Context) (bool, error) {
			count, err := getCount()
			if err != nil {
				log.Warn("failed to query for collected count",
					zap.String("type", name),
					zap.Error(err),
				)
				return false, nil
			}
			if count > 0 {
				log.Info("collected count is non-zero",
					zap.String("type", name),
					zap.Int("count", count),
				)
			}
			return count > 0, nil
		},
	)
	if err != nil {
		return fmt.Errorf("%s not found before timeout: %w", name, err)
	}
	return nil
}

// CheckLogsExist checks if logs exist for the given network. If no network UUID is
// provided, an attempt will be made to derive selectors from env vars (GH_*) identifying
// a github actions run.
func CheckLogsExist(ctx context.Context, log logging.Logger, networkUUID string) error {
	username, password, err := getCollectorCredentials(promtailCmd)
	if err != nil {
		return fmt.Errorf("failed to get collector credentials: %w", err)
	}

	url := getLokiURL()
	if !strings.HasPrefix(url, "https") {
		return fmt.Errorf("loki URL must be https for basic auth to be secure: %s", url)
	}

	selectors, err := getSelectors(networkUUID)
	if err != nil {
		return err
	}
	query := fmt.Sprintf("sum(count_over_time({%s}[1h]))", selectors)

	log.Info("checking if logs exist",
		zap.String("url", url),
		zap.String("query", query),
	)

	return waitForCount(
		ctx,
		log,
		"logs",
		func() (int, error) {
			return queryLoki(ctx, url, username, password, query)
		},
	)
}

func queryLoki(
	ctx context.Context,
	lokiURL string,
	username string,
	password string,
	query string,
) (int, error) {
	// Compose the URL
	params := url.Values{}
	params.Add("query", query)
	reqURL := fmt.Sprintf("%s/loki/api/v1/query?%s", lokiURL, params.Encode())

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	req.Header.Set("Authorization", "Basic "+auth)

	// Execute request
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	// Read and parse response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	// Parse JSON response
	var result struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Value []interface{} `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("failed to parse response: %w", err)
	}

	// Extract count value
	if len(result.Data.Result) == 0 {
		return 0, nil
	}
	if len(result.Data.Result[0].Value) != 2 {
		return 0, errors.New("unexpected value format in response")
	}
	// Convert value to a string
	valueStr, ok := result.Data.Result[0].Value[1].(string)
	if !ok {
		return 0, errors.New("value is not a string")
	}
	// Convert string to float64 first to handle scientific notation
	floatVal, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing count value: %w", err)
	}
	// Round to nearest integer
	return int(math.Round(floatVal)), nil
}

// CheckMetricsExist checks if metrics exist for the given network. Github labels are also
// used as filters if provided as env vars (GH_*).
func CheckMetricsExist(ctx context.Context, log logging.Logger, networkUUID string) error {
	username, password, err := getCollectorCredentials(prometheusCmd)
	if err != nil {
		return fmt.Errorf("failed to get collector credentials: %w", err)
	}

	url := getPrometheusURL()
	if !strings.HasPrefix(url, "https") {
		return fmt.Errorf("prometheus URL must be https for basic auth to be secure: %s", url)
	}

	selectors, err := getSelectors(networkUUID)
	if err != nil {
		return err
	}
	query := fmt.Sprintf("count({%s})", selectors)

	log.Info("checking if metrics exist",
		zap.String("url", url),
		zap.String("query", query),
	)

	return waitForCount(
		ctx,
		log,
		"metrics",
		func() (int, error) {
			return queryPrometheus(ctx, log, url, username, password, query)
		},
	)
}

func queryPrometheus(
	ctx context.Context,
	log logging.Logger,
	url string,
	username string,
	password string,
	query string,
) (int, error) {
	// Create client with basic auth
	client, err := api.NewClient(api.Config{
		Address: url,
		RoundTripper: &basicAuthRoundTripper{
			username: username,
			password: password,
			rt:       api.DefaultRoundTripper,
		},
	})
	if err != nil {
		return 0, fmt.Errorf("failed to create client: %w", err)
	}

	// Query Prometheus
	result, warnings, err := v1.NewAPI(client).QueryRange(ctx, query, v1.Range{
		Start: time.Now().Add(-time.Hour),
		End:   time.Now(),
		Step:  time.Minute,
	})
	if err != nil {
		return 0, fmt.Errorf("query failed: %w", err)
	}
	if len(warnings) > 0 {
		log.Warn("prometheus query warnings",
			zap.Strings("warnings", warnings),
		)
	}

	if matrix, ok := result.(model.Matrix); !ok {
		return 0, fmt.Errorf("unexpected result type: %s", result.Type())
	} else if len(matrix) > 0 {
		return int(matrix[0].Values[len(matrix[0].Values)-1].Value), nil
	}

	return 0, nil
}

type basicAuthRoundTripper struct {
	username, password string
	rt                 http.RoundTripper
}

func (b *basicAuthRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.SetBasicAuth(b.username, b.password)
	return b.rt.RoundTrip(req)
}

// getSelectors returns the comma-separated list of selectors.
func getSelectors(networkUUID string) (string, error) {
	// If network UUID is provided, use it as the only selector
	if len(networkUUID) > 0 {
		return fmt.Sprintf(`network_uuid="%s"`, networkUUID), nil
	}

	// Fall back to using Github labels as selectors
	selectors := []string{}
	for _, label := range githubLabels {
		value := os.Getenv(strings.ToUpper(label))
		if len(value) > 0 {
			selectors = append(selectors, fmt.Sprintf(`%s="%s"`, label, value))
		}
	}
	if len(selectors) == 0 {
		return "", errors.New("no GH_* env vars set to use for selectors")
	}

	return strings.Join(selectors, ","), nil
}
