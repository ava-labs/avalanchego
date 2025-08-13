#!/usr/bin/env python3

import argparse
import json
import logging
import os
import sys
import pandas as pd
import plotly.graph_objects as go
from plotly.subplots import make_subplots
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
    name: str
    labels: Dict[str, str]
    job_name: Optional[str] = None


@dataclass
class QueryConfig:
    """Configuration for a single query."""
    query: str
    metric_name: str
    y_axis_label: str
    x_axis_label: Optional[str] = None


@dataclass
class PlotConfig:
    """Configuration for the plot."""
    queries: List[QueryConfig]
    runs: List[TestRun]
    dashboard_title: str
    output_file: str


class MultiPlot:
    """Multi-metric Prometheus visualization tool."""

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

    def _sanitize_labels(self, labels: Dict[str, str]) -> Dict[str, str]:
        """Convert all label values to strings."""
        if not labels:
            return {}
        return {k: str(v).lower() if isinstance(v, bool) else str(v) for k, v in labels.items()}

    def _apply_labels_to_query(self, base_query: str, labels: Dict[str, str]) -> str:
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
                self.logger.warning("no_prometheus_data", run_name=run.name, query=query)
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
                             query=query,
                             data_points=len(df),
                             value_range=f"{df['value'].min():.2f} - {df['value'].max():.2f}" if not df.empty else "empty")
            return df

        except Exception as e:
            self.logger.error("data_fetch_failed", run_name=run.name, query=query, error=str(e))
            return pd.DataFrame(columns=['timestamp', 'value'])

    def align_to_reference_timeline(self, data_df: pd.DataFrame, reference_df: pd.DataFrame) -> pd.DataFrame:
        """Align data timestamps to reference timeline for comparison."""
        if data_df.empty or reference_df.empty:
            return data_df

        data_start = data_df['timestamp'].min()
        reference_start = reference_df['timestamp'].min()

        aligned_df = data_df.copy()
        time_offset = (data_df['timestamp'] - data_start).dt.total_seconds()
        aligned_df['timestamp'] = reference_start + pd.to_timedelta(time_offset, unit='s')

        return aligned_df.sort_values('timestamp').reset_index(drop=True)

    def create_subplot_chart(self, fig, row: int, col: int, query_config: QueryConfig,
                             datasets: List[Tuple[TestRun, pd.DataFrame]]) -> None:
        """Add a single metric chart to the subplot figure."""
        colors = ['#2E86AB', '#A23B72', '#F18F01', '#C73E1D', '#4CAF50', '#9C27B0', '#FF9800']

        # Get reference dataset for alignment (first non-empty dataset)
        reference_df = None
        for run, df in datasets:
            if not df.empty:
                reference_df = df
                break

        if reference_df is None:
            self.logger.warning("no_reference_data", metric=query_config.metric_name)
            return

        for i, (run, df) in enumerate(datasets):
            if df.empty:
                continue

            color = colors[i % len(colors)]
            run_name = run.name

            # Align data to reference timeline
            aligned_df = self.align_to_reference_timeline(df, reference_df)

            fig.add_trace(
                go.Scatter(
                    x=aligned_df['timestamp'],
                    y=aligned_df['value'],
                    mode='lines+markers',
                    name=f'{run_name}',
                    line=dict(color=color, width=2),
                    marker=dict(size=4),
                    hovertemplate=f'<b>{run_name}</b><br>Time: %{{x}}<br>Value: %{{y:.2f}}<extra></extra>',
                    legendgroup=f'run_{i}',
                    showlegend=(row == 1 and col == 1)  # Only show legend on first chart
                ),
                row=row, col=col
            )

        # Update y-axis title for this subplot
        fig.update_yaxes(title_text=query_config.y_axis_label, row=row, col=col)

    def create_multi_metric_chart(self, plot_config: PlotConfig) -> go.Figure:
        """Create multi-metric visualization with subplots."""
        num_queries = len(plot_config.queries)
        subplot_titles = [q.metric_name for q in plot_config.queries]

        fig = make_subplots(
            rows=num_queries,
            cols=1,
            subplot_titles=subplot_titles,
            vertical_spacing=0.08,
            shared_xaxes=True
        )

        # Fetch data for all metrics and all runs
        for i, query_config in enumerate(plot_config.queries):
            query_datasets = []

            for run in plot_config.runs:
                df = self.fetch_metric_data(query_config.query, run)
                query_datasets.append((run, df))

            self.create_subplot_chart(fig, i + 1, 1, query_config, query_datasets)

        # Update layout
        fig.update_layout(
            title=f'<b>{plot_config.dashboard_title}</b>',
            hovermode='x unified',
            template='plotly_white',
            width=1200,
            height=400 * num_queries,
            showlegend=True
        )

        # Update x-axis title only on the bottom chart
        # Use the x_axis_label from the last query if available, otherwise default
        last_query = plot_config.queries[-1]
        x_axis_title = last_query.x_axis_label if last_query.x_axis_label else 'Time'
        fig.update_xaxes(title_text=x_axis_title, row=num_queries, col=1)

        # Add time range selector to bottom chart
        fig.update_xaxes(
            rangeslider_visible=True,
            rangeselector=dict(
                buttons=[
                    dict(count=1, label="1m", step="minute", stepmode="backward"),
                    dict(count=5, label="5m", step="minute", stepmode="backward"),
                    dict(count=10, label="10m", step="minute", stepmode="backward"),
                    dict(step="all")
                ]
            ),
            row=num_queries, col=1
        )

        return fig

    def visualize_multiple_metrics(self, config_dict: dict) -> str:
        """Main multi-metric visualization method."""

        # Parse configuration into strongly typed objects
        plot_config = self._parse_config(config_dict)

        self.logger.info("starting_multi_visualization",
                         num_metrics=len(plot_config.queries),
                         num_runs=len(plot_config.runs),
                         metrics=[q.metric_name for q in plot_config.queries])

        # Generate visualization
        chart = self.create_multi_metric_chart(plot_config)
        html_output = chart.to_html(include_plotlyjs='cdn')

        # Write output file
        try:
            with open(plot_config.output_file, 'w') as f:
                f.write(html_output)

            self.logger.info("multi_visualization_completed",
                             output_file=plot_config.output_file,
                             metrics_processed=len(plot_config.queries),
                             runs_processed=len(plot_config.runs))

            print(f"Multi-metric visualization saved to: {plot_config.output_file}")
            return html_output

        except Exception as e:
            self.logger.error("file_write_failed", output_file=plot_config.output_file, error=str(e))
            return ""

    def _parse_config(self, config_dict: dict) -> PlotConfig:
        """Parse configuration dictionary into strongly typed objects."""

        # Parse queries
        queries = []
        for i, query_item in enumerate(config_dict['queries']):
            if not isinstance(query_item, dict):
                raise ValueError(f"Query {i} must be an object")

            required_fields = ['query', 'metric_name', 'y_axis_label']
            for field in required_fields:
                if field not in query_item:
                    raise ValueError(f"Missing required field '{field}' in query {i}")

            query_config = QueryConfig(
                query=query_item['query'],
                metric_name=query_item['metric_name'],
                y_axis_label=query_item['y_axis_label'],
                x_axis_label=query_item.get('x_axis_label')
            )
            queries.append(query_config)

        # Parse runs
        runs = []
        for i, run_item in enumerate(config_dict['runs']):
            if not isinstance(run_item, dict):
                raise ValueError(f"Run {i} must be an object")

            required_fields = ['start_time', 'end_time', 'name', 'labels']
            for field in required_fields:
                if field not in run_item:
                    raise ValueError(f"Missing required field '{field}' in run {i}")

            test_run = TestRun(
                start_time=run_item['start_time'],
                end_time=run_item['end_time'],
                name=run_item['name'],
                labels=run_item['labels'],
                job_name=run_item.get('job_name')
            )
            runs.append(test_run)

        return PlotConfig(
            queries=queries,
            runs=runs,
            dashboard_title=config_dict.get('dashboard_title', 'Multi-Metric Analysis'),
            output_file=config_dict.get('output_file', 'multi_metric_visualization.html')
        )


def load_config(config_path: str) -> dict:
    """Load and validate JSON configuration."""
    try:
        with open(config_path, 'r') as f:
            config = json.load(f)

        # Validate required top-level fields
        required_fields = ['queries', 'runs']
        for field in required_fields:
            if field not in config:
                raise ValueError(f"Missing required field: {field}")

        if not isinstance(config['queries'], list) or len(config['queries']) == 0:
            raise ValueError("queries must be a non-empty array")

        if not isinstance(config['runs'], list) or len(config['runs']) == 0:
            raise ValueError("runs must be a non-empty array")

        return config

    except json.JSONDecodeError as e:
        raise ValueError(f"Invalid JSON in config file: {e}")
    except FileNotFoundError:
        raise ValueError(f"Config file not found: {config_path}")


def main():
    parser = argparse.ArgumentParser(description='Prometheus visualization tool')
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

        plot = MultiPlot(
            prometheus_url=args.prometheus_url,
            prometheus_auth=(prometheus_username, prometheus_password),
            step_size=args.step_size,
            timezone=args.timezone
        )

        result = plot.visualize_multiple_metrics(config)

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
