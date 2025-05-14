// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/onsi/ginkgo/v2"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

// keyToFilter is the label name to remove from Grafana dashboard URLs.
// Load metrics don't have this label, so removing it prevents "No Data" errors.
const keyToFilter = "is_ephemeral_node"

var _ = ginkgo.BeforeEach(func() {
	// Disable default metrics link generation
	e2e.EmitMetricsLink = false

	tc := e2e.NewTestContext()
	env := e2e.GetEnv(tc)

	if env == nil {
		return
	}

	specReport := ginkgo.CurrentSpecReport()
	startTimeMs := specReport.StartTime.UnixMilli()
	metricsLink, err := removeQueryFilter(e2e.MetricsLink(startTimeMs))
	if err != nil {
		tc.Log().Error("Failed to modify metrics link", zap.Error(err))
		tc.FailNow()
	}

	tc.Log().Info(tmpnet.MetricsAvailableMessage,
		zap.String("uri", metricsLink),
	)
})

// removeQueryFilter removes specified label filters from Grafana dashboard URLs.
// This prevents "No Data" errors when metrics lack expected labels.
func removeQueryFilter(metricsURL string) (string, error) {
	parsedURL, err := url.Parse(metricsURL)
	if err != nil {
		return "", fmt.Errorf("parsing URL: %w", err)
	}

	query := parsedURL.Query()
	filters := query["var-filter"]
	if len(filters) == 0 {
		return metricsURL, nil
	}

	newFilters := make([]string, 0, len(filters))
	for _, filter := range filters {
		if !strings.Contains(filter, keyToFilter+"|=|") {
			newFilters = append(newFilters, filter)
		}
	}

	if len(newFilters) != len(filters) {
		query["var-filter"] = newFilters
		parsedURL.RawQuery = query.Encode()
	}

	return parsedURL.String(), nil
}
