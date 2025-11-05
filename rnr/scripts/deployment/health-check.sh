#!/bin/bash

echo "🏥 RNR Blockchain - Health Check"
echo "================================"

if [ "$#" -lt 1 ]; then
    echo "Usage: ./health-check.sh <vps-ip> [ssh-user]"
    echo "Example: ./health-check.sh 192.168.1.100 ubuntu"
    exit 1
fi

VPS_IP=$1
SSH_USER=${2:-"ubuntu"}

echo "📋 Checking node: $VPS_IP"
echo ""

echo "🔍 Service Status:"
ssh $SSH_USER@$VPS_IP "sudo systemctl status rnr-node --no-pager | head -15"
echo ""

echo "💾 Disk Usage:"
ssh $SSH_USER@$VPS_IP "df -h | grep -E '(Filesystem|/dev/)'"
echo ""

echo "🧠 Memory Usage:"
ssh $SSH_USER@$VPS_IP "free -h"
echo ""

echo "🌐 Network Ports:"
ssh $SSH_USER@$VPS_IP "sudo netstat -tulpn | grep -E '(6000|rnr-node)' || echo 'No active ports found'"
echo ""

echo "📊 Latest Logs (last 20 lines):"
ssh $SSH_USER@$VPS_IP "sudo journalctl -u rnr-node -n 20 --no-pager"
echo ""

echo "🔥 Firewall Status:"
ssh $SSH_USER@$VPS_IP "sudo ufw status | head -10"
echo ""

echo "⏱️  Uptime:"
ssh $SSH_USER@$VPS_IP "uptime"
echo ""

echo "✅ Health check complete!"
