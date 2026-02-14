package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds all Prometheus metrics for the relay
type Metrics struct {
	// Ingest metrics
	EventsReceived    *prometheus.CounterVec
	EventsProcessed   *prometheus.CounterVec
	EventsDropped     *prometheus.CounterVec
	IngestLag         *prometheus.GaugeVec
	PDSConnections    *prometheus.GaugeVec
	PDSReconnects     *prometheus.CounterVec
	BufferSize        prometheus.Gauge
	BufferCapacity    prometheus.Gauge

	// Verification metrics
	VerificationTotal    *prometheus.CounterVec
	VerificationDuration prometheus.Histogram
	DIDCacheHits         prometheus.Counter
	DIDCacheMisses       prometheus.Counter
	DIDCacheSize         prometheus.Gauge

	// Firehose metrics
	FirehoseClients         prometheus.Gauge
	FirehoseEventsSent      *prometheus.CounterVec
	FirehoseEventsBroadcast prometheus.Counter
	FirehoseSlowClients     prometheus.Counter

	// Sequencing metrics
	CurrentSequence prometheus.Gauge
	SequenceRate    prometheus.Gauge

	// Storage metrics
	CarFilesTotal prometheus.Gauge
	CarBytesTotal prometheus.Gauge
}

// New creates and registers all metrics
func New() *Metrics {
	m := &Metrics{
		// Ingest metrics
		EventsReceived: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "relay_events_received_total",
				Help: "Total number of events received from PDSs",
			},
			[]string{"pds", "event_type"},
		),
		EventsProcessed: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "relay_events_processed_total",
				Help: "Total number of events successfully processed",
			},
			[]string{"pds", "event_type"},
		),
		EventsDropped: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "relay_events_dropped_total",
				Help: "Total number of events dropped",
			},
			[]string{"pds", "reason"},
		),
		IngestLag: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "relay_ingest_lag_seconds",
				Help: "Lag between event time and processing time",
			},
			[]string{"pds"},
		),
		PDSConnections: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "relay_pds_connections",
				Help: "Number of active PDS connections",
			},
			[]string{"pds", "status"},
		),
		PDSReconnects: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "relay_pds_reconnects_total",
				Help: "Total number of PDS reconnection attempts",
			},
			[]string{"pds"},
		),
		BufferSize: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "relay_buffer_size",
				Help: "Current number of events in the buffer",
			},
		),
		BufferCapacity: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "relay_buffer_capacity",
				Help: "Maximum buffer capacity",
			},
		),

		// Verification metrics
		VerificationTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "relay_verification_total",
				Help: "Total number of verification attempts",
			},
			[]string{"result"},
		),
		VerificationDuration: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "relay_verification_duration_seconds",
				Help:    "Duration of verification operations",
				Buckets: prometheus.ExponentialBuckets(0.001, 2, 10),
			},
		),
		DIDCacheHits: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "relay_did_cache_hits_total",
				Help: "Total number of DID cache hits",
			},
		),
		DIDCacheMisses: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "relay_did_cache_misses_total",
				Help: "Total number of DID cache misses",
			},
		),
		DIDCacheSize: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "relay_did_cache_size",
				Help: "Current number of entries in DID cache",
			},
		),

		// Firehose metrics
		FirehoseClients: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "relay_firehose_clients",
				Help: "Number of connected firehose clients",
			},
		),
		FirehoseEventsSent: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "relay_firehose_events_sent_total",
				Help: "Total number of events sent to firehose clients",
			},
			[]string{"event_type"},
		),
		FirehoseEventsBroadcast: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "relay_firehose_events_broadcast_total",
				Help: "Total number of events broadcast to firehose",
			},
		),
		FirehoseSlowClients: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "relay_firehose_slow_clients_total",
				Help: "Total number of slow clients disconnected",
			},
		),

		// Sequencing metrics
		CurrentSequence: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "relay_current_sequence",
				Help: "Current sequence number",
			},
		),
		SequenceRate: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "relay_sequence_rate",
				Help: "Rate of sequence number increases per second",
			},
		),

		// Storage metrics
		CarFilesTotal: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "relay_car_files_total",
				Help: "Total number of CAR files",
			},
		),
		CarBytesTotal: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "relay_car_bytes_total",
				Help: "Total bytes used by CAR files",
			},
		),
	}

	return m
}

// Handler returns the Prometheus HTTP handler
func Handler() http.Handler {
	return promhttp.Handler()
}
