// Local Discord RPC client with Activity Type support
// Based on github.com/hugolgst/rich-go but extended with Type field for Activity Type 2 (Listening)

package discord

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"
)

// Activity Types
const (
	ActivityTypePlaying   = 0 // "Playing {name}"
	ActivityTypeStreaming = 1 // "Streaming {details}"
	ActivityTypeListening = 2 // "Listening to {name}"
	ActivityTypeWatching  = 3 // "Watching {name}"
	ActivityTypeCompeting = 5 // "Competing in {name}"
)

// Activity holds the data for discord rich presence
type Activity struct {
	Type       int        // Activity type (0=Playing, 2=Listening, etc.)
	Details    string     // What the player is currently doing
	State      string     // The user's current party status
	LargeImage string     // Large image URL or asset key
	LargeText  string     // Text displayed when hovering over large image
	SmallImage string     // Small image URL or asset key
	SmallText  string     // Text displayed when hovering over small image
	Timestamps *Timestamps
	Buttons    []*Button
}

// Timestamps holds unix timestamps for start and/or end
type Timestamps struct {
	Start *time.Time
	End   *time.Time
}

// Button holds a clickable button
type Button struct {
	Label string
	Url   string
}

// Internal payload structures
type handshake struct {
	V        string `json:"v"`
	ClientId string `json:"client_id"`
}

type frame struct {
	Cmd   string `json:"cmd"`
	Args  args   `json:"args"`
	Nonce string `json:"nonce"`
}

type args struct {
	Pid      int              `json:"pid"`
	Activity *payloadActivity `json:"activity"`
}

type payloadActivity struct {
	Type       int               `json:"type"` // This is the key addition!
	Details    string            `json:"details,omitempty"`
	State      string            `json:"state,omitempty"`
	Assets     payloadAssets     `json:"assets,omitempty"`
	Timestamps *payloadTimestamps `json:"timestamps,omitempty"`
	Buttons    []*payloadButton  `json:"buttons,omitempty"`
}

type payloadAssets struct {
	LargeImage string `json:"large_image,omitempty"`
	LargeText  string `json:"large_text,omitempty"`
	SmallImage string `json:"small_image,omitempty"`
	SmallText  string `json:"small_text,omitempty"`
}

type payloadTimestamps struct {
	Start *uint64 `json:"start,omitempty"`
	End   *uint64 `json:"end,omitempty"`
}

type payloadButton struct {
	Label string `json:"label,omitempty"`
	Url   string `json:"url,omitempty"`
}

// Client manages the Discord RPC connection
type Client struct {
	clientID string
	conn     net.Conn
	logged   bool
}

// NewClient creates a new Discord RPC client
func NewClient(clientID string) *Client {
	return &Client{
		clientID: clientID,
	}
}

// Login connects to Discord RPC
func (c *Client) Login() error {
	if c.logged {
		return nil
	}

	// Find Discord socket
	conn, err := openSocket()
	if err != nil {
		return fmt.Errorf("failed to connect to Discord: %w", err)
	}
	c.conn = conn

	// Send handshake
	payload, err := json.Marshal(handshake{"1", c.clientID})
	if err != nil {
		return err
	}

	if err := c.send(0, payload); err != nil {
		return err
	}

	// Read response (we don't parse it, just confirm connection)
	if _, err := c.receive(); err != nil {
		return fmt.Errorf("handshake failed: %w", err)
	}

	c.logged = true
	return nil
}

// Logout disconnects from Discord RPC
func (c *Client) Logout() {
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.logged = false
}

// SetActivity updates the Discord Rich Presence
func (c *Client) SetActivity(activity Activity) error {
	if !c.logged {
		return fmt.Errorf("not logged in")
	}

	// Map activity to payload
	pa := &payloadActivity{
		Type:    activity.Type,
		Details: activity.Details,
		State:   activity.State,
		Assets: payloadAssets{
			LargeImage: activity.LargeImage,
			LargeText:  activity.LargeText,
			SmallImage: activity.SmallImage,
			SmallText:  activity.SmallText,
		},
	}

	if activity.Timestamps != nil {
		pa.Timestamps = &payloadTimestamps{}
		if activity.Timestamps.Start != nil {
			start := uint64(activity.Timestamps.Start.UnixMilli())
			pa.Timestamps.Start = &start
		}
		if activity.Timestamps.End != nil {
			end := uint64(activity.Timestamps.End.UnixMilli())
			pa.Timestamps.End = &end
		}
	}

	for _, btn := range activity.Buttons {
		pa.Buttons = append(pa.Buttons, &payloadButton{
			Label: btn.Label,
			Url:   btn.Url,
		})
	}

	payload, err := json.Marshal(frame{
		Cmd:   "SET_ACTIVITY",
		Args:  args{os.Getpid(), pa},
		Nonce: nonce(),
	})
	if err != nil {
		return err
	}

	return c.send(1, payload)
}

// ClearActivity clears the current presence
func (c *Client) ClearActivity() error {
	if !c.logged {
		return nil
	}

	payload, err := json.Marshal(frame{
		Cmd:   "SET_ACTIVITY",
		Args:  args{Pid: os.Getpid(), Activity: nil},
		Nonce: nonce(),
	})
	if err != nil {
		return err
	}

	return c.send(1, payload)
}

// send writes a message to the Discord socket
func (c *Client) send(opcode uint32, payload []byte) error {
	header := make([]byte, 8)
	binary.LittleEndian.PutUint32(header[0:4], opcode)
	binary.LittleEndian.PutUint32(header[4:8], uint32(len(payload)))

	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	if _, err := c.conn.Write(payload); err != nil {
		return err
	}
	return nil
}

// receive reads a message from the Discord socket
func (c *Client) receive() ([]byte, error) {
	header := make([]byte, 8)
	if _, err := c.conn.Read(header); err != nil {
		return nil, err
	}

	length := binary.LittleEndian.Uint32(header[4:8])
	data := make([]byte, length)
	if _, err := c.conn.Read(data); err != nil {
		return nil, err
	}

	return data, nil
}

// nonce generates a random nonce for RPC requests
func nonce() string {
	buf := make([]byte, 16)
	rand.Read(buf)
	buf[6] = (buf[6] & 0x0f) | 0x40
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:])
}

// openSocket connects to the Discord IPC socket (macOS/Linux)
func openSocket() (net.Conn, error) {
	// Try different socket paths
	tmpDirs := []string{
		os.Getenv("XDG_RUNTIME_DIR"),
		os.Getenv("TMPDIR"),
		os.Getenv("TMP"),
		os.Getenv("TEMP"),
		"/tmp",
	}

	for _, tmpDir := range tmpDirs {
		if tmpDir == "" {
			continue
		}
		for i := 0; i < 10; i++ {
			path := fmt.Sprintf("%s/discord-ipc-%d", tmpDir, i)
			conn, err := net.Dial("unix", path)
			if err == nil {
				return conn, nil
			}
		}
	}

	return nil, fmt.Errorf("Discord IPC socket not found")
}
