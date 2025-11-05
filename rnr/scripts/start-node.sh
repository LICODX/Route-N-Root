#!/bin/bash

GENESIS_FILE=""
WALLET_FILE=""
PASSWORD=""
PORT=6000

while [[ $# -gt 0 ]]; do
  case $1 in
    --genesis)
      GENESIS_FILE="$2"
      shift 2
      ;;
    --wallet)
      WALLET_FILE="$2"
      shift 2
      ;;
    --password)
      PASSWORD="$2"
      shift 2
      ;;
    --port)
      PORT="$2"
      shift 2
      ;;
    *)
      echo "Unknown option: $1"
      echo "Usage: $0 --genesis <file> --wallet <file> --password <password> [--port <port>]"
      exit 1
      ;;
  esac
done

if [ -z "$GENESIS_FILE" ] || [ -z "$WALLET_FILE" ] || [ -z "$PASSWORD" ]; then
    echo "❌ Missing required arguments"
    echo "Usage: $0 --genesis <file> --wallet <file> --password <password> [--port <port>]"
    exit 1
fi

if [ ! -f "$GENESIS_FILE" ]; then
    echo "❌ Genesis file not found: $GENESIS_FILE"
    exit 1
fi

if [ ! -f "$WALLET_FILE" ]; then
    echo "❌ Wallet file not found: $WALLET_FILE"
    exit 1
fi

export RNR_GENESIS_CONFIG="$GENESIS_FILE"
export RNR_WALLET_FILE="$WALLET_FILE"
export RNR_WALLET_PASSWORD="$PASSWORD"
export RNR_P2P_PORT="$PORT"

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🚀 Starting RNR Node"
echo "   Genesis: $GENESIS_FILE"
echo "   Wallet: $WALLET_FILE"
echo "   P2P Port: $PORT"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

go run ./cmd/rnr/main.go
