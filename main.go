// Apple Music Discord Bridge
// A high-performance, ultra-lightweight macOS CLI daemon that bridges
// Apple Music metadata to Discord Rich Presence (RPC).
//
// Architecture:
//   - Polls macOS Music app via osascript every 10 seconds
//   - Fetches album artwork from iTunes Search API with in-memory caching
//   - Uses Discord Activity Type 2 (Listening) for native "Listening to" badge
//   - Sends EndTimestamp once per track change for efficient progress bar rendering
//
// Build: go build -ldflags="-s -w" -o am-bridge
// Run:   ./am-bridge

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"am-discord-bridge/discord"
)

// ============================================================================
// Configuration
// ============================================================================

const (
	// DiscordAppID - Create yours at https://discord.com/developers/applications
	DiscordAppID = "1463599058189946981"

	// PollInterval - How often to check Apple Music state
	PollInterval = 10 * time.Second

	// APITimeout - HTTP timeout for iTunes Search API
	APITimeout = 15 * time.Second

	// iTunesSearchURL - Base URL for artwork lookups
	iTunesSearchURL = "https://itunes.apple.com/search"
)

// ============================================================================
// Data Structures
// ============================================================================

// PlayerState represents the current state of the Music app
type PlayerState int

const (
	StateNotRunning PlayerState = iota
	StatePaused
	StatePlaying
)

func (s PlayerState) String() string {
	switch s {
	case StatePlaying:
		return "Playing"
	case StatePaused:
		return "Paused"
	case StateNotRunning:
		return "Not Running"
	default:
		return "Unknown"
	}
}

// Track holds the metadata extracted from Apple Music
type Track struct {
	Name           string
	Artist         string
	Album          string
	Duration       float64 // seconds
	PlayerPosition float64 // seconds
}

// Equals checks if two tracks are the same (ignoring position)
func (t Track) Equals(other Track) bool {
	return t.Name == other.Name &&
		t.Artist == other.Artist &&
		t.Album == other.Album
}

// iTunesSearchResult represents the API response structure
type iTunesSearchResult struct {
	ResultCount int `json:"resultCount"`
	Results     []struct {
		ArtworkURL100 string `json:"artworkUrl100"`
	} `json:"results"`
}

// ============================================================================
// Artwork Cache (Thread-Safe)
// ============================================================================

// ArtworkCache provides thread-safe caching for iTunes artwork URLs
type ArtworkCache struct {
	mu    sync.RWMutex
	cache map[string]string // key: "artist|album" -> value: artworkUrl600
}

// NewArtworkCache creates a new artwork cache instance
func NewArtworkCache() *ArtworkCache {
	return &ArtworkCache{
		cache: make(map[string]string),
	}
}

// cacheKey generates a unique key for artist/album combination
func (c *ArtworkCache) cacheKey(artist, album string) string {
	return artist + "|" + album
}

// Get retrieves a cached artwork URL if available
func (c *ArtworkCache) Get(artist, album string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	url, exists := c.cache[c.cacheKey(artist, album)]
	return url, exists
}

// Set stores an artwork URL in the cache
func (c *ArtworkCache) Set(artist, album, artworkURL string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[c.cacheKey(artist, album)] = artworkURL
}

// ============================================================================
// AppleScript Integration
// ============================================================================

// runAppleScript executes an AppleScript and returns the trimmed output
func runAppleScript(script string) (string, error) {
	cmd := exec.Command("osascript", "-e", script)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// GetPlayerState checks if Music app is running and its playback state
func GetPlayerState() (PlayerState, error) {
	// Check if Music app is running
	script := `tell application "System Events" to (name of processes) contains "Music"`
	result, err := runAppleScript(script)
	if err != nil {
		return StateNotRunning, err
	}

	if result != "true" {
		return StateNotRunning, nil
	}

	// Get player state
	script = `tell application "Music" to player state as string`
	result, err = runAppleScript(script)
	if err != nil {
		return StateNotRunning, err
	}

	switch result {
	case "playing":
		return StatePlaying, nil
	case "paused":
		return StatePaused, nil
	default:
		return StateNotRunning, nil
	}
}

// GetCurrentTrack extracts metadata from the currently playing track
func GetCurrentTrack() (*Track, error) {
	// Combined AppleScript for efficiency - single osascript call
	script := `
		tell application "Music"
			set trackName to name of current track
			set trackArtist to artist of current track
			set trackAlbum to album of current track
			set trackDuration to duration of current track
			set playerPos to player position
			return trackName & "|||" & trackArtist & "|||" & trackAlbum & "|||" & trackDuration & "|||" & playerPos
		end tell
	`

	result, err := runAppleScript(script)
	if err != nil {
		return nil, fmt.Errorf("failed to get track info: %w", err)
	}

	parts := strings.Split(result, "|||")
	if len(parts) != 5 {
		return nil, fmt.Errorf("unexpected AppleScript output format: %s", result)
	}

	duration, err := strconv.ParseFloat(parts[3], 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse duration: %w", err)
	}

	position, err := strconv.ParseFloat(parts[4], 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse position: %w", err)
	}

	return &Track{
		Name:           parts[0],
		Artist:         parts[1],
		Album:          parts[2],
		Duration:       duration,
		PlayerPosition: position,
	}, nil
}

// ============================================================================
// iTunes API Client
// ============================================================================

// httpClient is a shared client with timeout for all API requests
var httpClient = &http.Client{
	Timeout: APITimeout,
}

// searchITunes performs a single iTunes API search and returns artwork URL if found
func searchITunes(query string) (string, error) {
	params := url.Values{}
	params.Set("term", query)
	params.Set("media", "music")
	params.Set("entity", "album")
	params.Set("limit", "1")

	requestURL := fmt.Sprintf("%s?%s", iTunesSearchURL, params.Encode())

	resp, err := httpClient.Get(requestURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}

	var result iTunesSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if result.ResultCount == 0 || len(result.Results) == 0 {
		return "", fmt.Errorf("no results")
	}

	// Transform 100x100 URL to 600x600 for high resolution
	artworkURL := result.Results[0].ArtworkURL100
	artworkURL = strings.Replace(artworkURL, "100x100bb", "600x600bb", 1)

	return artworkURL, nil
}

// FetchArtworkURL queries the iTunes Search API to find album artwork
// Uses multiple fallback search strategies for better hit rate
// Returns the 600x600 version of the artwork URL
func FetchArtworkURL(artist, album string) (string, error) {
	// Clean up common album name patterns that hurt search
	cleanAlbum := album
	// Remove " - Single", " (From ...)" etc.
	if idx := strings.Index(cleanAlbum, " - Single"); idx != -1 {
		cleanAlbum = cleanAlbum[:idx]
	}
	if idx := strings.Index(cleanAlbum, " (From"); idx != -1 {
		cleanAlbum = cleanAlbum[:idx]
	}

	// Strategy 1: artist + clean album name
	if url, err := searchITunes(fmt.Sprintf("%s %s", artist, cleanAlbum)); err == nil {
		return url, nil
	}

	// Strategy 2: just the album name (works for well-known albums)
	if url, err := searchITunes(cleanAlbum); err == nil {
		return url, nil
	}

	// Strategy 3: just the artist (will get their most popular album)
	if url, err := searchITunes(artist); err == nil {
		return url, nil
	}

	// Strategy 4: original album name as fallback
	if cleanAlbum != album {
		if url, err := searchITunes(album); err == nil {
			return url, nil
		}
	}

	return "", fmt.Errorf("no artwork found for %s - %s", artist, album)
}

// ============================================================================
// Discord RPC Bridge
// ============================================================================

// Bridge manages the connection between Apple Music and Discord
type Bridge struct {
	cache     *ArtworkCache
	client    *discord.Client
	connected bool
	lastTrack *Track
	lastState PlayerState
	mu        sync.Mutex
}

// NewBridge creates a new Bridge instance
func NewBridge() *Bridge {
	return &Bridge{
		cache:     NewArtworkCache(),
		client:    discord.NewClient(DiscordAppID),
		lastState: StateNotRunning,
	}
}

// Connect establishes connection to Discord RPC
func (b *Bridge) Connect() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.connected {
		return nil
	}

	if err := b.client.Login(); err != nil {
		return fmt.Errorf("failed to connect to Discord: %w", err)
	}

	b.connected = true
	log.Println("✓ Connected to Discord RPC")
	return nil
}

// Disconnect closes the Discord RPC connection
func (b *Bridge) Disconnect() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.connected {
		return
	}

	b.client.Logout()
	b.connected = false
	log.Println("✓ Disconnected from Discord RPC")
}

// ClearPresence removes the current activity from Discord
func (b *Bridge) ClearPresence() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.connected {
		return
	}

	b.client.ClearActivity()
	log.Println("✓ Cleared Discord presence")
}

// UpdatePresence updates the Discord Rich Presence with current track info
func (b *Bridge) UpdatePresence(track *Track, state PlayerState) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.connected {
		return
	}

	// Fetch or retrieve cached artwork URL
	artworkURL := ""
	if cachedURL, exists := b.cache.Get(track.Artist, track.Album); exists {
		artworkURL = cachedURL
	} else {
		// Fetch synchronously - block until we have artwork
		// This ensures Discord gets the artwork on first track detection
		log.Printf("🔍 Fetching artwork for: %s - %s", track.Artist, track.Album)
		if url, err := FetchArtworkURL(track.Artist, track.Album); err == nil {
			b.cache.Set(track.Artist, track.Album, url)
			artworkURL = url
			log.Printf("📀 Cached artwork: %s", artworkURL)
		} else {
			log.Printf("⚠️  Artwork fetch failed: %v", err)
		}
	}

	// Calculate end timestamp for Discord progress bar
	// Only Discord handles the animation from here
	remainingSeconds := track.Duration - track.PlayerPosition
	endTime := time.Now().Add(time.Duration(remainingSeconds) * time.Second)

	// Build the activity with Type 2 = Listening
	activity := discord.Activity{
		Type:       discord.ActivityTypeListening, // "Listening to" badge!
		Details:    track.Name,
		State:      fmt.Sprintf("by %s", track.Artist),
		LargeImage: artworkURL,
		LargeText:  track.Album,
		Timestamps: &discord.Timestamps{
			End: &endTime,
		},
	}

	if err := b.client.SetActivity(activity); err != nil {
		log.Printf("⚠️  Failed to update Discord presence: %v", err)
		return
	}

	log.Printf("🎵 Now playing: %s - %s (%s)", track.Name, track.Artist, track.Album)
	if artworkURL != "" {
		log.Printf("🖼️  Artwork URL: %s", artworkURL)
	}
}

// ShouldUpdate determines if a presence update is needed
func (b *Bridge) ShouldUpdate(track *Track, state PlayerState) bool {
	// Always update if state changed
	if state != b.lastState {
		return true
	}

	// Update if track changed
	if b.lastTrack == nil || !track.Equals(*b.lastTrack) {
		return true
	}

	return false
}

// ============================================================================
// Main Application Loop
// ============================================================================

func main() {
	log.SetFlags(log.Ltime)
	log.Println("🍎 Apple Music Discord Bridge starting...")

	bridge := NewBridge()

	// Connect to Discord (non-fatal, will retry in loop)
	if err := bridge.Connect(); err != nil {
		log.Printf("⚠️  Initial Discord connection failed: %v (will retry)", err)
	}

	// Setup graceful shutdown
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	// Main polling ticker
	ticker := time.NewTicker(PollInterval)
	defer ticker.Stop()

	// Initial poll
	pollAndUpdate(bridge)

	log.Printf("⏱️  Polling every %v for changes...", PollInterval)

	// Main event loop
	for {
		select {
		case <-ticker.C:
			pollAndUpdate(bridge)

		case sig := <-shutdown:
			log.Printf("\n🛑 Received signal: %v", sig)
			log.Println("🧹 Cleaning up...")

			// Clear Discord presence before exit
			bridge.ClearPresence()
			bridge.Disconnect()

			log.Println("👋 Goodbye!")
			os.Exit(0)
		}
	}
}

// pollAndUpdate checks Apple Music state and updates Discord accordingly
func pollAndUpdate(bridge *Bridge) {
	// Try to connect if we aren't already
	if !bridge.connected {
		if err := bridge.Connect(); err != nil {
			// Don't log spam every 10s, maybe just debug or silence
			// We'll keep it silent to avoid log flooding unless we want to debug
			return 
		}
	}

	state, err := GetPlayerState()
	if err != nil {
		// Also silence this slightly to avoid log flooding in background
		// log.Printf("⚠️  Error checking player state: %v", err) 
		return
	}

	switch state {
	case StateNotRunning:
		if bridge.lastState != StateNotRunning {
			log.Println("💤 Music app not running")
			bridge.ClearPresence()
			bridge.lastState = StateNotRunning
			bridge.lastTrack = nil
		}

	case StatePaused:
		if bridge.lastState != StatePaused {
			log.Println("⏸️  Playback paused")
			bridge.ClearPresence()
			bridge.lastState = StatePaused
		}

	case StatePlaying:
		track, err := GetCurrentTrack()
		if err != nil {
			log.Printf("⚠️  Error getting track info: %v", err)
			return
		}

		if bridge.ShouldUpdate(track, state) {
			bridge.UpdatePresence(track, state)
			bridge.lastTrack = track
			bridge.lastState = state
		}
	}
}
