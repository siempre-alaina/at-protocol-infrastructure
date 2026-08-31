# AT Protocol Relay

A prototype AT Protocol relay implementation in Go. Subscribes to PDS (Personal
Data Server) firehoses and rebroadcasts events to downstream consumers like feed
generators, search indexers, and other applications.

> **Prototype — not production software.** The firehose output is not yet
> spec-compliant (JSON rather than binary DAG-CBOR), signature verification is
> off by default and non-blocking when enabled, and there are no tests. See
> "Status: this is a prototype" in the [root README](../README.md) for the full
> list of known gaps.

## Reference deployment

The relay previously ran at `relay.service.dashofextra.com`. **That instance has
been decommissioned** — the URLs and server addresses throughout this document are
kept as a worked example of a real deployment, not as live endpoints.

| Endpoint | Path |
|----------|------|
| Health Check | `/xrpc/_health` |
| Firehose | `wss://<your-host>/xrpc/com.atproto.sync.subscribeRepos` |
| Feed Viewer | `/feed` |

## Features

- **Multi-PDS Support** - Connect to multiple Personal Data Servers simultaneously
- **Event Sequencing** - Global sequence numbers via PostgreSQL for reliable replay
- **Cursor Support** - Clients can resume from any sequence number
- **Post Content Extraction** - Extracts actual post text from CBOR-encoded commits
- **DID Resolution** - 100k entry LRU cache for identity lookups
- **Signature Verification** - Optional cryptographic verification of commits
- **Prometheus Metrics** - Full observability with `/metrics` endpoint
- **WebSocket Firehose** - Real-time event broadcast to subscribers
- **Web Feed Viewer** - Browser-based feed viewer at `/feed`
- **Graceful Shutdown** - Clean connection handling and cursor persistence

## Implementation Status

| Phase | Component | Status |
|-------|-----------|--------|
| 1 | Foundation (config, logging, health) | Complete |
| 2 | Ingest Pipeline (WebSocket PDS client) | Complete |
| 3 | Verification (DID resolution, signatures) | Complete |
| 4 | Storage (PostgreSQL, cursor persistence) | Complete |
| 5 | Firehose (WebSocket broadcast) | Complete |
| 6 | Observability (Prometheus metrics) | Complete |
| 7 | Production Deployment | Complete |
| 8 | Feed Viewer with Content Extraction | Complete |

## Architecture

```
┌─────────────┐     ┌─────────────┐
│    PDS 1    │     │    PDS 2    │
└──────┬──────┘     └──────┬──────┘
       │ WebSocket         │
       └─────────┬─────────┘
                 ▼
┌────────────────────────────────────┐
│              RELAY                  │
│  ┌─────────┐   ┌─────────────────┐ │
│  │ Ingest  │──▶│   PostgreSQL    │ │
│  │ Manager │   │  (Sequencing)   │ │
│  └────┬────┘   └────────┬────────┘ │
│       │ CBOR decode     │          │
│       │ extract content │          │
│       ▼                 ▼          │
│            ┌────────────────────┐  │
│            │     Firehose       │  │
│            │ (WebSocket Server) │  │
│            └────────────────────┘  │
└────────────────────────────────────┘
                 │
        ┌────────┼────────┐
        ▼        ▼        ▼
    ┌───────┐ ┌───────┐ ┌───────┐
    │ Feed  │ │Search │ │ Web   │
    │ Gen   │ │Index  │ │Viewer │
    └───────┘ └───────┘ └───────┘
```

## Quick Start

### Prerequisites

- Go 1.21+
- PostgreSQL 16 (optional, for persistence)

### Build

```bash
go build -o relay ./cmd/relay
```

### Run (Development)

```bash
# Without database (events not persisted)
./relay --config relay.yaml --debug

# With PostgreSQL
./relay --config relay.yaml --db --debug
```

### Configuration

Edit `relay.yaml`:

```yaml
server:
  host: 0.0.0.0
  port: 2470

database:
  postgres_url: "postgres://relay:password@localhost/relay?sslmode=disable"

ingest:
  initial_hosts:
    - "your-pds.example.com"
  worker_count: 4

metrics:
  enabled: true
  port: 9091
```

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/xrpc/_health` | GET | Health check with component status |
| `/xrpc/com.atproto.sync.subscribeRepos` | WebSocket | Firehose - subscribe to events |
| `/xrpc/com.atproto.sync.requestCrawl?hostname=...` | POST | Add a new PDS to crawl |
| `/feed` | GET | Web-based feed viewer |
| `/stats` | GET | Internal statistics (JSON) |
| `/ready` | GET | Readiness probe |
| `/live` | GET | Liveness probe |
| `/metrics` | GET | Prometheus metrics (port 9091) |

### Firehose Usage

Connect via WebSocket to receive real-time events:

```javascript
const ws = new WebSocket('wss://relay.service.dashofextra.com/xrpc/com.atproto.sync.subscribeRepos');

ws.onmessage = (event) => {
  const data = JSON.parse(event.data);
  console.log('Event:', data.seq, data.type, data.content);
};
```

With cursor (resume from sequence number):

```javascript
const ws = new WebSocket('wss://relay.service.dashofextra.com/xrpc/com.atproto.sync.subscribeRepos?cursor=12345');
```

### Event Format

```json
{
  "seq": 12345,
  "type": "commit",
  "did": "did:plc:abc123",
  "commit_cid": "bafyreib...",
  "time": "2026-02-14T18:00:00Z",
  "host": "pds.example.com",
  "record_path": "app.bsky.feed.post/3abc123",
  "record_type": "app.bsky.feed.post",
  "content": "Hello, world!",
  "action": "create"
}
```

## Feed Viewer

The relay includes a web-based feed viewer at `/feed` that displays:
- Real-time events as they arrive
- Post content extracted from commits
- Event type (Post, Like, Repost, Follow)
- Action (create, delete)
- Sequence numbers for debugging

Visit https://relay.service.dashofextra.com/feed to see it in action.

## Deployment

### Server Details

- **Server:** <YOUR_SERVER_IP> (DigitalOcean droplet)
- **Domain:** relay.service.dashofextra.com
- **Internal Port:** 2470
- **Metrics Port:** 9091
- **Database:** PostgreSQL 16

### Deployment Files

Files in `deploy/` directory:

| File | Purpose |
|------|---------|
| `relay-linux-amd64` | Pre-built Linux binary |
| `relay.service` | systemd service unit |
| `relay.yaml` | Production configuration |
| `nginx-relay.conf` | nginx reverse proxy config |
| `setup.sh` | Server provisioning script |

### Deploy Commands

```bash
cd deploy/

# Cross-compile for Linux
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o relay-linux-amd64 ../cmd/relay

# Deploy binary
scp relay-linux-amd64 root@<YOUR_SERVER_IP>:/srv/relay/app/relay
ssh root@<YOUR_SERVER_IP> 'chown relay:relay /srv/relay/app/relay && chmod +x /srv/relay/app/relay'

# Restart service
ssh root@<YOUR_SERVER_IP> 'systemctl restart relay'
```

### Deploy Feed Viewer

```bash
scp web/feed.html root@<YOUR_SERVER_IP>:/var/www/html/relay-feed.html
```

### Server Management

```bash
# Check status
ssh root@<YOUR_SERVER_IP> 'systemctl status relay'

# View logs
ssh root@<YOUR_SERVER_IP> 'journalctl -u relay -f'

# Check database
ssh root@<YOUR_SERVER_IP> 'sudo -u postgres psql -d relay -c "SELECT COUNT(*) FROM events;"'

# View recent posts
ssh root@<YOUR_SERVER_IP> 'sudo -u postgres psql -d relay -c "SELECT seq, record_path, record_content FROM events ORDER BY seq DESC LIMIT 10;"'

# Internal stats
ssh root@<YOUR_SERVER_IP> 'curl -s http://127.0.0.1:2470/stats | jq .'
```

## Project Structure

```
relay/
├── cmd/
│   ├── relay/main.go          # Main entry point
│   └── test-firehose/main.go  # Test client
├── internal/
│   ├── config/                # Configuration (Viper)
│   ├── ingest/                # PDS connection, CBOR parsing
│   ├── verify/                # DID resolution, signatures
│   ├── storage/               # PostgreSQL persistence
│   ├── firehose/              # WebSocket broadcast server
│   └── metrics/               # Prometheus metrics
├── pkg/health/                # Health check handlers
├── web/
│   └── feed.html              # Feed viewer web page
├── deploy/                    # Deployment files
├── relay.yaml                 # Default configuration
├── CLAUDE.md                  # AI assistant guidance
└── README.md                  # This file
```

## Database Schema

```sql
CREATE TABLE events (
    seq BIGINT PRIMARY KEY,
    event_type TEXT NOT NULL,
    did TEXT NOT NULL,
    commit_cid TEXT,
    prev_cid TEXT,
    host_id INTEGER REFERENCES hosts(id),
    raw_data BYTEA,
    record_path TEXT,      -- e.g., "app.bsky.feed.post/abc123"
    record_content TEXT,   -- Extracted post text
    created_at TIMESTAMP WITH TIME ZONE
);
```

## Metrics

Prometheus metrics available at `:9091/metrics`:

- `relay_events_received_total` - Events received by PDS and type
- `relay_events_processed_total` - Events successfully processed
- `relay_current_sequence` - Current database sequence number
- `relay_firehose_clients` - Connected firehose clients
- `relay_pds_connections` - PDS connection status
- `relay_did_cache_hits_total` - DID cache performance
- `relay_verification_total` - Verification results

## Adding a New PDS

```bash
curl -X POST "https://relay.service.dashofextra.com/xrpc/com.atproto.sync.requestCrawl?hostname=new-pds.example.com"
```

Or add to `relay.yaml`:

```yaml
ingest:
  initial_hosts:
    - "existing-pds.example.com"
    - "new-pds.example.com"
```

## License

MIT

## Acknowledgments

Built with:
- [indigo](https://github.com/bluesky-social/indigo) - Bluesky's Go SDK
- [gorilla/websocket](https://github.com/gorilla/websocket) - WebSocket implementation
- [fxamacker/cbor](https://github.com/fxamacker/cbor) - CBOR encoding/decoding
- [zerolog](https://github.com/rs/zerolog) - Structured logging
- [viper](https://github.com/spf13/viper) - Configuration management
