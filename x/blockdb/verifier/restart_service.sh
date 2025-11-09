#!/bin/bash
# restart_service.sh - Build and restart the block verifier service

set -e  # Exit on any error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
INSTANCE_NAME="${1:-mainnet}"
SERVICE_NAME="block-verifier-${INSTANCE_NAME}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BINARY_NAME="block-verifier"
INSTALL_PATH="/usr/local/bin/${BINARY_NAME}"

echo -e "${YELLOW}=== Block Verifier Build & Restart ===${NC}"
echo "Instance: ${INSTANCE_NAME}"
echo "Service: ${SERVICE_NAME}"
echo ""

# Step 1: Stop the service
echo -e "${YELLOW}[1/6]${NC} Stopping service..."
if sudo systemctl is-active --quiet "${SERVICE_NAME}"; then
    sudo systemctl stop "${SERVICE_NAME}"
    echo -e "${GREEN}✓${NC} Service stopped"
else
    echo -e "${YELLOW}!${NC} Service was not running"
fi
echo ""

# Step 2: Clear error log
echo -e "${YELLOW}[2/7]${NC} Clearing error log..."
ERROR_LOG="/var/log/block_verifier_${INSTANCE_NAME}_errors.log"
if [ -f "${ERROR_LOG}" ]; then
    # Backup the old log with timestamp
    BACKUP_LOG="${ERROR_LOG}.$(date +%Y%m%d_%H%M%S).bak"
    sudo cp "${ERROR_LOG}" "${BACKUP_LOG}"
    echo "  Backed up to: ${BACKUP_LOG}"
    # Clear the log
    sudo truncate -s 0 "${ERROR_LOG}"
    echo -e "${GREEN}✓${NC} Error log cleared"
else
    echo -e "${YELLOW}!${NC} Error log doesn't exist yet (will be created on start)"
fi
echo ""

# Step 3: Build the verifier
echo -e "${YELLOW}[3/7]${NC} Building verifier..."
cd "${SCRIPT_DIR}"
if go build -buildvcs=false -o "${BINARY_NAME}" verifier.go; then
    echo -e "${GREEN}✓${NC} Build successful"
else
    echo -e "${RED}✗${NC} Build failed"
    exit 1
fi
echo ""

# Step 4: Install the binary
echo -e "${YELLOW}[4/7]${NC} Installing binary to ${INSTALL_PATH}..."
if sudo cp "${BINARY_NAME}" "${INSTALL_PATH}"; then
    echo -e "${GREEN}✓${NC} Binary installed"
else
    echo -e "${RED}✗${NC} Installation failed"
    exit 1
fi
echo ""

# Step 5: Clean up build artifact
echo -e "${YELLOW}[5/7]${NC} Cleaning up build artifacts..."
rm -f "${BINARY_NAME}"
echo -e "${GREEN}✓${NC} Cleanup complete"
echo ""

# Step 6: Start the service
echo -e "${YELLOW}[6/7]${NC} Starting service..."
if sudo systemctl start "${SERVICE_NAME}"; then
    echo -e "${GREEN}✓${NC} Service started"
else
    echo -e "${RED}✗${NC} Service failed to start"
    exit 1
fi
echo ""

# Step 7: Verify it's running
echo -e "${YELLOW}[7/7]${NC} Verifying service status..."
sleep 2  # Give it a moment to start

if sudo systemctl is-active --quiet "${SERVICE_NAME}"; then
    echo -e "${GREEN}✓${NC} Service is running"
    echo ""

    # Show status
    echo -e "${YELLOW}=== Service Status ===${NC}"
    systemctl status "${SERVICE_NAME}" --no-pager | head -n 15

    echo ""
    echo -e "${YELLOW}=== Recent Logs ===${NC}"
    sudo journalctl -u "${SERVICE_NAME}" -n 10 --no-pager

    echo ""
    echo -e "${GREEN}=== SUCCESS ===${NC}"
    echo "Verifier has been rebuilt and restarted successfully!"
    echo ""
    echo "To view live logs:"
    echo "  sudo journalctl -u ${SERVICE_NAME} -f"
    echo ""
    echo "To view error log:"
    echo "  tail -f /var/log/block_verifier_${INSTANCE_NAME}_errors.log"
else
    echo -e "${RED}✗${NC} Service is not running"
    echo ""
    echo "Check logs for errors:"
    echo "  sudo journalctl -u ${SERVICE_NAME} -n 50"
    exit 1
fi

