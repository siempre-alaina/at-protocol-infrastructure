package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
	"github.com/rs/zerolog/log"
)

// DB wraps the PostgreSQL connection
type DB struct {
	*sql.DB
}

// NewDB creates a new database connection
func NewDB(connStr string, maxOpen, maxIdle int, maxLifetime time.Duration) (*DB, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(maxLifetime)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{db}, nil
}

// Migrate runs database migrations
func (db *DB) Migrate(ctx context.Context) error {
	log.Info().Msg("Running database migrations")

	migrations := []string{
		// Hosts table - tracks PDSs we're subscribed to
		`CREATE TABLE IF NOT EXISTS hosts (
			id SERIAL PRIMARY KEY,
			hostname TEXT UNIQUE NOT NULL,
			status TEXT NOT NULL DEFAULT 'active',
			cursor BIGINT,
			last_seen_at TIMESTAMP WITH TIME ZONE,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		)`,

		// Create index on hostname
		`CREATE INDEX IF NOT EXISTS idx_hosts_hostname ON hosts(hostname)`,

		// Sequence table - global event sequence
		`CREATE SEQUENCE IF NOT EXISTS event_seq START 1`,

		// Events table - stores sequenced events
		`CREATE TABLE IF NOT EXISTS events (
			seq BIGINT PRIMARY KEY DEFAULT nextval('event_seq'),
			event_type TEXT NOT NULL,
			did TEXT NOT NULL,
			commit_cid TEXT,
			prev_cid TEXT,
			host_id INTEGER REFERENCES hosts(id),
			raw_data BYTEA,
			record_path TEXT,
			record_content TEXT,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		)`,

		// Create indexes for events
		`CREATE INDEX IF NOT EXISTS idx_events_did ON events(did)`,
		`CREATE INDEX IF NOT EXISTS idx_events_created_at ON events(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_events_host_id ON events(host_id)`,

		// Repos table - tracks repo state
		`CREATE TABLE IF NOT EXISTS repos (
			id SERIAL PRIMARY KEY,
			did TEXT UNIQUE NOT NULL,
			head_cid TEXT,
			signing_key TEXT,
			last_event_seq BIGINT,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		)`,

		// Create index on DID
		`CREATE INDEX IF NOT EXISTS idx_repos_did ON repos(did)`,

		// Blocks table - CAR block index
		`CREATE TABLE IF NOT EXISTS blocks (
			id SERIAL PRIMARY KEY,
			cid TEXT UNIQUE NOT NULL,
			repo_id INTEGER REFERENCES repos(id),
			car_path TEXT NOT NULL,
			car_offset BIGINT NOT NULL,
			block_size INTEGER NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		)`,

		// Create index on CID
		`CREATE INDEX IF NOT EXISTS idx_blocks_cid ON blocks(cid)`,
		`CREATE INDEX IF NOT EXISTS idx_blocks_repo_id ON blocks(repo_id)`,
	}

	for _, migration := range migrations {
		if _, err := db.ExecContext(ctx, migration); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	log.Info().Msg("Database migrations completed")
	return nil
}

// Host operations

// UpsertHost adds or updates a PDS host
func (db *DB) UpsertHost(ctx context.Context, hostname string) (int, error) {
	var id int
	err := db.QueryRowContext(ctx, `
		INSERT INTO hosts (hostname, status)
		VALUES ($1, 'active')
		ON CONFLICT (hostname) DO UPDATE SET updated_at = NOW()
		RETURNING id
	`, hostname).Scan(&id)
	return id, err
}

// UpdateHostCursor updates the cursor position for a host
func (db *DB) UpdateHostCursor(ctx context.Context, hostID int, cursor int64) error {
	_, err := db.ExecContext(ctx, `
		UPDATE hosts SET cursor = $1, last_seen_at = NOW(), updated_at = NOW() WHERE id = $2
	`, cursor, hostID)
	return err
}

// GetHostCursor returns the cursor position for a host
func (db *DB) GetHostCursor(ctx context.Context, hostname string) (int64, error) {
	var cursor sql.NullInt64
	err := db.QueryRowContext(ctx, `
		SELECT cursor FROM hosts WHERE hostname = $1
	`, hostname).Scan(&cursor)
	if err != nil {
		return 0, err
	}
	return cursor.Int64, nil
}

// GetActiveHosts returns all active hosts
func (db *DB) GetActiveHosts(ctx context.Context) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT hostname FROM hosts WHERE status = 'active'
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hosts []string
	for rows.Next() {
		var hostname string
		if err := rows.Scan(&hostname); err != nil {
			return nil, err
		}
		hosts = append(hosts, hostname)
	}
	return hosts, rows.Err()
}

// Event operations

// InsertEvent stores a new event and returns its sequence number
func (db *DB) InsertEvent(ctx context.Context, eventType, did, commitCID, prevCID string, hostID int, rawData []byte, recordPath, recordContent string) (int64, error) {
	var seq int64
	err := db.QueryRowContext(ctx, `
		INSERT INTO events (event_type, did, commit_cid, prev_cid, host_id, raw_data, record_path, record_content)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING seq
	`, eventType, did, commitCID, prevCID, hostID, rawData, recordPath, recordContent).Scan(&seq)
	return seq, err
}

// GetEventsFromSeq returns events starting from a sequence number
func (db *DB) GetEventsFromSeq(ctx context.Context, fromSeq int64, limit int) ([]Event, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT seq, event_type, did, commit_cid, prev_cid, raw_data, record_path, record_content, created_at
		FROM events
		WHERE seq > $1
		ORDER BY seq ASC
		LIMIT $2
	`, fromSeq, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		var commitCID, prevCID, recordPath, recordContent sql.NullString
		if err := rows.Scan(&e.Seq, &e.EventType, &e.DID, &commitCID, &prevCID, &e.RawData, &recordPath, &recordContent, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.CommitCID = commitCID.String
		e.PrevCID = prevCID.String
		e.RecordPath = recordPath.String
		e.RecordContent = recordContent.String
		events = append(events, e)
	}
	return events, rows.Err()
}

// GetCurrentSeq returns the current sequence number
func (db *DB) GetCurrentSeq(ctx context.Context) (int64, error) {
	var seq int64
	err := db.QueryRowContext(ctx, `SELECT last_value FROM event_seq`).Scan(&seq)
	return seq, err
}

// Repo operations

// UpsertRepo adds or updates a repo
func (db *DB) UpsertRepo(ctx context.Context, did, headCID, signingKey string, lastEventSeq int64) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO repos (did, head_cid, signing_key, last_event_seq)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (did) DO UPDATE SET
			head_cid = $2,
			signing_key = $3,
			last_event_seq = $4,
			updated_at = NOW()
	`, did, headCID, signingKey, lastEventSeq)
	return err
}

// GetRepo returns repo information
func (db *DB) GetRepo(ctx context.Context, did string) (*Repo, error) {
	var r Repo
	var headCID, signingKey sql.NullString
	var lastEventSeq sql.NullInt64
	err := db.QueryRowContext(ctx, `
		SELECT id, did, head_cid, signing_key, last_event_seq, created_at, updated_at
		FROM repos WHERE did = $1
	`, did).Scan(&r.ID, &r.DID, &headCID, &signingKey, &lastEventSeq, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	r.HeadCID = headCID.String
	r.SigningKey = signingKey.String
	r.LastEventSeq = lastEventSeq.Int64
	return &r, nil
}

// Event represents a stored event
type Event struct {
	Seq           int64
	EventType     string
	DID           string
	CommitCID     string
	PrevCID       string
	RawData       []byte
	RecordPath    string
	RecordContent string
	CreatedAt     time.Time
}

// Repo represents a stored repo
type Repo struct {
	ID           int
	DID          string
	HeadCID      string
	SigningKey   string
	LastEventSeq int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// CursorStore implements the ingest.CursorStore interface
type CursorStore struct {
	db *DB
}

// NewCursorStore creates a new cursor store
func NewCursorStore(db *DB) *CursorStore {
	return &CursorStore{db: db}
}

// GetCursor returns the cursor position for a host
func (cs *CursorStore) GetCursor(ctx context.Context, hostname string) (int64, error) {
	cursor, err := cs.db.GetHostCursor(ctx, hostname)
	if err == sql.ErrNoRows {
		return 0, nil // No cursor yet, start from beginning
	}
	return cursor, err
}

// SaveCursor saves the cursor position for a host
func (cs *CursorStore) SaveCursor(ctx context.Context, hostname string, cursor int64) error {
	// Upsert the host first to get/create the ID
	hostID, err := cs.db.UpsertHost(ctx, hostname)
	if err != nil {
		return fmt.Errorf("failed to upsert host: %w", err)
	}
	return cs.db.UpdateHostCursor(ctx, hostID, cursor)
}

// EventStore handles event persistence
type EventStore struct {
	db      *DB
	hostIDs map[string]int // Cache of hostname -> hostID
}

// NewEventStore creates a new event store
func NewEventStore(db *DB) *EventStore {
	return &EventStore{
		db:      db,
		hostIDs: make(map[string]int),
	}
}

// StoreEvent stores an event and returns its sequence number
func (es *EventStore) StoreEvent(ctx context.Context, eventType, did, commitCID, prevCID, hostname string, rawData []byte, recordPath, recordContent string) (int64, error) {
	// Get or create host ID (cached)
	hostID, ok := es.hostIDs[hostname]
	if !ok {
		var err error
		hostID, err = es.db.UpsertHost(ctx, hostname)
		if err != nil {
			return 0, fmt.Errorf("failed to get host ID: %w", err)
		}
		es.hostIDs[hostname] = hostID
	}

	return es.db.InsertEvent(ctx, eventType, did, commitCID, prevCID, hostID, rawData, recordPath, recordContent)
}

// GetEventsFromSeq returns events starting from a sequence number
func (es *EventStore) GetEventsFromSeq(ctx context.Context, fromSeq int64, limit int) ([]Event, error) {
	return es.db.GetEventsFromSeq(ctx, fromSeq, limit)
}

// GetCurrentSeq returns the current sequence number
func (es *EventStore) GetCurrentSeq(ctx context.Context) (int64, error) {
	return es.db.GetCurrentSeq(ctx)
}
