#!/bin/bash
# ABOUTME: Dependency installation script for Resonate Protocol
# ABOUTME: Installs required audio libraries on macOS and Linux

set -e

echo "Installing Resonate Protocol dependencies..."

if [[ "$OSTYPE" == "darwin"* ]]; then
    # macOS
    echo "Detected macOS"

    # Check if Homebrew is installed
    if ! command -v brew &> /dev/null; then
        echo "Error: Homebrew is not installed. Please install it from https://brew.sh"
        exit 1
    fi

    echo "Installing audio libraries via Homebrew..."
    brew install opus opusfile

elif [[ "$OSTYPE" == "linux-gnu"* ]]; then
    # Linux
    echo "Detected Linux"

    if command -v apt-get &> /dev/null; then
        # Debian/Ubuntu
        echo "Installing audio libraries via apt..."
        sudo apt-get update
        sudo apt-get install -y libopus-dev libopusfile-dev
    elif command -v dnf &> /dev/null; then
        # Fedora/RHEL
        echo "Installing audio libraries via dnf..."
        sudo dnf install -y opus-devel opusfile-devel
    elif command -v pacman &> /dev/null; then
        # Arch Linux
        echo "Installing audio libraries via pacman..."
        sudo pacman -S --noconfirm opus opusfile
    else
        echo "Error: Unsupported Linux distribution. Please install manually:"
        echo "  - libopus / opus-devel"
        echo "  - libopusfile / opusfile-devel"
        exit 1
    fi
else
    echo "Error: Unsupported operating system: $OSTYPE"
    exit 1
fi

echo ""
echo "✅ Dependencies installed successfully!"
echo ""
echo "You can now run the resonate-player:"
echo "  ./resonate-player"
