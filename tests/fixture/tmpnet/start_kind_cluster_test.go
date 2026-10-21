// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHelmChart(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("HELM_CACHE_HOME", cacheDir)
	t.Setenv("AVALANCHEGO_KIND_OFFLINE", "true")

	chart, err := helmChart(ingressChartName, ingressChartVersion)
	require.ErrorIs(t, err, fs.ErrNotExist)
	require.Empty(t, chart)

	chartPath := filepath.Join(cacheDir, "repository", "traefik-41.4.0.tgz")
	require.NoError(t, os.Mkdir(filepath.Dir(chartPath), 0o755))
	require.NoError(t, os.WriteFile(chartPath, nil, 0o600))

	chart, err = helmChart(ingressChartName, ingressChartVersion)
	require.NoError(t, err)
	require.Equal(t, chartPath, chart)

	t.Setenv("AVALANCHEGO_KIND_OFFLINE", "")
	chart, err = helmChart(ingressChartName, ingressChartVersion)
	require.NoError(t, err)
	require.Equal(t, ingressChartName, chart)
}
