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
import structlog


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

        self.logger = structlog.get_logger()

        try:
            self.display_tz = pytz.timezone(timezone)
        except pytz.UnknownTimeZoneError:
            self.logger.warning("timezone_fallback",
                                requested=timezone, fallback="UTC")
            self.display_tz = pytz.UTC

        if not prometheus_auth[0] or not prometheus_auth[1]:
            raise ValueError("Missing Prometheus credentials")

        self.prom = PrometheusConnect(prometheus_url, auth=prometheus_auth)

    # Prometheus stores all timestamps in UTC as Unix seconds
    def _normalize_timestamp(self, timestamp: int) -> datetime:
        if timestamp > 1e10:
            # Milliseconds - convert to seconds
            return datetime.fromtimestamp(timestamp / 1000, tz=pytz.UTC)
        else:
            # Already in seconds
            return datetime.fromtimestamp(timestamp, tz=pytz.UTC)

    def _format_timestamp_for_display(self, timestamp: int) -> str:
        utc_dt = self._normalize_timestamp(timestamp)
        display_dt = utc_dt.astimezone(self.display_tz)
        return f"{display_dt.strftime('%Y-%m-%d %H:%M:%S %Z')}"

    def _sanitize_labels(self, labels: Dict) -> Dict[str, str]:
        """Convert all label values to strings, handling booleans properly."""
        if not labels:
            return {}

        sanitized = {}
        for key, value in labels.items():
            if isinstance(value, bool):
                sanitized[key] = str(value).lower()  # Convert True/False to "true"/"false"
            else:
              sanitized[key] = str(value)
        return sanitized

    def _apply_labels_to_query(self, base_query: str, labels: Dict[str, str] = None) -> str:
        """Apply labels to the base Prometheus query if labels are provided."""
        if not labels:
            return base_query

        sanitized_labels = self._sanitize_labels(labels)

        # Convert labels to Prometheus label syntax
        label_strings = []
        for key, value in sanitized_labels.items():
            key_str = str(key).lower()
            value_str = str(value).lower()
            label_strings.append(f'{key_str}="{value_str}"')

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

        result = re.sub(r'([a-zA-Z_][a-zA-Z0-9_]*)\{([^}]*)\}', replace_with_labels, base_query)

        # Handle metrics without labels - look for metric names followed by [ (for range queries)
        def add_labels_to_bare_metrics(match):
            metric_name = match.group(1)
            bracket_part = match.group(2)  # This is the [1m] part
            return f'{metric_name}{{{label_selector}}}{bracket_part}'

        # Match metric names followed by time range brackets like [1m], [5s], etc.
        result = re.sub(r'([a-zA-Z_][a-zA-Z0-9_]*)(\[[^]]+\])', add_labels_to_bare_metrics, result)

        return result

    def detect_baseline_periods(self, query: str, labels: Dict[str, str], lookback_days: int = 7) -> Optional[TestRun]:
        now = datetime.now(tz=pytz.UTC)
        start = now - timedelta(days=lookback_days)

        actual_query = self._apply_labels_to_query(query, labels)

        try:
            initial_results = self.prom.custom_query_range(
                query=actual_query,
                start_time=start,
                end_time=now,
                step="5m",
            )

            if not initial_results:
                return None

            timestamps = []
            for result in initial_results:
                values = result.get('values', [])
                for value in values:
                    timestamp = float(value[0])
                    timestamps.append(timestamp)

            if not timestamps:
                return None

            min_timestamp = min(timestamps)
            max_timestamp = max(timestamps)

            # Ensure we have a reasonable time range
            if min_timestamp >= max_timestamp:
                max_timestamp = min_timestamp + timedelta(minutes=15).total_seconds()

            # Add ±15s variance
            min_datetime = datetime.fromtimestamp(min_timestamp - 15, tz=pytz.UTC)
            max_datetime = datetime.fromtimestamp(max_timestamp + 15, tz=pytz.UTC)

            return TestRun(
                start_time=int(min_datetime.timestamp() * 1000),
                end_time=int(max_datetime.timestamp() * 1000),
                name=f"Baseline ({min_datetime.strftime('%m/%d %H:%M')})",
                labels=labels
            )

        except Exception as e:
            self.logger.debug("baseline_detection_failed", error=str(e))
            return None

    def fetch_metric_data(self, query: str, run: TestRun) -> pd.DataFrame:
        """Fetch metric data for a specific test run."""
        # Convert timestamps to UTC datetime objects (Prometheus requirement)
        start_dt = self._normalize_timestamp(run.start_time)
        end_dt = self._normalize_timestamp(run.end_time)
        duration = end_dt - start_dt

        actual_query = self._apply_labels_to_query(query, run.labels)

        try:
            result = self.prom.custom_query_range(
                query=actual_query,
                start_time=start_dt,
                end_time=end_dt,
                step=self.step_size
            )

            if not result:
                return pd.DataFrame(columns=['timestamp', 'value'])

            timestamp_values = {}
            for series in result:
                for timestamp, value in series['values']:
                    ts = float(timestamp)
                    val = float(value) if value else 0
                    timestamp_values[ts] = timestamp_values.get(ts, 0) + val

            data = []
            for timestamp, value in sorted(timestamp_values.items()):
                dt = datetime.fromtimestamp(timestamp, tz=pytz.UTC)
                data.append({'timestamp': dt, 'value': value})

            df = pd.DataFrame(data)
            return df

        except Exception as e:
            self.logger.error("data_fetch_failed", error=str(e))
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

        # 1. Initialize candidate run
        candidate_run = TestRun(**config['candidate'])
        self.logger.info("visualization_started",
                         metric=config['metric_name'],
                         candidate=candidate_run.name or 'Unnamed')

        # 2. Fetch candidate data first
        candidate_data = self.fetch_metric_data(config['query'], candidate_run)
        if candidate_data.empty:
            self.logger.error("candidate_data_empty")
            return ""

        start_time = self._format_timestamp_for_display(candidate_run.start_time)
        end_time = self._format_timestamp_for_display(candidate_run.end_time)
        duration = self._normalize_timestamp(candidate_run.end_time) - self._normalize_timestamp(
            candidate_run.start_time)

        self.logger.info("candidate_data_loaded",
                         name=candidate_run.name or 'Unnamed',
                         start_time=start_time,
                         end_time=end_time,
                         duration=str(duration),
                         data_points=len(candidate_data),
                         value_min=round(candidate_data['value'].min(), 2),
                         value_max=round(candidate_data['value'].max(), 2))

        # 3. Detect and fetch baseline data
        baseline_runs = []
        baselines = config.get('baselines', [])

        self.logger.info("baseline_detection_started", count=len(baselines))

        for i, baseline in enumerate(baselines, 1):
            detected_baseline = self.detect_baseline_periods(
                query=config['query'],
                labels=baseline['labels'],
            )
            if not detected_baseline:
                self.logger.warning("baseline_detection_failed", baseline_index=i)
                continue
            baseline_runs.append(detected_baseline)

        if not baseline_runs:
            self.logger.error("no_baselines_detected")
            return ""

        # 4. Process baseline data
        baseline_data = []
        for i, baseline_run in enumerate(baseline_runs, 1):
            df = self.fetch_metric_data(config['query'], baseline_run)
            if df.empty:
                self.logger.warning("baseline_data_empty", baseline_index=i)
                continue

            aligned_df = self.align_to_candidate_timeline(df, candidate_data)
            baseline_data.append((baseline_run, aligned_df))

            start_time = self._format_timestamp_for_display(baseline_run.start_time)
            self.logger.info("baseline_data_loaded",
                             baseline_index=i,
                             name=baseline_run.name,
                             start_time=start_time,
                             data_points=len(df),
                             value_min=round(df['value'].min(), 2),
                             value_max=round(df['value'].max(), 2))

        if not baseline_data:
            self.logger.error("no_valid_baseline_data")
            return ""

        # 5. Generate visualization
        chart = self.create_metric_chart(candidate_data, candidate_run, baseline_data, config)
        html_output = chart.to_html(include_plotlyjs='cdn')

        output_file = config.get('output_file', 'metric_visualization.html')
        with open(output_file, 'w') as f:
            f.write(html_output)

        self.logger.info("visualization_completed",
                         output_file=output_file,
                         baselines_processed=len(baseline_data))
        return html_output


def _convert_config_values(obj):
    """Recursively convert boolean values to strings in configuration."""
    if isinstance(obj, dict):
        return {k: _convert_config_values(v) for k, v in obj.items()}
    elif isinstance(obj, list):
        return [_convert_config_values(item) for item in obj]
    elif isinstance(obj, bool):
        return str(obj).lower()  # Convert True/False to "true"/"false"
    else:
        return obj


def load_config(config_path: str) -> dict:
    """Load and validate JSON configuration."""
    try:
        with open(config_path, 'r') as f:
            config = json.load(f)

        config = _convert_config_values(config)

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

    parser.add_argument('--config', help='Path to JSON configuration file', default='config.json')
    parser.add_argument('--prometheus-url', help='Prometheus server URL',
                        default='https://prometheus-poc.avax-dev.network')
    parser.add_argument('--step-size', default='15s',
                        help='Prometheus query step size (default: 15s)')
    parser.add_argument('--timezone', default='US/Eastern',
                        help='Display timezone (default: US/Eastern)')
    parser.add_argument('--verbose', action='store_true',
                        help='Enable verbose logging', default=False)

    args = parser.parse_args()

    if args.verbose:
        structlog.configure(
            processors=[
                structlog.stdlib.filter_by_level,
                structlog.stdlib.add_logger_name,
                structlog.stdlib.add_log_level,
                structlog.stdlib.PositionalArgumentsFormatter(),
                structlog.processors.TimeStamper(fmt="iso"),
                structlog.processors.StackInfoRenderer(),
                structlog.processors.format_exc_info,
                structlog.processors.JSONRenderer()
            ],
            context_class=dict,
            logger_factory=structlog.stdlib.LoggerFactory(),
            wrapper_class=structlog.stdlib.BoundLogger,
            cache_logger_on_first_use=True,
        )
    else:
        structlog.configure(
            processors=[
                structlog.stdlib.filter_by_level,
                structlog.stdlib.add_log_level,
                structlog.stdlib.PositionalArgumentsFormatter(),
                structlog.processors.TimeStamper(fmt="%H:%M:%S"),
                structlog.dev.ConsoleRenderer(colors=False)
            ],
            context_class=dict,
            logger_factory=structlog.stdlib.LoggerFactory(),
            wrapper_class=structlog.stdlib.BoundLogger,
            cache_logger_on_first_use=True,
        )

    log_level = logging.DEBUG if args.verbose else logging.INFO
    logging.basicConfig(
        level=log_level,
        format='%(message)s',
        handlers=[logging.StreamHandler(sys.stdout)]
    )

    logger = structlog.get_logger()

    prometheus_username = os.getenv('PROMETHEUS_ID')
    prometheus_password = os.getenv('PROMETHEUS_PASSWORD')

    if not prometheus_username or not prometheus_password:
        logger.error("missing_credentials",
                     required_vars=["PROMETHEUS_ID", "PROMETHEUS_PASSWORD"])
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

        if not result:
            logger.error("visualization_failed")
            sys.exit(1)

    except Exception as e:
        logger.error("application_error", error=str(e))
        if args.verbose:
            import traceback
            traceback.print_exc()
        sys.exit(1)


if __name__ == "__main__":
    main()
