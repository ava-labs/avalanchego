#!/usr/bin/env python3

import argparse
import json
import logging
import os
import sys
import pandas as pd
import plotly.graph_objects as go
from prometheus_api_client import PrometheusConnect
from typing import List, Dict, Optional, Tuple
from datetime import datetime, timedelta
from dataclasses import dataclass
import pytz


@dataclass
class TestRun:
    """Represents a single test run."""
    start_time: int  # Unix timestamp (seconds or milliseconds)
    end_time: int  # Unix timestamp (seconds or milliseconds)
    name: Optional[str] = None  # Human-readable name for display
    labels: Optional[Dict[str, str]] = None  # Run-specific labels


class Plot:
    """
    A tool for displaying advanced Prometheus metrics.
    Uses JSON configuration to specify runs and their metadata.
    """

    def __init__(self, prometheus_url: str, prometheus_auth: tuple,
                 step_size: str = '15s', timezone: str = 'UTC'):
        self.prometheus_url = prometheus_url
        self.step_size = step_size

        # Set up logging
        self.logger = logging.getLogger(__name__)

        # Set up timezone for display (Prometheus stores everything in UTC internally)
        try:
            self.display_tz = pytz.timezone(timezone)
        except pytz.UnknownTimeZoneError:
            self.logger.warning(f"Unknown timezone '{timezone}', using UTC")
            self.display_tz = pytz.UTC

        if not prometheus_auth[0] or not prometheus_auth[1]:
            raise ValueError("Missing Prometheus credentials")

        self.prom = PrometheusConnect(prometheus_url, auth=prometheus_auth)

        self.logger.info(f"Initialized Metric Visualizer")
        self.logger.info(f"  Prometheus URL: {prometheus_url}")
        self.logger.info(f"  Step size: {step_size}")
        self.logger.info(f"  Display timezone: {timezone}")

    def _normalize_timestamp(self, timestamp: int) -> datetime:
        """Convert timestamp to UTC datetime object."""
        # Prometheus stores all timestamps in UTC as Unix seconds
        # Check if input timestamp is in milliseconds or seconds
        if timestamp > 1e10:
            # Milliseconds - convert to seconds
            return datetime.fromtimestamp(timestamp / 1000, tz=pytz.UTC)
        else:
            # Already in seconds
            return datetime.fromtimestamp(timestamp, tz=pytz.UTC)

    def _format_timestamp_for_display(self, timestamp: int) -> str:
        """Format timestamp for human-readable display."""
        utc_dt = self._normalize_timestamp(timestamp)
        display_dt = utc_dt.astimezone(self.display_tz)
        return f"{display_dt.strftime('%Y-%m-%d %H:%M:%S %Z')}"

    def _apply_labels_to_query(self, base_query: str, labels: Dict[str, str] = None) -> str:
        """Apply labels to the base Prometheus query if labels are provided."""
        if not labels:
            return base_query

        # Convert labels to Prometheus label syntax
        label_strings = [f'{key}="{value}"' for key, value in labels.items()]
        label_selector = ','.join(label_strings)

        import re

        # Handle metrics that already have labels: metric_name{existing_labels}
        def replace_with_labels(match):
            metric_name = match.group(1)
            existing_labels = match.group(2).strip()

            if existing_labels:
                return f'{metric_name}{{{existing_labels},{label_selector}}}'
            else:
                return f'{metric_name}{{{label_selector}}}'

        # Apply to metrics with existing labels
        result = re.sub(r'([a-zA-Z_][a-zA-Z0-9_]*)\{([^}]*)\}', replace_with_labels, base_query)

        # Handle metrics without labels - look for metric names followed by [ (for range queries)
        def add_labels_to_bare_metrics(match):
            metric_name = match.group(1)
            bracket_part = match.group(2)  # This is the [1m] part
            return f'{metric_name}{{{label_selector}}}{bracket_part}'

        # Match metric names followed by time range brackets like [1m], [5s], etc.
        result = re.sub(r'([a-zA-Z_][a-zA-Z0-9_]*)(\[[^]]+\])', add_labels_to_bare_metrics, result)

        return result

    def detect_baseline_periods(self, query: str, labels: Dict[str, str], lookback_days: int = 7) -> TestRun:
        self.logger.info(f"Dynamically detecting baseline periods...")
        self.logger.info(f"Looking back {lookback_days} days for data")

        now = datetime.now(tz=pytz.UTC)
        start = now - timedelta(days=lookback_days)

        actual_query = self._apply_labels_to_query(query, labels)
        self.logger.debug(f"Query for baseline detection: {actual_query}")

        try:
            initial_results = self.prom.custom_query_range(
                query=actual_query,
                start_time=start,
                end_time=now,
                step="5m",
            )

            if not initial_results:
                self.logger.warning("No initial results found for baseline detection")
                return []

            timestamps = []
            for result in initial_results:
                values = result.get('values', [])
                for value in values:
                    timestamp = float(value[0])
                    timestamps.append(timestamp)

            if not timestamps:
                self.logger.warning("No timestamps found in results")
                return []

            self.logger.info(f"Found {len(timestamps)} data points in historical scan")

            min_timestamp = min(timestamps)
            max_timestamp = max(timestamps)

            # Ensure we have a reasonable time range
            if min_timestamp >= max_timestamp:
                max_timestamp = min_timestamp + timedelta(minutes=15).total_seconds()

            # Add ±15s variance
            min_datetime = datetime.fromtimestamp(min_timestamp - 15, tz=pytz.UTC)
            max_datetime = datetime.fromtimestamp(max_timestamp + 15, tz=pytz.UTC)

            self.logger.info(f"Baseline period: {min_datetime.isoformat()} to {max_datetime.isoformat()}")

            return TestRun(
                start_time=int(min_datetime.timestamp() * 1000),
                end_time=int(max_datetime.timestamp() * 1000),
                name=f"Baseline 1 ({min_datetime.strftime('%m/%d %H:%M')})",
                labels=labels
            )

        except Exception as e:
            self.logger.error(f"Error in baseline detection: {e}")
            import traceback
            traceback.print_exc()
            return []

    def fetch_metric_data(self, query: str, run: TestRun) -> pd.DataFrame:
        """Fetch metric data for a specific test run."""
        # Convert timestamps to UTC datetime objects (Prometheus requirement)
        start_dt = self._normalize_timestamp(run.start_time)
        end_dt = self._normalize_timestamp(run.end_time)
        duration = end_dt - start_dt

        actual_query = self._apply_labels_to_query(query, run.labels)

        run_name = run.name or (run.labels.get('gh_run_id') if run.labels else None) or "Unnamed Run"

        self.logger.info(f"Fetching data for: {run_name}")
        self.logger.info(
            f"  Time range: {self._format_timestamp_for_display(run.start_time)} to {self._format_timestamp_for_display(run.end_time)}")
        self.logger.info(f"  Duration: {duration}")
        if run.labels:
            self.logger.debug(f"  Labels: {run.labels}")
        self.logger.debug(f"  Query: {actual_query}")

        try:
            result = self.prom.custom_query_range(
                query=actual_query,
                start_time=start_dt,
                end_time=end_dt,
                step=self.step_size
            )

            if not result:
                self.logger.warning(f"No data found for {run_name}")
                return pd.DataFrame(columns=['timestamp', 'value'])

            # Process results - combine all series by summing values at each timestamp
            timestamp_values = {}
            for series in result:
                for timestamp, value in series['values']:
                    ts = float(timestamp)
                    val = float(value) if value else 0
                    timestamp_values[ts] = timestamp_values.get(ts, 0) + val

            # Create DataFrame with UTC timestamps (Prometheus native format)
            data = []
            for timestamp, value in sorted(timestamp_values.items()):
                dt = datetime.fromtimestamp(timestamp, tz=pytz.UTC)
                data.append({'timestamp': dt, 'value': value})

            df = pd.DataFrame(data)

            if not df.empty:
                self.logger.info(f"  Retrieved {len(df)} data points")
                self.logger.debug(f"  Value range: {df['value'].min():.2f} to {df['value'].max():.2f}")
            else:
                self.logger.warning(f"No valid data points for {run_name}")

            return df

        except Exception as e:
            self.logger.error(f"Error fetching data for {run_name}: {e}")
            return pd.DataFrame(columns=['timestamp', 'value'])

    def align_to_candidate_timeline(self, baseline_df: pd.DataFrame, candidate_df: pd.DataFrame) -> pd.DataFrame:
        """Align baseline timestamps to candidate timeline for visual comparison."""
        if baseline_df.empty or candidate_df.empty:
            return baseline_df

        baseline_start = baseline_df['timestamp'].min()
        candidate_start = candidate_df['timestamp'].min()

        # Calculate time offset and apply to baseline
        aligned_df = baseline_df.copy()
        time_offset = (baseline_df['timestamp'] - baseline_start).dt.total_seconds()
        aligned_df['timestamp'] = candidate_start + pd.to_timedelta(time_offset, unit='s')

        return aligned_df.sort_values('timestamp').reset_index(drop=True)

    def create_metric_chart(self, candidate_data: pd.DataFrame, candidate_run: TestRun,
                            baseline_data: List[tuple], config: dict) -> go.Figure:
        """Create interactive metric visualization chart."""
        fig = go.Figure()

        # Colors for baseline runs
        colors = ['#2E86AB', '#A23B72', '#F18F01', '#C73E1D', '#4CAF50', '#9C27B0', '#FF9800']

        for i, (baseline_run, df) in enumerate(baseline_data):
            if df.empty:
                continue

            color = colors[i % len(colors)]
            run_name = baseline_run.name or f"Baseline {i + 1}"

            fig.add_trace(go.Scatter(
                x=df['timestamp'],
                y=df['value'],
                mode='lines+markers',
                name=f'Baseline: {run_name}',
                line=dict(color=color, width=2),
                marker=dict(size=4),
                hovertemplate=f'<b>{run_name}</b><br>Time: %{{x}}<br>Value: %{{y:.2f}}<extra></extra>'
            ))

        if not candidate_data.empty:
            candidate_name = candidate_run.name or "Candidate"

            fig.add_trace(go.Scatter(
                x=candidate_data['timestamp'],
                y=candidate_data['value'],
                mode='lines+markers',
                name=f'Candidate: {candidate_name}',
                line=dict(color='#E53E3E', width=3),
                marker=dict(size=6),
                hovertemplate=f'<b>{candidate_name}</b><br>Time: %{{x}}<br>Value: %{{y:.2f}}<extra></extra>'
            ))

        # Get axis labels from config
        x_axis_label = config.get('x_axis_label', 'Time')
        y_axis_label = config.get('y_axis_label', config['metric_name'])

        # Layout
        fig.update_layout(
            title=f'<b>{config["metric_name"]} - Metric Visualization</b>',
            xaxis_title=x_axis_label,
            yaxis_title=y_axis_label,
            hovermode='x unified',
            template='plotly_white',
            width=1200, height=700,
            showlegend=True
        )

        # Add time range selector
        fig.update_xaxes(
            rangeslider_visible=True,
            rangeselector=dict(
                buttons=list([
                    dict(count=1, label="1m", step="minute", stepmode="backward"),
                    dict(count=5, label="5m", step="minute", stepmode="backward"),
                    dict(count=10, label="10m", step="minute", stepmode="backward"),
                    dict(step="all")
                ])
            )
        )

        return fig

    def visualize_metrics(self, config: dict) -> str:
        """Visualize metrics based on JSON configuration."""

        candidate_run = TestRun(**config['candidate'])
        self.logger.info(f"Candidate: {candidate_run.name or 'Unnamed Candidate'}")

        # Check if we need dynamic baseline detection
        baseline_runs = []
        baselines = config['baselines']

        for baseline in baselines:
            detected_baselines = self.detect_baseline_periods(
                query=config['query'],
                labels=baseline['labels'],
            )
            baseline_runs.append(detected_baselines)


        # if config.get('baselines') and len(config['baselines']) > 0:
        #     first_baseline = config['baselines'][0]
        #
        #     # Only do dynamic detection if start_time key exists and is explicitly null
        #     if 'start_time' in first_baseline and first_baseline['start_time'] is None:
        #         self.logger.info("Performing dynamic baseline detection...")
        #
        #         # Calculate candidate duration
        #         candidate_duration = (candidate_run.end_time - candidate_run.start_time) // 1000  # Convert to seconds
        #         baseline_count = len(config['baselines'])
        #
        #         # Use labels from candidate for baseline detection
        #         baseline_labels = candidate_run.labels.copy() if candidate_run.labels else {}
        #         # Remove run-specific labels for baseline search
        #         baseline_labels.pop('gh_run_id', None)
        #         baseline_labels.pop('gh_run_attempt', None)
        #
        #         detected_baselines = self.detect_baseline_periods(
        #             query=config['query'],
        #             labels=baseline_labels,
        #             candidate_duration=candidate_duration,
        #             baseline_count=baseline_count
        #         )
        #
        #         baseline_runs = detected_baselines
        #     else:
        #         # Use provided baseline configurations with candidate timing
        #         self.logger.info("Using provided baseline configurations")
        #         for baseline_config in config['baselines']:
        #             # Use candidate timing for baselines that have labels but no timing
        #             baseline_with_timing = baseline_config.copy()
        #             if 'start_time' not in baseline_with_timing:
        #                 baseline_with_timing['start_time'] = candidate_run.start_time
        #             if 'end_time' not in baseline_with_timing:
        #                 baseline_with_timing['end_time'] = candidate_run.end_time
        #
        #             baseline_runs.append(TestRun(**baseline_with_timing))

        # Fetch candidate data
        candidate_data = self.fetch_metric_data(config['query'], candidate_run)

        if candidate_data.empty:
            self.logger.error("No candidate data found")
            return ""

        # Fetch baseline data
        baseline_data = []
        for baseline_run in baseline_runs:
            df = self.fetch_metric_data(config['query'], baseline_run)
            if not df.empty:
                aligned_df = self.align_to_candidate_timeline(df, candidate_data)
                baseline_data.append((baseline_run, aligned_df))

        if not baseline_data:
            self.logger.error("No baseline data found")
            return ""

        chart = self.create_metric_chart(candidate_data, candidate_run, baseline_data, config)
        html_output = chart.to_html(include_plotlyjs='cdn')

        output_file = config.get('output_file', 'metric_visualization.html')
        with open(output_file, 'w') as f:
            f.write(html_output)

        self.logger.info(f"Visualization saved to: {output_file}")
        return html_output


def load_config(config_path: str) -> dict:
    """Load and validate JSON configuration."""
    try:
        with open(config_path, 'r') as f:
            config = json.load(f)

        required_fields = ['query', 'metric_name', 'candidate']
        for field in required_fields:
            if field not in config:
                raise ValueError(f"Missing required field: {field}")

        candidate_fields = ['start_time', 'end_time']
        for field in candidate_fields:
            if field not in config['candidate']:
                raise ValueError(f"Missing candidate field: {field}")

        return config

    except json.JSONDecodeError as e:
        raise ValueError(f"Invalid JSON in config file: {e}")
    except FileNotFoundError:
        raise ValueError(f"Config file not found: {config_path}")


def main():
    parser = argparse.ArgumentParser(
        description='JSON-based metric visualization tool',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Example JSON Configuration:
{
  "query": "sum by (chain) (rate(avalanche_evm_eth_chain_block_gas_used_processed[1m])) / 1e+06",
  "metric_name": "Gas Used Rate",
  "x_axis_label": "Time",
  "y_axis_label": "Gas Usage (mGAS/sec)",
  "candidate": {
    "start_time": 1753361293000,
    "end_time": 1753362193000,
    "name": "Current PR Test",
    "labels": {
      "gh_run_id": "16497290497",
      "gh_job_id": "c-chain-reexecution",
      "chain": "C"
    }
  },
  "baselines": [
    {
      "name": "Baseline 1",
      "labels": {
        "gh_run_id": "16475196790",
        "gh_job_id": "c-chain-reexecution",
        "chain": "C"
      }
    }
  ],
  "output_file": "metrics.html"
}
        """
    )

    parser.add_argument('--config', help='Path to JSON configuration file', required=True)
    parser.add_argument('--prometheus-url', help='Prometheus server URL', required=True)
    parser.add_argument('--step-size', default='15s',
                        help='Prometheus query step size (default: 15s)')
    parser.add_argument('--timezone', default='US/Eastern',
                        help='Display timezone (default: US/Eastern)')
    parser.add_argument('--verbose', action='store_true',
                        help='Enable verbose logging', default=True)

    args = parser.parse_args()

    log_level = logging.DEBUG if args.verbose else logging.INFO
    logging.basicConfig(
        level=log_level,
        format='%(asctime)s - %(levelname)s - %(message)s',
        handlers=[logging.StreamHandler(sys.stdout)]
    )

    logger = logging.getLogger(__name__)

    prometheus_username = os.getenv('PROMETHEUS_ID')
    prometheus_password = os.getenv('PROMETHEUS_PASSWORD')

    if not prometheus_username or not prometheus_password:
        logger.error("Missing Prometheus credentials")
        logger.error("Set PROMETHEUS_ID and PROMETHEUS_PASSWORD environment variables")
        sys.exit(1)

    try:
        config = load_config(args.config)

        plot = Plot(
            prometheus_url=args.prometheus_url,
            prometheus_auth=(prometheus_username, prometheus_password),
            step_size=args.step_size,
            timezone=args.timezone
        )

        result = plot.visualize_metrics(config)

        if result:
            logger.info("Metric visualization completed successfully")
        else:
            logger.error("Metric visualization failed")
            sys.exit(1)

    except Exception as e:
        logger.error(f"Error: {e}")
        if args.verbose:
            import traceback
            traceback.print_exc()
        sys.exit(1)


if __name__ == "__main__":
    main()
