package firehose

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"

	"github.com/siempre-alaina/at-protocol/relay/internal/ingest"
	"github.com/siempre-alaina/at-protocol/relay/internal/metrics"
	"github.com/siempre-alaina/at-protocol/relay/internal/storage"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for now
	},
}

// Client represents a connected firehose subscriber
type Client struct {
	id         uint64
	conn       *websocket.Conn
	send       chan []byte
	server     *Server
	cursor     int64
	remoteAddr string
	connectedAt time.Time
}

// Server manages firehose WebSocket connections
type Server struct {
	clients     map[uint64]*Client
	clientCount atomic.Uint64
	nextID      atomic.Uint64
	broadcast   chan []byte
	register    chan *Client
	unregister  chan *Client
	eventStore  *storage.EventStore
	metrics     *metrics.Metrics
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewServer creates a new firehose server
func NewServer(eventStore *storage.EventStore, m *metrics.Metrics) *Server {
	ctx, cancel := context.WithCancel(context.Background())

	s := &Server{
		clients:    make(map[uint64]*Client),
		broadcast:  make(chan []byte, 1000),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		eventStore: eventStore,
		metrics:    m,
		ctx:        ctx,
		cancel:     cancel,
	}

	return s
}

// Start begins the firehose server event loop
func (s *Server) Start() {
	go s.run()
	log.Info().Msg("Firehose server started")
}

// Stop gracefully shuts down the firehose server
func (s *Server) Stop() {
	s.cancel()

	s.mu.Lock()
	for _, client := range s.clients {
		client.conn.Close()
	}
	s.mu.Unlock()

	log.Info().Msg("Firehose server stopped")
}

func (s *Server) run() {
	for {
		select {
		case <-s.ctx.Done():
			return

		case client := <-s.register:
			s.mu.Lock()
			s.clients[client.id] = client
			count := s.clientCount.Add(1)
			s.mu.Unlock()

			log.Info().
				Uint64("client_id", client.id).
				Str("remote_addr", client.remoteAddr).
				Int64("cursor", client.cursor).
				Uint64("total_clients", count).
				Msg("Client connected to firehose")

			if s.metrics != nil {
				s.metrics.FirehoseClients.Set(float64(count))
			}

		case client := <-s.unregister:
			s.mu.Lock()
			if _, ok := s.clients[client.id]; ok {
				delete(s.clients, client.id)
				close(client.send)
				count := s.clientCount.Add(^uint64(0)) // Decrement
				s.mu.Unlock()

				log.Info().
					Uint64("client_id", client.id).
					Str("remote_addr", client.remoteAddr).
					Uint64("total_clients", count).
					Msg("Client disconnected from firehose")

				if s.metrics != nil {
					s.metrics.FirehoseClients.Set(float64(count))
				}
			} else {
				s.mu.Unlock()
			}

		case message := <-s.broadcast:
			s.mu.RLock()
			for _, client := range s.clients {
				select {
				case client.send <- message:
				default:
					// Client buffer full, skip this message for them
					log.Warn().
						Uint64("client_id", client.id).
						Msg("Client send buffer full, dropping message")
				}
			}
			s.mu.RUnlock()
		}
	}
}

// Broadcast sends an event to all connected clients
func (s *Server) Broadcast(event ingest.Event) {
	// Convert event to wire format
	msg, err := eventToMessage(event)
	if err != nil {
		log.Error().Err(err).Msg("Failed to encode event for broadcast")
		return
	}

	select {
	case s.broadcast <- msg:
		if s.metrics != nil {
			s.metrics.FirehoseEventsBroadcast.Inc()
		}
	default:
		log.Warn().Msg("Broadcast channel full, dropping event")
	}
}

// BroadcastWithSeq sends an event with its database sequence number
func (s *Server) BroadcastWithSeq(event ingest.Event, seq int64) {
	// Create message with sequence number
	msg := FirehoseMessage{
		Seq:           seq,
		Type:          event.Type,
		DID:           event.DID,
		CommitCID:     event.CommitCID,
		PrevCID:       event.PrevCID,
		Time:          time.Now().Format(time.RFC3339),
		Host:          event.Host,
		RecordPath:    event.RecordPath,
		RecordContent: event.RecordContent,
		RecordType:    event.RecordType,
		Action:        event.Action,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Error().Err(err).Msg("Failed to encode event for broadcast")
		return
	}

	select {
	case s.broadcast <- data:
		if s.metrics != nil {
			s.metrics.FirehoseEventsBroadcast.Inc()
		}
	default:
		log.Warn().Msg("Broadcast channel full, dropping event")
	}
}

// FirehoseMessage is the wire format for firehose events
type FirehoseMessage struct {
	Seq           int64  `json:"seq"`
	Type          string `json:"type"`
	DID           string `json:"did"`
	CommitCID     string `json:"commit_cid,omitempty"`
	PrevCID       string `json:"prev_cid,omitempty"`
	Time          string `json:"time"`
	Host          string `json:"host"`
	RecordPath    string `json:"record_path,omitempty"`
	RecordContent string `json:"content,omitempty"`
	RecordType    string `json:"record_type,omitempty"`
	Action        string `json:"action,omitempty"`
}

func eventToMessage(event ingest.Event) ([]byte, error) {
	msg := FirehoseMessage{
		Seq:           event.Seq,
		Type:          event.Type,
		DID:           event.DID,
		CommitCID:     event.CommitCID,
		PrevCID:       event.PrevCID,
		Time:          time.Now().Format(time.RFC3339),
		Host:          event.Host,
		RecordPath:    event.RecordPath,
		RecordContent: event.RecordContent,
		RecordType:    event.RecordType,
		Action:        event.Action,
	}
	return json.Marshal(msg)
}

// Handler returns the HTTP handler for the firehose WebSocket endpoint
func (s *Server) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Parse cursor from query params
		var cursor int64
		cursorStr := r.URL.Query().Get("cursor")
		if cursorStr != "" {
			var err error
			cursor, err = strconv.ParseInt(cursorStr, 10, 64)
			if err != nil {
				http.Error(w, "Invalid cursor parameter", http.StatusBadRequest)
				return
			}
		}

		// Upgrade to WebSocket
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Error().Err(err).Msg("Failed to upgrade WebSocket connection")
			return
		}

		// Create client
		client := &Client{
			id:          s.nextID.Add(1),
			conn:        conn,
			send:        make(chan []byte, 256),
			server:      s,
			cursor:      cursor,
			remoteAddr:  r.RemoteAddr,
			connectedAt: time.Now(),
		}

		// Register client
		s.register <- client

		// Start client goroutines
		go client.writePump()
		go client.readPump()

		// If cursor provided (including 0 for "replay all"), replay from that point
		if cursorStr != "" && s.eventStore != nil {
			go client.replayFromCursor(cursor)
		}
	}
}

// writePump sends messages to the WebSocket connection
func (c *Client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// readPump reads messages from the WebSocket connection
func (c *Client) readPump() {
	defer func() {
		c.server.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Error().Err(err).Uint64("client_id", c.id).Msg("WebSocket error")
			}
			break
		}
	}
}

// replayFromCursor sends historical events from the given cursor
func (c *Client) replayFromCursor(cursor int64) {
	if c.server.eventStore == nil {
		return
	}

	log.Info().
		Uint64("client_id", c.id).
		Int64("cursor", cursor).
		Msg("Replaying events from cursor")

	batchSize := 100
	currentCursor := cursor

	for {
		events, err := c.server.eventStore.GetEventsFromSeq(context.Background(), currentCursor, batchSize)
		if err != nil {
			log.Error().Err(err).Msg("Failed to get events for replay")
			return
		}

		if len(events) == 0 {
			break
		}

		for _, event := range events {
			// Extract record_type from record_path (e.g., "app.bsky.feed.post/abc123" -> "app.bsky.feed.post")
			recordType := ""
			if event.RecordPath != "" {
				if idx := strings.Index(event.RecordPath, "/"); idx > 0 {
					recordType = event.RecordPath[:idx]
				}
			}

			msg := FirehoseMessage{
				Seq:           event.Seq,
				Type:          event.EventType,
				DID:           event.DID,
				CommitCID:     event.CommitCID,
				PrevCID:       event.PrevCID,
				Time:          event.CreatedAt.Format(time.RFC3339),
				RecordPath:    event.RecordPath,
				RecordContent: event.RecordContent,
				RecordType:    recordType,
				Action:        "create", // Historical events are creates
			}

			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}

			select {
			case c.send <- data:
				currentCursor = event.Seq
			default:
				log.Warn().Uint64("client_id", c.id).Msg("Client buffer full during replay")
				return
			}
		}

		if len(events) < batchSize {
			break
		}
	}

	log.Info().
		Uint64("client_id", c.id).
		Int64("replayed_to", currentCursor).
		Msg("Replay complete")
}

// ClientCount returns the number of connected clients
func (s *Server) ClientCount() uint64 {
	return s.clientCount.Load()
}

// GetClientInfo returns information about connected clients
func (s *Server) GetClientInfo() []map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	clients := make([]map[string]interface{}, 0, len(s.clients))
	for _, client := range s.clients {
		clients = append(clients, map[string]interface{}{
			"id":           client.id,
			"remote_addr":  client.remoteAddr,
			"cursor":       client.cursor,
			"connected_at": client.connectedAt,
			"connected_for": time.Since(client.connectedAt).String(),
		})
	}
	return clients
}
