package ingest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers/sequential"
	"github.com/bluesky-social/indigo/repo"
	"github.com/fxamacker/cbor/v2"
	"github.com/gorilla/websocket"
	"github.com/ipfs/go-cid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Event represents a parsed event from a PDS
type Event struct {
	Type          string    // "commit", "identity", "account", "handle", etc.
	Seq           int64     // Sequence number from the PDS
	DID           string    // DID of the repo
	Time          time.Time // Event timestamp
	CommitCID     string    // CID of the commit (for commit events)
	PrevCID       string    // Previous commit CID
	RawData       []byte    // Raw CBOR data
	Host          string    // Source PDS hostname
	RecordPath    string    // Path of the record (e.g., "app.bsky.feed.post/abc123")
	RecordContent string    // Content of the record (e.g., post text)
	RecordType    string    // Type of record (e.g., "app.bsky.feed.post")
	Action        string    // Action (create, update, delete)
}

// PDSClient manages a WebSocket connection to a single PDS
type PDSClient struct {
	hostname      string
	cursor        int64
	conn          *websocket.Conn
	eventChan     chan<- Event
	logger        zerolog.Logger
	mu            sync.RWMutex
	connected     bool
	lastEventTime time.Time
	eventsCount   int64

	// Reconnection settings
	reconnectDelay    time.Duration
	maxReconnectDelay time.Duration
	currentDelay      time.Duration

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc
}

// NewPDSClient creates a new PDS client
func NewPDSClient(hostname string, cursor int64, eventChan chan<- Event, reconnectDelay, maxReconnectDelay time.Duration) *PDSClient {
	ctx, cancel := context.WithCancel(context.Background())

	return &PDSClient{
		hostname:          hostname,
		cursor:            cursor,
		eventChan:         eventChan,
		logger:            log.With().Str("pds", hostname).Logger(),
		reconnectDelay:    reconnectDelay,
		maxReconnectDelay: maxReconnectDelay,
		currentDelay:      reconnectDelay,
		ctx:               ctx,
		cancel:            cancel,
	}
}

// Start begins the connection loop
func (c *PDSClient) Start() {
	go c.connectionLoop()
}

// Stop closes the connection
func (c *PDSClient) Stop() {
	c.cancel()
	c.mu.Lock()
	if c.conn != nil {
		c.conn.Close()
	}
	c.mu.Unlock()
}

// IsConnected returns whether the client is currently connected
func (c *PDSClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// GetCursor returns the current cursor position
func (c *PDSClient) GetCursor() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cursor
}

// GetStats returns connection statistics
func (c *PDSClient) GetStats() (connected bool, eventsCount int64, lastEventTime time.Time) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected, c.eventsCount, c.lastEventTime
}

func (c *PDSClient) connectionLoop() {
	for {
		select {
		case <-c.ctx.Done():
			c.logger.Info().Msg("PDS client stopped")
			return
		default:
		}

		err := c.connect()
		if err != nil {
			c.logger.Error().Err(err).Dur("retry_in", c.currentDelay).Msg("Connection failed")

			// Exponential backoff
			select {
			case <-c.ctx.Done():
				return
			case <-time.After(c.currentDelay):
				c.currentDelay = minDuration(c.currentDelay*2, c.maxReconnectDelay)
			}
			continue
		}

		// Reset delay on successful connection
		c.currentDelay = c.reconnectDelay

		// Run the event loop
		err = c.eventLoop()
		if err != nil {
			c.logger.Warn().Err(err).Msg("Event loop ended")
		}

		c.mu.Lock()
		c.connected = false
		if c.conn != nil {
			c.conn.Close()
			c.conn = nil
		}
		c.mu.Unlock()
	}
}

func (c *PDSClient) connect() error {
	// Build WebSocket URL
	u := url.URL{
		Scheme: "wss",
		Host:   c.hostname,
		Path:   "/xrpc/com.atproto.sync.subscribeRepos",
	}

	// Add cursor if we have one
	if c.cursor > 0 {
		q := u.Query()
		q.Set("cursor", fmt.Sprintf("%d", c.cursor))
		u.RawQuery = q.Encode()
	}

	c.logger.Info().Str("url", u.String()).Int64("cursor", c.cursor).Msg("Connecting to PDS")

	// Create WebSocket connection with custom headers
	header := http.Header{}
	header.Set("User-Agent", "ATProto-Relay/1.0")

	dialer := websocket.Dialer{
		HandshakeTimeout: 30 * time.Second,
	}

	conn, resp, err := dialer.DialContext(c.ctx, u.String(), header)
	if err != nil {
		if resp != nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return fmt.Errorf("websocket dial failed: %w (status: %d, body: %s)", err, resp.StatusCode, string(body))
		}
		return fmt.Errorf("websocket dial failed: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.connected = true
	c.mu.Unlock()

	c.logger.Info().Msg("Connected to PDS")
	return nil
}

func (c *PDSClient) eventLoop() error {
	// Create a scheduler for processing events
	scheduler := sequential.NewScheduler(
		"relay",
		c.handleRepoStreamEvent,
	)

	// Use indigo's event handling with slog logger (pass nil for default)
	return events.HandleRepoStream(c.ctx, c.conn, scheduler, slog.Default())
}

// handleRepoStreamEvent processes events from the indigo event stream
func (c *PDSClient) handleRepoStreamEvent(ctx context.Context, evt *events.XRPCStreamEvent) error {
	if evt.Error != nil {
		c.logger.Error().
			Str("error", evt.Error.Error).
			Str("message", evt.Error.Message).
			Msg("Stream error from PDS")
		return fmt.Errorf("stream error: %s - %s", evt.Error.Error, evt.Error.Message)
	}

	var event Event
	event.Host = c.hostname
	event.Time = time.Now()

	switch {
	case evt.RepoCommit != nil:
		commit := evt.RepoCommit
		event.Type = "commit"
		event.Seq = commit.Seq
		event.DID = commit.Repo
		event.CommitCID = commit.Commit.String()
		if commit.PrevData != nil {
			event.PrevCID = commit.PrevData.String()
		}
		// Store the blocks (CAR data) for later processing
		event.RawData = commit.Blocks

		// Extract record content from operations
		if len(commit.Ops) > 0 && len(commit.Blocks) > 0 {
			// Parse the first operation (usually there's only one per commit)
			op := commit.Ops[0]
			event.Action = op.Action
			event.RecordPath = op.Path

			// Extract record type from path (e.g., "app.bsky.feed.post/abc123" -> "app.bsky.feed.post")
			if idx := bytes.IndexByte([]byte(op.Path), '/'); idx > 0 {
				event.RecordType = op.Path[:idx]
			}

			// For creates/updates, extract the record content
			if (op.Action == "create" || op.Action == "update") && op.Cid != nil {
				recordCID := cid.Cid(*op.Cid)
				content := c.extractRecordContent(commit.Blocks, recordCID, event.RecordType)
				if content != "" {
					event.RecordContent = content
				}
			}
		}

		c.logger.Debug().
			Int64("seq", commit.Seq).
			Str("did", commit.Repo).
			Int("ops", len(commit.Ops)).
			Str("record_type", event.RecordType).
			Str("action", event.Action).
			Msg("Received commit")

	case evt.RepoIdentity != nil:
		identity := evt.RepoIdentity
		event.Type = "identity"
		event.Seq = identity.Seq
		event.DID = identity.Did
		event.RawData = c.serializeEvent(evt)

		c.logger.Debug().
			Int64("seq", identity.Seq).
			Str("did", identity.Did).
			Msg("Received identity event")

	case evt.RepoAccount != nil:
		account := evt.RepoAccount
		event.Type = "account"
		event.Seq = account.Seq
		event.DID = account.Did
		event.RawData = c.serializeEvent(evt)

		c.logger.Debug().
			Int64("seq", account.Seq).
			Str("did", account.Did).
			Bool("active", account.Active).
			Msg("Received account event")

	case evt.RepoSync != nil:
		sync := evt.RepoSync
		event.Type = "sync"
		event.Seq = sync.Seq
		event.DID = sync.Did
		event.RawData = sync.Blocks

		c.logger.Debug().
			Int64("seq", sync.Seq).
			Str("did", sync.Did).
			Msg("Received sync event")

	case evt.RepoInfo != nil:
		// Info messages are typically metadata, not actionable events
		info := evt.RepoInfo
		c.logger.Debug().
			Str("name", info.Name).
			Str("message", *info.Message).
			Msg("Received info event")
		return nil

	default:
		// Unknown event type, skip
		return nil
	}

	// Update cursor
	c.mu.Lock()
	c.cursor = event.Seq
	c.eventsCount++
	c.lastEventTime = event.Time
	c.mu.Unlock()

	// Send to event channel (non-blocking with timeout)
	select {
	case c.eventChan <- event:
	case <-time.After(5 * time.Second):
		c.logger.Warn().Int64("seq", event.Seq).Msg("Event channel full, dropping event")
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

// serializeEvent converts an event back to bytes for storage
func (c *PDSClient) serializeEvent(evt *events.XRPCStreamEvent) []byte {
	// For now, we'll store a simple representation
	// In production, you'd want to serialize the full CBOR
	var buf bytes.Buffer

	switch {
	case evt.RepoCommit != nil:
		// The blocks are already in CAR format in evt.RepoCommit.Blocks
		return evt.RepoCommit.Blocks
	}

	return buf.Bytes()
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// extractRecordContent extracts readable content from a record in CAR blocks
func (c *PDSClient) extractRecordContent(blocks []byte, recordCID cid.Cid, recordType string) string {
	if len(blocks) == 0 {
		return ""
	}

	// Read blocks from CAR data
	reader, err := repo.ReadRepoFromCar(context.Background(), bytes.NewReader(blocks))
	if err != nil {
		c.logger.Debug().Err(err).Msg("Failed to read repo from CAR")
		return ""
	}

	// Get the block for the record CID
	block, err := reader.Blockstore().Get(context.Background(), recordCID)
	if err != nil {
		c.logger.Debug().Err(err).Str("cid", recordCID.String()).Msg("Failed to get block")
		return ""
	}

	// Decode the CBOR block into a map using proper CBOR library
	var record map[string]interface{}
	if err := cbor.Unmarshal(block.RawData(), &record); err != nil {
		c.logger.Debug().Err(err).Str("cid", recordCID.String()).Msg("Failed to decode CBOR")
		return ""
	}

	c.logger.Debug().
		Str("record_type", recordType).
		Interface("record", record).
		Msg("Decoded record")

	// Extract content based on record type
	switch recordType {
	case "app.bsky.feed.post":
		if text, ok := record["text"].(string); ok {
			return text
		}
	case "app.bsky.feed.like":
		if subject, ok := record["subject"].(map[string]interface{}); ok {
			if uri, ok := subject["uri"].(string); ok {
				return "Liked: " + uri
			}
		}
	case "app.bsky.feed.repost":
		if subject, ok := record["subject"].(map[string]interface{}); ok {
			if uri, ok := subject["uri"].(string); ok {
				return "Reposted: " + uri
			}
		}
	case "app.bsky.graph.follow":
		if subject, ok := record["subject"].(string); ok {
			return "Followed: " + subject
		}
	case "app.bsky.actor.profile":
		var parts []string
		if displayName, ok := record["displayName"].(string); ok {
			parts = append(parts, displayName)
		}
		if description, ok := record["description"].(string); ok {
			parts = append(parts, description)
		}
		if len(parts) > 0 {
			return parts[0]
		}
	}

	return ""
}
