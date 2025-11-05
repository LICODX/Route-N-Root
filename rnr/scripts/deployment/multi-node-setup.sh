#!/bin/bash

set -e

echo "🌐 RNR Blockchain - Multi-Node Setup"
echo "===================================="

if [ "$#" -lt 2 ]; then
    echo "Usage: ./multi-node-setup.sh <node1-ip> <node2-ip> [node3-ip] ..."
    echo "Example: ./multi-node-setup.sh 192.168.1.100 192.168.1.101 192.168.1.102"
    exit 1
fi

NODES=("$@")
SSH_USER="ubuntu"

echo "📋 Setting up ${#NODES[@]} nodes:"
for i in "${!NODES[@]}"; do
    echo "   Node $((i+1)): ${NODES[$i]}"
done
echo ""

echo "🔧 Step 1: Setup VPS prerequisites..."
for NODE_IP in "${NODES[@]}"; do
    echo "  → Configuring $NODE_IP..."
    ./scripts/deployment/setup-vps.sh "$NODE_IP" "$SSH_USER" > /dev/null 2>&1 &
done
wait

echo "✅ All nodes configured"
echo ""

echo "🚀 Step 2: Build and deploy..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o rnr-node \
    ./cmd/rnr/main.go

echo "✅ Binary built"
echo ""

for NODE_IP in "${NODES[@]}"; do
    echo "  → Deploying to $NODE_IP..."
    scp -q rnr-node $SSH_USER@$NODE_IP:/home/$SSH_USER/rnr-blockchain/
    ssh $SSH_USER@$NODE_IP "chmod +x /home/$SSH_USER/rnr-blockchain/rnr-node"
done

rm -f rnr-node

echo "✅ All nodes deployed"
echo ""

echo "▶️  Step 3: Starting nodes..."
for i in "${!NODES[@]}"; do
    NODE_IP="${NODES[$i]}"
    echo "  → Starting Node $((i+1)) at $NODE_IP..."
    ssh $SSH_USER@$NODE_IP "sudo systemctl restart rnr-node"
    sleep 2
done

echo "✅ All nodes started"
echo ""

echo "📊 Step 4: Checking status..."
for i in "${!NODES[@]}"; do
    NODE_IP="${NODES[$i]}"
    echo ""
    echo "  Node $((i+1)) ($NODE_IP):"
    ssh $SSH_USER@$NODE_IP "sudo systemctl status rnr-node --no-pager | head -10"
done

echo ""
echo "✨ Multi-node setup complete!"
echo ""
echo "📝 Useful commands:"
echo "   Check logs: ssh $SSH_USER@<node-ip> 'sudo journalctl -u rnr-node -f'"
echo "   Stop node: ssh $SSH_USER@<node-ip> 'sudo systemctl stop rnr-node'"
echo "   Restart node: ssh $SSH_USER@<node-ip> 'sudo systemctl restart rnr-node'"
echo ""
