# Future Roadmap

> Apple Music Discord Bridge - Improvement Plan

---

## 🏆 High Priority: Production-Readiness

### 1. LaunchAgent Integration

Package as a macOS service that auto-starts on login using `launchd` plist file in `~/Library/LaunchAgents/`. Enables "set and forget" experience for non-technical users.

### 2. Persistent Artwork Cache

Currently in-memory only → lost on restart. Use SQLite or a simple JSON file in `~/.cache/am-bridge/` to persist artwork URLs across restarts. Reduces API calls over time.

### 3. Retry Logic with Exponential Backoff

iTunes API can be flaky. Add 3 retries with 1s → 2s → 4s delays to prevent temporary network issues from showing no artwork.

---

## 🔧 Medium Priority: Polish

### 4. Dual Timestamp Support

Currently sending `EndTimestamp` for the progress bar. Consider sending both `StartTimestamp` and `EndTimestamp` for maximum Discord client compatibility.

### 5. "Listen on Apple Music" Button

Add a clickable button that opens the track in Apple Music. Generate URL like `https://music.apple.com/search?term=...`. Discord supports up to 2 buttons per activity.

### 6. Configurable Poll Interval

Some users want faster updates (5s), others want battery savings (30s). Support via:

- Environment variable: `AM_POLL_INTERVAL=5s`
- Config file: `~/.config/am-bridge/config.json`

---

## 🚀 Low Priority: Advanced Features

### 7. Native macOS Event Listening

Replace polling with `NSDistributedNotificationCenter`. Music.app broadcasts `com.apple.Music.playerInfo` on state changes. Reduces CPU usage to near-zero when idle.

> **Caveat:** Requires CGO or calling out to a Swift helper binary.

### 8. Discord Reconnection Handling

If Discord restarts, the socket dies silently. Add heartbeat/ping to detect dead connections and auto-reconnect when Discord comes back online.

### 9. Multiple Presence Profiles

Support different Discord App IDs for different contexts. Toggle between "Work" and "Personal" profiles.

### 10. Homebrew Distribution

Create a Homebrew formula for easy installation:

```bash
brew install am-discord-bridge
brew services start am-discord-bridge
```

---

## 📊 Metrics to Track

| Metric                    | Why It Matters                |
| ------------------------- | ----------------------------- |
| Cache hit rate            | Are you saving API calls?     |
| Artwork fetch latency     | User-perceived responsiveness |
| Discord connection uptime | Reliability indicator         |
| Memory usage over 24h     | Watch for leaks               |

---

## Recommended Starting Point

**Focus on #1 and #2 first.** LaunchAgent and persistent cache transform this from a "dev tool" into something genuinely shareable with non-technical users.
