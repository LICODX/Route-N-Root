#!/bin/bash

set -e

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🔐 RNR Genesis Configuration Creator"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

read -p "Chain ID [rnr-mainnet-1]: " CHAIN_ID
CHAIN_ID=${CHAIN_ID:-rnr-mainnet-1}

read -p "Network Name [RNR Mainnet]: " NETWORK_NAME
NETWORK_NAME=${NETWORK_NAME:-RNR Mainnet}

read -p "Number of genesis validators [3]: " VALIDATORS
VALIDATORS=${VALIDATORS:-3}

read -sp "Wallet Password (for encryption): " PASSWORD
echo ""
echo ""

if [ -z "$PASSWORD" ]; then
    echo "❌ Password cannot be empty"
    exit 1
fi

echo "📝 Building genesis tool..."
go build -o ./bin/genesis ./cmd/genesis/main.go

echo "🚀 Creating genesis configuration..."
./bin/genesis init \
    -chain-id "$CHAIN_ID" \
    -network "$NETWORK_NAME" \
    -validators "$VALIDATORS" \
    -password "$PASSWORD" \
    -output genesis.json

echo ""
echo "✅ Genesis configuration complete!"
echo ""
echo "📦 Next Steps for VPS Deployment:"
echo "   1. Copy genesis.json to ALL VPS nodes"
echo "   2. Copy ./genesis-keys/validator-N-wallet.json to each VPS"
echo "   3. Run: ./scripts/start-node.sh --genesis genesis.json --wallet validator-1-wallet.json --password YOUR_PASSWORD"
echo ""
echo "⚠️  SECURITY:"
echo "   - Backup ./genesis-keys/ directory to secure location"
echo "   - Never commit genesis-keys/ to git"
echo "   - Delete local genesis-keys/ after distributing to VPS nodes"
