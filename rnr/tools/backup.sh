#!/bin/bash
# RNR Blockchain - Backup Tool
# Creates timestamped backups of blockchain state, database, and configurations

set -euo pipefail

BACKUP_DIR="${BACKUP_DIR:-./backups}"
DATA_DIR="${DATA_DIR:-./data}"
CONFIG_DIR="${CONFIG_DIR:-./config}"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_NAME="rnr-backup-${TIMESTAMP}"
BACKUP_PATH="${BACKUP_DIR}/${BACKUP_NAME}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

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
echo "🛡️  RNR BLOCKCHAIN - BACKUP TOOL"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# Create backup directory
mkdir -p "${BACKUP_PATH}"

# Check if node is running
if pgrep -f "rnr" > /dev/null; then
    log_warn "Node is currently running. For best results, stop the node before backup."
    read -p "Continue anyway? (y/N): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        log_info "Backup cancelled"
        exit 0
    fi
fi

# Backup blockchain state
log_info "Backing up blockchain state..."
if [ -d "${DATA_DIR}" ]; then
    cp -r "${DATA_DIR}" "${BACKUP_PATH}/data"
    log_info "✅ Blockchain data backed up"
else
    log_warn "Data directory not found: ${DATA_DIR}"
fi

# Backup configuration
log_info "Backing up configuration..."
if [ -d "${CONFIG_DIR}" ]; then
    cp -r "${CONFIG_DIR}" "${BACKUP_PATH}/config"
    log_info "✅ Configuration backed up"
else
    log_warn "Config directory not found: ${CONFIG_DIR}"
fi

# Backup wallets (encrypted)
log_info "Backing up wallets..."
if [ -d "./wallets" ]; then
    cp -r "./wallets" "${BACKUP_PATH}/wallets"
    log_info "✅ Wallets backed up"
fi

# Create metadata file
cat > "${BACKUP_PATH}/metadata.json" <<EOF
{
  "backup_timestamp": "${TIMESTAMP}",
  "blockchain_height": $(curl -s http://localhost:9090/metrics | grep rnr_blockchain_height | awk '{print $2}' || echo "0"),
  "node_version": "1.0.0",
  "data_dir": "${DATA_DIR}",
  "config_dir": "${CONFIG_DIR}"
}
EOF

# Calculate checksums
log_info "Calculating checksums..."
cd "${BACKUP_PATH}"
find . -type f -exec sha256sum {} \; > checksums.txt
cd - > /dev/null

# Compress backup
log_info "Compressing backup..."
tar -czf "${BACKUP_PATH}.tar.gz" -C "${BACKUP_DIR}" "${BACKUP_NAME}"
rm -rf "${BACKUP_PATH}"

BACKUP_SIZE=$(du -h "${BACKUP_PATH}.tar.gz" | awk '{print $1}')

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
log_info "✅ Backup completed successfully!"
log_info "📦 Backup file: ${BACKUP_PATH}.tar.gz"
log_info "📊 Backup size: ${BACKUP_SIZE}"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# Cleanup old backups (keep last 10)
log_info "Cleaning up old backups..."
cd "${BACKUP_DIR}"
ls -t rnr-backup-*.tar.gz | tail -n +11 | xargs -r rm
REMAINING=$(ls -1 rnr-backup-*.tar.gz 2>/dev/null | wc -l)
log_info "Kept ${REMAINING} most recent backups"

exit 0
