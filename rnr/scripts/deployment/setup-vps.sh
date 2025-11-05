#!/bin/bash

set -e

echo "🚀 RNR Blockchain - VPS Setup Script"
echo "====================================="

if [ "$#" -lt 1 ]; then
    echo "Usage: ./setup-vps.sh <vps-ip> [ssh-user]"
    echo "Example: ./setup-vps.sh 192.168.1.100 ubuntu"
    exit 1
fi

VPS_IP=$1
SSH_USER=${2:-"ubuntu"}

echo "📋 Configuring VPS: $VPS_IP"
echo ""

echo "📦 Step 1: Installing dependencies..."
ssh $SSH_USER@$VPS_IP 'bash -s' << 'ENDSSH'
    set -e
    
    echo "  → Updating packages..."
    sudo apt update -qq
    
    echo "  → Installing prerequisites..."
    sudo apt install -y curl wget git build-essential
    
    echo "  → Checking Go installation..."
    if ! command -v go &> /dev/null; then
        echo "  → Installing Go 1.23..."
        wget -q https://go.dev/dl/go1.23.0.linux-amd64.tar.gz
        sudo tar -C /usr/local -xzf go1.23.0.linux-amd64.tar.gz
        rm go1.23.0.linux-amd64.tar.gz
        
        echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
        export PATH=$PATH:/usr/local/go/bin
    fi
    
    go version
ENDSSH

echo "✅ Dependencies installed"
echo ""

echo "🔥 Step 2: Configuring firewall..."
ssh $SSH_USER@$VPS_IP 'bash -s' << 'ENDSSH'
    set -e
    
    if ! command -v ufw &> /dev/null; then
        sudo apt install -y ufw
    fi
    
    sudo ufw --force enable
    sudo ufw allow 22/tcp comment 'SSH'
    sudo ufw allow 6000/tcp comment 'RNR P2P'
    sudo ufw status
ENDSSH

echo "✅ Firewall configured"
echo ""

echo "📁 Step 3: Creating directories..."
ssh $SSH_USER@$VPS_IP "mkdir -p /home/$SSH_USER/rnr-blockchain/data"

echo "✅ Directories created"
echo ""

echo "⚙️  Step 4: Installing systemd service..."
SYSTEMD_SERVICE=$(cat << 'EOF'
[Unit]
Description=RNR Blockchain Node
Documentation=https://github.com/yourusername/rnr-blockchain
After=network.target

[Service]
Type=simple
User=REPLACE_USER
Group=REPLACE_USER
WorkingDirectory=/home/REPLACE_USER/rnr-blockchain
ExecStart=/home/REPLACE_USER/rnr-blockchain/rnr-node
Restart=on-failure
RestartSec=10
Environment="NODE_ENV=production"
StandardOutput=journal
StandardError=journal
SyslogIdentifier=rnr-node
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF
)

SYSTEMD_SERVICE="${SYSTEMD_SERVICE//REPLACE_USER/$SSH_USER}"

echo "$SYSTEMD_SERVICE" | ssh $SSH_USER@$VPS_IP "sudo tee /etc/systemd/system/rnr-node.service > /dev/null"

ssh $SSH_USER@$VPS_IP 'bash -s' << 'ENDSSH'
    sudo systemctl daemon-reload
    sudo systemctl enable rnr-node
ENDSSH

echo "✅ Systemd service installed"
echo ""

echo "✨ VPS Setup Complete!"
echo ""
echo "📝 Next steps:"
echo "   1. Deploy binary: ./build-and-deploy.sh $VPS_IP $SSH_USER"
echo "   2. Start node: ssh $SSH_USER@$VPS_IP 'sudo systemctl start rnr-node'"
echo "   3. Check logs: ssh $SSH_USER@$VPS_IP 'sudo journalctl -u rnr-node -f'"
echo ""
