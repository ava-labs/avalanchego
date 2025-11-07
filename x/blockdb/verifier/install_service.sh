#!/bin/bash

# Block Verifier Systemd Service Installation Script
# This script installs the block verifier as a systemd service

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

print_header() {
    echo -e "${BLUE}=== $1 ===${NC}"
}

# Function to show usage
show_usage() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  --instance-name NAME     Service instance name (required for install/uninstall)"
    echo "  --endpoint1 URL          First RPC endpoint (required for install)"
    echo "  --endpoint2 URL          Second RPC endpoint (required for install)"
    echo "  --start-height HEIGHT    Starting block height (optional, defaults to latest)"
    echo "  --log-file PATH          Path to error log file (optional)"
    echo "  --workers COUNT          Number of concurrent workers (optional, default: 10)"
    echo "  --check-interval SECONDS Interval between checks for new blocks (optional, default: 120)"
    echo "  --max-transactions COUNT Maximum transactions to verify per block (optional, default: 5)"
    echo "  --uninstall              Uninstall the service instance"
    echo "  --help                   Show this help message"
    echo ""
    echo "Examples:"
    echo "  Install: $0 --instance-name mainnet --endpoint1 http://node1:9650/ext/bc/C/rpc --endpoint2 http://node2:9650/ext/bc/C/rpc"
    echo "  Install: $0 --instance-name testnet --endpoint1 http://test1:9650/ext/bc/C/rpc --endpoint2 http://test2:9650/ext/bc/C/rpc --start-height 1000000"
    echo "  Uninstall: $0 --instance-name mainnet --uninstall"
    echo ""
    echo "Service Management Commands:"
    echo "  sudo systemctl start block-verifier-{instance-name}"
    echo "  sudo systemctl stop block-verifier-{instance-name}"
    echo "  sudo systemctl restart block-verifier-{instance-name}"
    echo "  sudo systemctl status block-verifier-{instance-name}"
    echo "  sudo journalctl -u block-verifier-{instance-name} -f"
}

# Parse command line arguments
INSTANCE_NAME=""
ENDPOINT1=""
ENDPOINT2=""
START_HEIGHT=""
LOG_FILE=""
WORKERS=""
CHECK_INTERVAL=""
MAX_TRANSACTIONS=""
UNINSTALL=false

while [[ $# -gt 0 ]]; do
    case $1 in
        --instance-name)
            INSTANCE_NAME="$2"
            shift 2
            ;;
        --endpoint1)
            ENDPOINT1="$2"
            shift 2
            ;;
        --endpoint2)
            ENDPOINT2="$2"
            shift 2
            ;;
        --start-height)
            START_HEIGHT="$2"
            shift 2
            ;;
        --log-file)
            LOG_FILE="$2"
            shift 2
            ;;
        --workers)
            WORKERS="$2"
            shift 2
            ;;
        --check-interval)
            CHECK_INTERVAL="$2"
            shift 2
            ;;
        --max-transactions)
            MAX_TRANSACTIONS="$2"
            shift 2
            ;;
        --uninstall)
            UNINSTALL=true
            shift
            ;;
        --help)
            show_usage
            exit 0
            ;;
        *)
            print_error "Unknown option: $1"
            show_usage
            exit 1
            ;;
    esac
done

# Validate required arguments
if [[ -z "$INSTANCE_NAME" ]]; then
    print_error "Instance name is required. Use --instance-name"
    show_usage
    exit 1
fi

# Validate instance name (alphanumeric and hyphens only)
if [[ ! "$INSTANCE_NAME" =~ ^[a-zA-Z0-9-]+$ ]]; then
    print_error "Instance name must contain only alphanumeric characters and hyphens"
    exit 1
fi

# Check if running as root
if [[ $EUID -ne 0 ]]; then
    print_error "This script must be run as root (use sudo)"
    exit 1
fi

# Handle uninstall
if [[ "$UNINSTALL" == "true" ]]; then
    print_header "Block Verifier Service Uninstallation"

    SERVICE_NAME="block-verifier-${INSTANCE_NAME}"
    SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"

    print_status "Uninstalling service: $SERVICE_NAME"

    # Stop the service if it's running
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        print_status "Stopping service..."
        systemctl stop "$SERVICE_NAME"
    fi

    # Disable the service
    if systemctl is-enabled --quiet "$SERVICE_NAME" 2>/dev/null; then
        print_status "Disabling service..."
        systemctl disable "$SERVICE_NAME"
    fi

    # Remove service file
    if [[ -f "$SERVICE_FILE" ]]; then
        print_status "Removing service file..."
        rm -f "$SERVICE_FILE"
    fi

    # Reload systemd
    print_status "Reloading systemd daemon..."
    systemctl daemon-reload
    systemctl reset-failed

    print_header "Uninstallation Complete"
    print_status "Service '$SERVICE_NAME' has been uninstalled"
    print_status "Note: The binary at /usr/local/bin/block-verifier was not removed (may be used by other instances)"
    print_status "Note: Error log files were not removed and remain at their configured locations"
    exit 0
fi

# Validate install requirements
if [[ -z "$ENDPOINT1" ]]; then
    print_error "Endpoint1 is required for installation. Use --endpoint1"
    show_usage
    exit 1
fi

if [[ -z "$ENDPOINT2" ]]; then
    print_error "Endpoint2 is required for installation. Use --endpoint2"
    show_usage
    exit 1
fi

print_header "Block Verifier Service Installation"

# Check if Go is installed
if ! command -v go &> /dev/null; then
    print_error "Go is not installed. Please install Go first."
    exit 1
fi

# Get the directory where this script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVICE_NAME="block-verifier-${INSTANCE_NAME}"
BINARY_PATH="/usr/local/bin/block-verifier"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"

print_status "Installing service: $SERVICE_NAME"
print_status "Endpoint 1: $ENDPOINT1"
print_status "Endpoint 2: $ENDPOINT2"

# Create dedicated service user if it doesn't exist
if ! id -u block-verifier &>/dev/null; then
    print_status "Creating dedicated service user 'block-verifier'..."
    useradd -r -s /bin/false -M -d /nonexistent block-verifier
else
    print_status "Service user 'block-verifier' already exists"
fi

# Build the binary
print_status "Building block verifier binary..."
cd "$SCRIPT_DIR"
if ! go build -o block-verifier verifier.go; then
    print_error "Failed to build block verifier binary"
    exit 1
fi

# Install the binary
print_status "Installing binary to $BINARY_PATH..."
cp block-verifier "$BINARY_PATH"
chmod +x "$BINARY_PATH"

# Create systemd service file
print_status "Creating systemd service file..."

# Build command arguments
CMD_ARGS="--endpoint1 \"$ENDPOINT1\" --endpoint2 \"$ENDPOINT2\""

if [[ -n "$START_HEIGHT" ]]; then
    CMD_ARGS="$CMD_ARGS --start-height $START_HEIGHT"
fi

# Set default log file if not provided
if [[ -z "$LOG_FILE" ]]; then
    LOG_FILE="/var/log/block_verifier_${INSTANCE_NAME}_errors.log"
    print_status "Using default log file: $LOG_FILE"
fi

if [[ -n "$LOG_FILE" ]]; then
    CMD_ARGS="$CMD_ARGS --log-file \"$LOG_FILE\""
fi

if [[ -n "$WORKERS" ]]; then
    CMD_ARGS="$CMD_ARGS --workers $WORKERS"
fi

if [[ -n "$CHECK_INTERVAL" ]]; then
    CMD_ARGS="$CMD_ARGS --check-interval $CHECK_INTERVAL"
fi

if [[ -n "$MAX_TRANSACTIONS" ]]; then
    CMD_ARGS="$CMD_ARGS --max-transactions $MAX_TRANSACTIONS"
fi

# Determine ReadWritePaths based on log file location
READ_WRITE_PATHS="/var/log"
if [[ -n "$LOG_FILE" ]]; then
    LOG_DIR=$(dirname "$LOG_FILE")
    # Add log directory if it's not /var/log
    if [[ "$LOG_DIR" != "/var/log" ]]; then
        READ_WRITE_PATHS="$READ_WRITE_PATHS $LOG_DIR"
        print_status "Custom log path detected, adding $LOG_DIR to service permissions"
    fi
    # Ensure directory exists and has correct permissions
    mkdir -p "$LOG_DIR"
    chown block-verifier:block-verifier "$LOG_DIR"
    # Ensure the log file itself can be created (touch it, then set permissions)
    touch "$LOG_FILE" 2>/dev/null || true
    chown block-verifier:block-verifier "$LOG_FILE" 2>/dev/null || true
fi

# Create the service file
cat > "$SERVICE_FILE" << EOF
[Unit]
Description=Block Verifier Service - $INSTANCE_NAME
After=network.target
Wants=network.target

[Service]
Type=simple
User=block-verifier
Group=block-verifier
ExecStart=$BINARY_PATH $CMD_ARGS
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=block-verifier-$INSTANCE_NAME

# Security settings
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=$READ_WRITE_PATHS

[Install]
WantedBy=multi-user.target
EOF

# Configure journald for 60-day retention (if not already configured)
JOURNALD_CONF="/etc/systemd/journald.conf"
if ! grep -q "^MaxRetentionSec=60d" "$JOURNALD_CONF" 2>/dev/null; then
    print_status "Configuring systemd journal retention (60 days)..."

    # Backup original config
    if [[ ! -f "${JOURNALD_CONF}.backup" ]]; then
        cp "$JOURNALD_CONF" "${JOURNALD_CONF}.backup"
    fi

    # Check if MaxRetentionSec is commented or not present
    if grep -q "^#MaxRetentionSec=" "$JOURNALD_CONF" 2>/dev/null; then
        if [[ "$OSTYPE" == "darwin"* ]]; then
            sed -i '' 's/^#MaxRetentionSec=.*/MaxRetentionSec=60d/' "$JOURNALD_CONF"
        else
            sed -i 's/^#MaxRetentionSec=.*/MaxRetentionSec=60d/' "$JOURNALD_CONF"
        fi
    elif grep -q "^MaxRetentionSec=" "$JOURNALD_CONF" 2>/dev/null; then
        if [[ "$OSTYPE" == "darwin"* ]]; then
            sed -i '' 's/^MaxRetentionSec=.*/MaxRetentionSec=60d/' "$JOURNALD_CONF"
        else
            sed -i 's/^MaxRetentionSec=.*/MaxRetentionSec=60d/' "$JOURNALD_CONF"
        fi
    else
        # Add to [Journal] section
        if [[ "$OSTYPE" == "darwin"* ]]; then
            sed -i '' '/^\[Journal\]/a\'$'\n''MaxRetentionSec=60d' "$JOURNALD_CONF"
        else
            sed -i '/^\[Journal\]/a MaxRetentionSec=60d' "$JOURNALD_CONF"
        fi
    fi

    print_status "Journal retention configured. Restarting systemd-journald..."
    systemctl restart systemd-journald
else
    print_status "Journal retention already configured (60 days)"
fi

# Reload systemd and enable the service
print_status "Reloading systemd daemon..."
systemctl daemon-reload

print_status "Enabling service $SERVICE_NAME..."
systemctl enable "$SERVICE_NAME"

print_status "Starting service $SERVICE_NAME..."
systemctl start "$SERVICE_NAME"

# Wait a moment and check status
sleep 2
if systemctl is-active --quiet "$SERVICE_NAME"; then
    print_status "Service started successfully!"
else
    print_warning "Service may not have started properly. Check status with: systemctl status $SERVICE_NAME"
fi

print_header "Service Management Commands"
echo "Start service:     sudo systemctl start $SERVICE_NAME"
echo "Stop service:      sudo systemctl stop $SERVICE_NAME"
echo "Restart service:   sudo systemctl restart $SERVICE_NAME"
echo "Check status:      sudo systemctl status $SERVICE_NAME"
echo "View live logs:    sudo journalctl -u $SERVICE_NAME -f"
echo "View recent logs:  sudo journalctl -u $SERVICE_NAME --since \"1 hour ago\""
echo "View last 7 days:  sudo journalctl -u $SERVICE_NAME --since \"7 days ago\""
echo "Uninstall service: sudo $0 --instance-name $INSTANCE_NAME --uninstall"

print_header "Installation Complete"
print_status "Service '$SERVICE_NAME' has been installed and started"
print_status "Binary location: $BINARY_PATH"
print_status "Service file: $SERVICE_FILE"
print_status "Systemd journal retention: 60 days (configured globally)"
print_status "Error log retention: Unlimited (append-only, no rotation)"

# Clean up temporary binary
rm -f "$SCRIPT_DIR/block-verifier"

print_status "Installation completed successfully!"
