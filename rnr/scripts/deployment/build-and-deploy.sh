#!/bin/bash

set -e

echo "🔨 RNR Blockchain - Build & Deploy Script"
echo "=========================================="

if [ "$#" -lt 1 ]; then
    echo "Usage: ./build-and-deploy.sh <vps-ip> [ssh-user]"
    echo "Example: ./build-and-deploy.sh 192.168.1.100 ubuntu"
    exit 1
fi

VPS_IP=$1
SSH_USER=${2:-"ubuntu"}
BINARY_NAME="rnr-node"
REMOTE_PATH="/home/$SSH_USER/rnr-blockchain"

echo "📋 Configuration:"
echo "   VPS IP: $VPS_IP"
echo "   SSH User: $SSH_USER"
echo "   Remote Path: $REMOTE_PATH"
echo ""

echo "🏗️  Step 1: Building binary for Linux..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o $BINARY_NAME \
    ./cmd/rnr/main.go

if [ ! -f "$BINARY_NAME" ]; then
    echo "❌ Build failed!"
    exit 1
fi

echo "✅ Binary built successfully ($(du -h $BINARY_NAME | cut -f1))"
echo ""

echo "📤 Step 2: Transferring to VPS..."
ssh $SSH_USER@$VPS_IP "mkdir -p $REMOTE_PATH"
scp $BINARY_NAME $SSH_USER@$VPS_IP:$REMOTE_PATH/

echo "✅ Binary transferred"
echo ""

echo "🔧 Step 3: Setting permissions..."
ssh $SSH_USER@$VPS_IP "chmod +x $REMOTE_PATH/$BINARY_NAME"

echo "✅ Permissions set"
echo ""

echo "🔄 Step 4: Restarting service..."
ssh $SSH_USER@$VPS_IP "sudo systemctl restart rnr-node || echo 'Service not configured yet'"

echo "✅ Service restarted"
echo ""

echo "📊 Step 5: Checking status..."
ssh $SSH_USER@$VPS_IP "sudo systemctl status rnr-node --no-pager | head -20 || echo 'Service status unavailable'"

echo ""
echo "✨ Deployment complete!"
echo "📝 View logs: ssh $SSH_USER@$VPS_IP 'sudo journalctl -u rnr-node -f'"
echo ""

rm -f $BINARY_NAME
