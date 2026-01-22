#!/bin/bash

# Configuration
APP_NAME="am-bridge"
PLIST_NAME="com.ahammednibras.am-bridge.plist"
INSTALL_DIR="$HOME/.local/bin"
LAUNCH_AGENTS_DIR="$HOME/Library/LaunchAgents"

echo "🚀 Installing $APP_NAME..."

# 1. Build the binary
echo "🔨 Building binary..."
go mod tidy
go build -ldflags="-s -w" -o $APP_NAME
if [ $? -ne 0 ]; then
    echo "❌ Build failed!"
    exit 1
fi

# 2. Create install directory
mkdir -p "$INSTALL_DIR"

# 3. Move binary
echo "📦 Moving binary to $INSTALL_DIR..."
mv $APP_NAME "$INSTALL_DIR/"

# 4. Copy plist
echo "📝 Configuring LaunchAgent..."
mkdir -p "$LAUNCH_AGENTS_DIR"
cp $PLIST_NAME "$LAUNCH_AGENTS_DIR/"

# 5. Reload service
echo "🔄 Reloading service..."
launchctl unload "$LAUNCH_AGENTS_DIR/$PLIST_NAME" 2>/dev/null
launchctl load "$LAUNCH_AGENTS_DIR/$PLIST_NAME"

echo "✅ Installation complete!"
echo "logs: tail -f /tmp/am-bridge.log"
