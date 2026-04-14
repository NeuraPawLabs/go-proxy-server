#!/bin/bash
# Build Windows resources using goversioninfo
# This is the recommended method - goversioninfo is more mature and stable

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "Building Windows resources using goversioninfo..."

# Check if goversioninfo is installed
if ! command -v goversioninfo &> /dev/null; then
    echo ""
    echo "goversioninfo not found. Installing..."
    echo ""

    # Try to install goversioninfo
    if go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest; then
        echo "✓ goversioninfo installed successfully"
    else
        echo ""
        echo "ERROR: Failed to install goversioninfo."
        echo ""
        echo "Please install it manually:"
        echo "  go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest"
        echo ""
        echo "Or use the alternative method in docs/WINDOWS_BUILD.md"
        exit 1
    fi
fi

# Generate resource.syso file
echo "Generating Windows resource file..."
cd "$PROJECT_ROOT"

# Run goversioninfo with the config file
goversioninfo -64 -o cmd/server/resource_windows_amd64.syso assets/versioninfo.json

SYSO_FILE="$PROJECT_ROOT/cmd/server/resource_windows_amd64.syso"
if [ -f "$SYSO_FILE" ]; then
    echo "✓ Resource file created: $SYSO_FILE"
    ls -lh "$SYSO_FILE"
else
    echo "✗ Failed to create resource file"
    exit 1
fi

echo "Done! The .syso file will be automatically included in Windows builds."
