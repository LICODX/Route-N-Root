#!/usr/bin/env bash
#
# RNR Blockchain - Cross-Platform Build Script
# Builds binaries untuk Windows, macOS, dan Linux
#

set -e

# Configuration
BINARY_NAME="rnr-node"
PACKAGE="./cmd/rnr"
OUTPUT_DIR="./dist"

# Build platforms
PLATFORMS=(
    "windows/amd64"
    "windows/386"
    "darwin/amd64"
    "darwin/arm64"
    "linux/amd64"
    "linux/386"
    "linux/arm64"
    "linux/arm"
)

# Colors untuk output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}🚀 RNR Blockchain - Cross-Platform Build${NC}"
echo "Building for ${#PLATFORMS[@]} platforms..."
echo ""

# Buat output directory
mkdir -p $OUTPUT_DIR

# Build untuk setiap platform
for platform in "${PLATFORMS[@]}"; do
    platform_split=(${platform//\// })
    GOOS=${platform_split[0]}
    GOARCH=${platform_split[1]}
    
    output_name="${BINARY_NAME}-${GOOS}-${GOARCH}"
    
    # Tambah .exe untuk Windows
    if [ $GOOS = "windows" ]; then
        output_name+='.exe'
    fi
    
    output_path="${OUTPUT_DIR}/${output_name}"
    
    echo -e "${GREEN}Building${NC} $GOOS/$GOARCH..."
    
    # Build dengan optimizations
    CGO_ENABLED=0 GOOS=$GOOS GOARCH=$GOARCH go build \
        -ldflags="-s -w" \
        -o $output_path \
        $PACKAGE
    
    if [ $? -ne 0 ]; then
        echo "❌ Build failed for $GOOS/$GOARCH"
        exit 1
    fi
    
    # Tampilkan ukuran file
    size=$(du -h "$output_path" | cut -f1)
    echo -e "   ✅ $output_name ($size)"
    echo ""
done

echo -e "${GREEN}✅ All builds completed successfully!${NC}"
echo ""
echo "Binaries available in: $OUTPUT_DIR/"
ls -lh $OUTPUT_DIR/
