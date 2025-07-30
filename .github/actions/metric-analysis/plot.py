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
from datetime import datetime
from dataclasses import dataclass
import pytz
import structlog


@dataclass
class TestRun:
    """Represents a single test run."""
    start_time: int  # Unix timestamp (milliseconds)
    end_time: int  # Unix timestamp (milliseconds)
    name: Optional[str] = None
    labels: Optional[Dict[str, str]] = None


class Plot:
    """Prometheus metrics visualization tool."""

    def __init__(self, prometheus_url: str, prometheus_auth: tuple,
                 step_size: str = '15s', timezone: str = 'UTC'):
        self.prometheus_url = prometheus_url
        self.step_size = step_size
        self.logger = structlog.get_logger()

        try:
            self.display_tz = pytz.timezone(timezone)
        except pytz.UnknownTimeZoneError:
            self.logger.warning("timezone_fallback", requested=timezone, fallback="UTC")
            self.display_tz = pytz.UTC

        if not prometheus_auth[0] or not prometheus_auth[1]:
            raise ValueError("Missing Prometheus credentials")

        self.prom = PrometheusConnect(prometheus_url, auth=prometheus_auth)

    def _normalize_timestamp(self, timestamp: int) -> datetime:
        """Convert timestamp to UTC datetime, handling both seconds and milliseconds."""
        if timestamp > 1e10:
            return datetime.fromtimestamp(timestamp / 1000, tz=pytz.UTC)
        else:
            return datetime.fromtimestamp(timestamp, tz=pytz.UTC)

    def _format_timestamp_for_display(self, timestamp: int) -> str:
        """Format timestamp for display in the configured timezone."""
        utc_dt = self._normalize_timestamp(timestamp)
        display_dt = utc_dt.astimezone(self.display_tz)
        return f"{display_dt.strftime('%Y-%m-%d %H:%M:%S %Z')}"

    def _sanitize_labels(self, labels: Dict) -> Dict[str, str]:
        """Convert all label values to strings."""
        if not labels:
            return {}

        return {k: str(v).lower() if isinstance(v, bool) else str(v) for k, v in labels.items()}

    def _apply_labels_to_query(self, base_query: str, labels: Dict[str, str] = None) -> str:
        """Apply labels to the Prometheus query."""
        if not labels:
            return base_query

        sanitized_labels = self._sanitize_labels(labels)
        label_strings = [f'{k.lower()}="{v.lower()}"' for k, v in sanitized_labels.items()]
        label_selector = ','.join(label_strings)

        import re

        # Handle metrics with existing labels
        def replace_with_labels(match):
            metric_name = match.group(1)
            existing_labels = match.group(2).strip()
            if existing_labels:
                return f'{metric_name}{{{existing_labels},{label_selector}}}'
            else:
                return f'{metric_name}{{{label_selector}}}'

        result = re.sub(r'([a-zA-Z_][a-zA-Z0-9_]*)\{([^}]*)\}', replace_with_labels, base_query)

        # Handle metrics without labels
        def add_labels_to_bare_metrics(match):
            metric_name = match.group(1)
            bracket_part = match.group(2)
            return f'{metric_name}{{{label_selector}}}{bracket_part}'

        result = re.sub(r'([a-zA-Z_][a-zA-Z0-9_]*)(\[[^]]+\])', add_labels_to_bare_metrics, result)
        return result

    def fetch_metric_data(self, query: str, run: TestRun) -> pd.DataFrame:
        """Fetch metric data for a test run."""
        start_dt = self._normalize_timestamp(run.start_time)
        end_dt = self._normalize_timestamp(run.end_time)

        actual_query = self._apply_labels_to_query(query, run.labels)

        try:
            result = self.prom.custom_query_range(
                query=actual_query,
                start_time=start_dt,
                end_time=end_dt,
                step=self.step_size
            )

            if not result:
                self.logger.warning("no_prometheus_data", run_name=run.name)
                return pd.DataFrame(columns=['timestamp', 'value'])

            # Aggregate data points
            timestamp_values = {}
            for series in result:
                for timestamp, value in series['values']:
                    ts = float(timestamp)
                    val = float(value) if value else 0
                    timestamp_values[ts] = timestamp_values.get(ts, 0) + val

            # Convert to DataFrame
            data = []
            for timestamp, value in sorted(timestamp_values.items()):
                dt = datetime.fromtimestamp(timestamp, tz=pytz.UTC)
                data.append({'timestamp': dt, 'value': value})

            df = pd.DataFrame(data)
            self.logger.info("data_fetched",
                             run_name=run.name,
                             data_points=len(df),
                             value_range=f"{df['value'].min():.2f} - {df['value'].max():.2f}" if not df.empty else "empty")
            return df

        except Exception as e:
            self.logger.error("data_fetch_failed", run_name=run.name, error=str(e))
            return pd.DataFrame(columns=['timestamp', 'value'])

    def align_to_candidate_timeline(self, baseline_df: pd.DataFrame, candidate_df: pd.DataFrame) -> pd.DataFrame:
        """Align baseline timestamps to candidate timeline for comparison."""
        if baseline_df.empty or candidate_df.empty:
            return baseline_df

        baseline_start = baseline_df['timestamp'].min()
        candidate_start = candidate_df['timestamp'].min()

        aligned_df = baseline_df.copy()
        time_offset = (baseline_df['timestamp'] - baseline_start).dt.total_seconds()
        aligned_df['timestamp'] = candidate_start + pd.to_timedelta(time_offset, unit='s')

        return aligned_df.sort_values('timestamp').reset_index(drop=True)

    def create_metric_chart(self, candidate_data: pd.DataFrame, candidate_run: TestRun,
                            baseline_data: List[Tuple[TestRun, pd.DataFrame]], config: dict) -> go.Figure:
        """Create the interactive visualization chart."""
        fig = go.Figure()
        colors = ['#2E86AB', '#A23B72', '#F18F01', '#C73E1D', '#4CAF50', '#9C27B0', '#FF9800']

        # Add baseline traces
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

        # Add candidate trace
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

        # Layout
        fig.update_layout(
            title=f'<b>{config["metric_name"]} - Metric Visualization</b>',
            xaxis_title=config.get('x_axis_label', 'Time'),
            yaxis_title=config.get('y_axis_label', config['metric_name']),
            hovermode='x unified',
            template='plotly_white',
            width=1200,
            height=700,
            showlegend=True
        )

        # Add time range selector
        fig.update_xaxes(
            rangeslider_visible=True,
            rangeselector=dict(
                buttons=[
                    dict(count=1, label="1m", step="minute", stepmode="backward"),
                    dict(count=5, label="5m", step="minute", stepmode="backward"),
                    dict(count=10, label="10m", step="minute", stepmode="backward"),
                    dict(step="all")
                ]
            )
        )

        return fig

    def visualize_metrics(self, config: dict) -> str:
        """Main visualization method."""

        # 1. Initialize candidate run
        candidate_run = TestRun(**config['candidate'])
        self.logger.info("starting_visualization",
                         metric=config['metric_name'],
                         candidate=candidate_run.name)

        # 2. Fetch candidate data
        candidate_data = self.fetch_metric_data(config['query'], candidate_run)
        if candidate_data.empty:
            self.logger.error("no_candidate_data")
            return ""

        # 3. Process baselines - use provided timing instead of detection
        baselines = config.get('baselines', [])

        baseline_data = []
        for i, baseline_config in enumerate(baselines, 1):
            # Create TestRun directly from config (already has timing and labels)
            baseline_run = TestRun(**baseline_config)

            self.logger.info("processing_baseline",
                             index=i,
                             name=baseline_run.name,
                             start_time=self._format_timestamp_for_display(baseline_run.start_time),
                             end_time=self._format_timestamp_for_display(baseline_run.end_time))

            df = self.fetch_metric_data(config['query'], baseline_run)
            if df.empty:
                self.logger.warning("baseline_data_empty", index=i, name=baseline_run.name)
                continue

            aligned_df = self.align_to_candidate_timeline(df, candidate_data)
            baseline_data.append((baseline_run, aligned_df))

        if not baseline_data:
            self.logger.error("no_valid_baselines")
            return ""

        # 4. Generate visualization
        chart = self.create_metric_chart(candidate_data, candidate_run, baseline_data, config)
        html_output = chart.to_html(include_plotlyjs='cdn')

        # 5. Write output file
        output_file = config.get('output_file', 'metric_visualization.html')

        try:
            with open(output_file, 'w') as f:
                f.write(html_output)

            self.logger.info("visualization_completed",
                             output_file=output_file,
                             baselines_processed=len(baseline_data))

            print(f"Visualization saved to: {output_file}")
            return html_output

        except Exception as e:
            self.logger.error("file_write_failed", output_file=output_file, error=str(e))
            return ""


def load_config(config_path: str) -> dict:
    """Load and validate JSON configuration."""
    try:
        with open(config_path, 'r') as f:
            config = json.load(f)

        # Convert boolean values to strings
        def convert_values(obj):
            if isinstance(obj, dict):
                return {k: convert_values(v) for k, v in obj.items()}
            elif isinstance(obj, list):
                return [convert_values(item) for item in obj]
            elif isinstance(obj, bool):
                return str(obj).lower()
            else:
                return obj

        config = convert_values(config)

        # Validate required fields
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
    parser = argparse.ArgumentParser(description='Prometheus metrics visualization tool')
    parser.add_argument('--config', help='Path to JSON configuration file', required=True)
    parser.add_argument('--prometheus-url', help='Prometheus server URL', required=True)
    parser.add_argument('--step-size', default='15s', help='Prometheus query step size')
    parser.add_argument('--timezone', default='US/Eastern', help='Display timezone')
    parser.add_argument('--verbose', action='store_true', help='Enable verbose logging')

    args = parser.parse_args()

    # Configure logging
    if args.verbose:
        structlog.configure(
            processors=[
                structlog.stdlib.filter_by_level,
                structlog.stdlib.add_log_level,
                structlog.stdlib.PositionalArgumentsFormatter(),
                structlog.processors.TimeStamper(fmt="iso"),
                structlog.processors.format_exc_info,
                structlog.processors.JSONRenderer()
            ],
            context_class=dict,
            logger_factory=structlog.stdlib.LoggerFactory(),
            wrapper_class=structlog.stdlib.BoundLogger,
            cache_logger_on_first_use=True,
        )
        log_level = logging.DEBUG
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
        log_level = logging.INFO

    logging.basicConfig(level=log_level, format='%(message)s', handlers=[logging.StreamHandler(sys.stdout)])

    logger = structlog.get_logger()

    # Get credentials
    prometheus_username = os.getenv('PROMETHEUS_ID')
    prometheus_password = os.getenv('PROMETHEUS_PASSWORD')

    if not prometheus_username or not prometheus_password:
        logger.error("missing_credentials", required_vars=["PROMETHEUS_ID", "PROMETHEUS_PASSWORD"])
        sys.exit(1)

    try:
        # Load config and run visualization
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

        logger.info("success")

    except Exception as e:
        logger.error("application_error", error=str(e))
        if args.verbose:
            import traceback
            traceback.print_exc()
        sys.exit(1)


if __name__ == "__main__":
    main()
