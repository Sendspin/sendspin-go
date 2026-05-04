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
    brew install opus

elif [[ "$OSTYPE" == "linux-gnu"* ]]; then
    # Linux
    echo "Detected Linux"

    if command -v apt-get &> /dev/null; then
        # Debian/Ubuntu
        echo "Installing audio libraries via apt..."
        sudo apt-get update
        sudo apt-get install -y libopus-dev
    elif command -v dnf &> /dev/null; then
        # Fedora/RHEL
        echo "Installing audio libraries via dnf..."
        sudo dnf install -y opus-devel
    elif command -v pacman &> /dev/null; then
        # Arch Linux
        echo "Installing audio libraries via pacman..."
        sudo pacman -S --noconfirm opus
    else
        echo "Error: Unsupported Linux distribution. Please install manually:"
        echo "  - libopus / opus-devel"
        exit 1
    fi
else
    echo "Error: Unsupported operating system: $OSTYPE"
    exit 1
fi

echo ""
echo "Dependencies installed successfully."
echo ""
echo "Note: builds use -tags=nolibopusfile by default (Makefile sets this),"
echo "so libopusfile is not required. If you build manually with raw 'go build'"
echo "and want to skip libopusfile yourself, run:"
echo "  GOFLAGS=-tags=nolibopusfile go build ./..."
