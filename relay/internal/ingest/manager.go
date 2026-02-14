package ingest

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/siempre-alaina/at-protocol/relay/internal/config"
	"github.com/siempre-alaina/at-protocol/relay/internal/metrics"
)

// CursorStore interface for persisting cursor positions
type CursorStore interface {
	GetCursor(ctx context.Context, hostname string) (int64, error)
	SaveCursor(ctx context.Context, hostname string, cursor int64) error
}

// EventHandler is called for each received event
type EventHandler func(ctx context.Context, event Event) error

// Manager orchestrates connections to multiple PDSs
type Manager struct {
	cfg           config.IngestConfig
	clients       map[string]*PDSClient
	eventChan     chan Event
	handler       EventHandler
	cursorStore   CursorStore
	metrics       *metrics.Metrics
	mu            sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
}

// NewManager creates a new ingest manager
func NewManager(cfg config.IngestConfig, handler EventHandler, cursorStore CursorStore, m *metrics.Metrics) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	return &Manager{
		cfg:         cfg,
		clients:     make(map[string]*PDSClient),
		eventChan:   make(chan Event, cfg.BufferSize),
		handler:     handler,
		cursorStore: cursorStore,
		metrics:     m,
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Start begins the ingest manager
func (m *Manager) Start() error {
	log.Info().
		Strs("hosts", m.cfg.InitialHosts).
		Int("worker_count", m.cfg.WorkerCount).
		Msg("Starting ingest manager")

	// Start event processing workers
	for i := 0; i < m.cfg.WorkerCount; i++ {
		m.wg.Add(1)
		go m.processEvents(i)
	}

	// Start metrics updater
	m.wg.Add(1)
	go m.updateMetrics()

	// Connect to initial hosts
	for _, host := range m.cfg.InitialHosts {
		if err := m.AddHost(host); err != nil {
			log.Error().Err(err).Str("host", host).Msg("Failed to add initial host")
		}
	}

	return nil
}

// Stop shuts down all connections
func (m *Manager) Stop() error {
	log.Info().Msg("Stopping ingest manager")

	m.cancel()

	// Stop all clients
	m.mu.Lock()
	for _, client := range m.clients {
		client.Stop()
	}
	m.mu.Unlock()

	// Close event channel to signal workers to stop
	close(m.eventChan)

	// Wait for all workers to finish
	m.wg.Wait()

	log.Info().Msg("Ingest manager stopped")
	return nil
}

// AddHost adds a new PDS host to ingest from
func (m *Manager) AddHost(hostname string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already connected
	if _, exists := m.clients[hostname]; exists {
		log.Warn().Str("host", hostname).Msg("Host already connected")
		return nil
	}

	// Get cursor from store
	var cursor int64
	if m.cursorStore != nil {
		var err error
		cursor, err = m.cursorStore.GetCursor(m.ctx, hostname)
		if err != nil {
			log.Warn().Err(err).Str("host", hostname).Msg("Failed to get cursor, starting from beginning")
		}
	}

	// Create and start client
	client := NewPDSClient(
		hostname,
		cursor,
		m.eventChan,
		m.cfg.ReconnectDelay,
		m.cfg.MaxReconnectDelay,
	)

	m.clients[hostname] = client
	client.Start()

	log.Info().Str("host", hostname).Int64("cursor", cursor).Msg("Added PDS host")

	if m.metrics != nil {
		m.metrics.PDSConnections.WithLabelValues(hostname, "connecting").Set(1)
	}

	return nil
}

// RemoveHost removes a PDS host
func (m *Manager) RemoveHost(hostname string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	client, exists := m.clients[hostname]
	if !exists {
		return nil
	}

	client.Stop()
	delete(m.clients, hostname)

	log.Info().Str("host", hostname).Msg("Removed PDS host")

	if m.metrics != nil {
		m.metrics.PDSConnections.WithLabelValues(hostname, "connected").Set(0)
		m.metrics.PDSConnections.WithLabelValues(hostname, "connecting").Set(0)
	}

	return nil
}

// GetHosts returns all connected hosts
func (m *Manager) GetHosts() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	hosts := make([]string, 0, len(m.clients))
	for host := range m.clients {
		hosts = append(hosts, host)
	}
	return hosts
}

// GetHostStatus returns status for a specific host
func (m *Manager) GetHostStatus(hostname string) (connected bool, cursor int64, eventsCount int64, lastEvent time.Time) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	client, exists := m.clients[hostname]
	if !exists {
		return false, 0, 0, time.Time{}
	}

	connected, eventsCount, lastEvent = client.GetStats()
	cursor = client.GetCursor()
	return
}

// processEvents handles events from the channel
func (m *Manager) processEvents(workerID int) {
	defer m.wg.Done()

	logger := log.With().Int("worker", workerID).Logger()
	logger.Debug().Msg("Event processor started")

	for event := range m.eventChan {
		if m.metrics != nil {
			m.metrics.EventsReceived.WithLabelValues(event.Host, event.Type).Inc()
		}

		// Call the handler
		if m.handler != nil {
			if err := m.handler(m.ctx, event); err != nil {
				logger.Error().Err(err).
					Str("type", event.Type).
					Str("did", event.DID).
					Int64("seq", event.Seq).
					Msg("Failed to handle event")

				if m.metrics != nil {
					m.metrics.EventsDropped.WithLabelValues(event.Host, "handler_error").Inc()
				}
				continue
			}
		}

		if m.metrics != nil {
			m.metrics.EventsProcessed.WithLabelValues(event.Host, event.Type).Inc()
		}

		// Save cursor periodically (every 100 events per host)
		if m.cursorStore != nil && event.Seq%100 == 0 {
			if err := m.cursorStore.SaveCursor(m.ctx, event.Host, event.Seq); err != nil {
				logger.Error().Err(err).Str("host", event.Host).Int64("cursor", event.Seq).Msg("Failed to save cursor")
			}
		}
	}

	logger.Debug().Msg("Event processor stopped")
}

// updateMetrics periodically updates metrics
func (m *Manager) updateMetrics() {
	defer m.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.mu.RLock()
			for hostname, client := range m.clients {
				connected, _, lastEvent := client.GetStats()

				if m.metrics != nil {
					if connected {
						m.metrics.PDSConnections.WithLabelValues(hostname, "connected").Set(1)
						m.metrics.PDSConnections.WithLabelValues(hostname, "connecting").Set(0)
					} else {
						m.metrics.PDSConnections.WithLabelValues(hostname, "connected").Set(0)
						m.metrics.PDSConnections.WithLabelValues(hostname, "connecting").Set(1)
					}

					// Calculate lag
					if !lastEvent.IsZero() {
						lag := time.Since(lastEvent).Seconds()
						m.metrics.IngestLag.WithLabelValues(hostname).Set(lag)
					}
				}
			}
			m.mu.RUnlock()

			// Update buffer size
			if m.metrics != nil {
				m.metrics.BufferSize.Set(float64(len(m.eventChan)))
				m.metrics.BufferCapacity.Set(float64(cap(m.eventChan)))
			}
		}
	}
}

// BufferStats returns current buffer statistics
func (m *Manager) BufferStats() (size int, capacity int) {
	return len(m.eventChan), cap(m.eventChan)
}
