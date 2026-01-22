# Apple Music Discord Bridge

A high-performance, ultra-lightweight macOS CLI daemon that bridges Apple Music metadata to Discord Rich Presence.

![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)
![macOS](https://img.shields.io/badge/macOS-11+-000000?style=flat&logo=apple)

## Features

- 🎵 **"Listening to" Badge** - Uses Discord Activity Type 2 for native Spotify-like appearance
- 🖼️ **Album Artwork** - Fetches high-resolution (600x600) artwork from iTunes Search API
- ⚡ **Progress Bar** - Sends `EndTimestamp` once per track; Discord animates the rest
- 💾 **In-Memory Cache** - Avoids redundant API calls for repeated tracks
- 🔄 **Graceful Shutdown** - Clears Discord status before exit (Ctrl+C)
- 📦 **~5MB Binary** - No CGO, pure Go with minimal dependencies

## Installation

### Prerequisites
- macOS 11+
- Go 1.21+
- Discord desktop app running
- A Discord Application ID from [Discord Developer Portal](https://discord.com/developers/applications)

### Build

```bash
# Clone the repository
git clone https://github.com/ahammednibras8/applemusicdiscord.git
cd applemusicdiscord

# Update Discord App ID in main.go (line 40)
# DiscordAppID = "YOUR_APP_ID"

# Build optimized binary
go build -ldflags="-s -w" -o am-bridge

# Run
./am-bridge
```

## Configuration

Edit `main.go` to customize:

```go
const (
    DiscordAppID = "YOUR_DISCORD_APP_ID"  // Required
    PollInterval = 10 * time.Second       // How often to check Apple Music
    APITimeout   = 15 * time.Second       // iTunes API timeout
)
```

## How It Works

```
┌─────────────┐     osascript      ┌─────────────┐
│ Apple Music │ ◄────────────────► │  our-app    │
└─────────────┘                    └──────┬──────┘
                                          │
                    iTunes API            │       Discord IPC
                   (artwork fetch)        │        (socket)
                          ▼               ▼
                   ┌─────────────┐  ┌─────────────┐
                   │  Apple CDN  │  │   Discord   │
                   └─────────────┘  └─────────────┘
```

1. Polls Apple Music every 10 seconds via AppleScript
2. Detects state changes: Playing → Paused → Not Running
3. Fetches album artwork from iTunes Search API (cached)
4. Updates Discord Rich Presence with Activity Type 2 (Listening)

## Architecture Highlights

| Feature | Implementation |
|---------|---------------|
| **No CGO** | Pure Go, easy cross-compilation |
| **Single Binary** | `discord/client.go` embedded, no external deps at runtime |
| **Efficient Polling** | Uses `time.Ticker` + `select` for non-blocking loop |
| **Progress Bar** | Sends `EndTimestamp` once; Discord handles animation |
| **Artwork Proxy** | Uses iTunes URL directly; Discord proxies images |


## Contributing

See [FUTURE_ROADMAP.md](FUTURE_ROADMAP.md) for planned improvements.
