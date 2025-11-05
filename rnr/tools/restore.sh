#!/bin/bash
# RNR Blockchain - Restore Tool
# Restores blockchain state from backup

set -euo pipefail

BACKUP_DIR="${BACKUP_DIR:-./backups}"
DATA_DIR="${DATA_DIR:-./data}"
CONFIG_DIR="${CONFIG_DIR:-./config}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🔄 RNR BLOCKCHAIN - RESTORE TOOL"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# Check if node is running
if pgrep -f "rnr" > /dev/null; then
    log_error "Node is currently running. Please stop the node before restore."
    exit 1
fi

# List available backups
log_info "Available backups:"
echo
cd "${BACKUP_DIR}"
select BACKUP_FILE in rnr-backup-*.tar.gz; do
    if [ -n "${BACKUP_FILE}" ]; then
        break
    fi
done

if [ -z "${BACKUP_FILE}" ]; then
    log_error "No backup selected"
    exit 1
fi

log_info "Selected backup: ${BACKUP_FILE}"

# Extract backup to temp directory
TEMP_DIR=$(mktemp -d)
log_info "Extracting backup to ${TEMP_DIR}..."
tar -xzf "${BACKUP_FILE}" -C "${TEMP_DIR}"

EXTRACTED_DIR="${TEMP_DIR}/$(basename ${BACKUP_FILE} .tar.gz)"

# Verify checksums
log_info "Verifying backup integrity..."
cd "${EXTRACTED_DIR}"
if sha256sum -c checksums.txt --quiet; then
    log_info "✅ Backup integrity verified"
else
    log_error "Backup integrity check failed!"
    rm -rf "${TEMP_DIR}"
    exit 1
fi

# Show backup metadata
if [ -f "metadata.json" ]; then
    log_info "Backup metadata:"
    cat metadata.json | jq '.' 2>/dev/null || cat metadata.json
    echo
fi

# Confirm restore
log_warn "⚠️  This will REPLACE your current blockchain state!"
read -p "Are you sure you want to continue? (yes/no): " -r
if [[ ! $REPLY =~ ^[Yy][Ee][Ss]$ ]]; then
    log_info "Restore cancelled"
    rm -rf "${TEMP_DIR}"
    exit 0
fi

# Backup current state before restore
if [ -d "${DATA_DIR}" ]; then
    log_info "Backing up current state..."
    mv "${DATA_DIR}" "${DATA_DIR}.pre-restore-$(date +%s)"
fi

if [ -d "${CONFIG_DIR}" ]; then
    mv "${CONFIG_DIR}" "${CONFIG_DIR}.pre-restore-$(date +%s)"
fi

# Restore data
log_info "Restoring blockchain data..."
if [ -d "${EXTRACTED_DIR}/data" ]; then
    cp -r "${EXTRACTED_DIR}/data" "${DATA_DIR}"
    log_info "✅ Blockchain data restored"
fi

# Restore config
log_info "Restoring configuration..."
if [ -d "${EXTRACTED_DIR}/config" ]; then
    cp -r "${EXTRACTED_DIR}/config" "${CONFIG_DIR}"
    log_info "✅ Configuration restored"
fi

# Restore wallets
log_info "Restoring wallets..."
if [ -d "${EXTRACTED_DIR}/wallets" ]; then
    cp -r "${EXTRACTED_DIR}/wallets" "./wallets"
    log_info "✅ Wallets restored"
fi

# Cleanup
rm -rf "${TEMP_DIR}"

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
log_info "✅ Restore completed successfully!"
log_info "You can now start your node"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

exit 0
