# AT Protocol Relay - Claude AI Guidance

## Project Overview

This is an AT Protocol relay implementation in Go. It subscribes to PDS (Personal Data Server) firehoses and rebroadcasts events to downstream consumers.

**This is application code, not just configuration.** Build, test, and run commands are applicable.

## Key Commands

### Development (Local)
```bash
# Build
go build -o relay ./cmd/relay

# Run with config file
./relay --config relay.yaml

# Run with debug logging
./relay --config relay.yaml --debug

# Run with database
./relay --config relay.yaml --db --debug

# Run tests
go test ./...

# Run with race detector
go test -race ./...

# Cross-compile for Linux
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o deploy/relay-linux-amd64 ./cmd/relay
```

### Server Management (Production at <YOUR_SERVER_IP>)
```bash
# Service management
ssh root@<YOUR_SERVER_IP> 'systemctl status relay'
ssh root@<YOUR_SERVER_IP> 'systemctl restart relay'
ssh root@<YOUR_SERVER_IP> 'systemctl stop relay'

# View logs
ssh root@<YOUR_SERVER_IP> 'journalctl -u relay -f'
ssh root@<YOUR_SERVER_IP> 'journalctl -u relay --since "1 hour ago"'

# Health check
curl https://relay.service.dashofextra.com/xrpc/_health
curl https://relay.service.dashofextra.com/ready

# Metrics
curl http://<YOUR_SERVER_IP>:9091/metrics
```

### Database
```bash
# Connect to database
ssh root@<YOUR_SERVER_IP> 'sudo -u postgres psql -d relay'

# Check current sequence
ssh root@<YOUR_SERVER_IP> 'sudo -u postgres psql -d relay -c "SELECT last_value FROM event_seq"'

# Count events
ssh root@<YOUR_SERVER_IP> 'sudo -u postgres psql -d relay -c "SELECT COUNT(*) FROM events"'

# View recent events with content
ssh root@<YOUR_SERVER_IP> 'sudo -u postgres psql -d relay -c "SELECT seq, event_type, record_path, record_content FROM events ORDER BY seq DESC LIMIT 10"'

# Check connected hosts
ssh root@<YOUR_SERVER_IP> 'sudo -u postgres psql -d relay -c "SELECT hostname, status, cursor, last_seen_at FROM hosts"'
```

## Key Paths

### Local Development
| Path | Description |
|------|-------------|
| `cmd/relay/main.go` | Main entry point |
| `internal/ingest/` | PDS connection and event parsing |
| `internal/firehose/` | WebSocket broadcast server |
| `internal/storage/` | PostgreSQL persistence |
| `web/feed.html` | Feed viewer web page |
| `deploy/` | Deployment files |

### Production Server (<YOUR_SERVER_IP>)
| Path | Description |
|------|-------------|
| `/srv/relay/app/relay` | Production binary |
| `/srv/relay/data/cars/` | CAR file storage |
| `/etc/relay/relay.env` | Environment variables (secrets) |
| `/etc/relay/relay.yaml` | Config file |
| `/etc/systemd/system/relay.service` | systemd unit |
| `/etc/nginx/sites-enabled/relay` | nginx config |
| `/var/www/html/relay-feed.html` | Feed viewer page |

## Architecture

```
                    ┌─────────────┐
                    │    PDS 1    │
                    └──────┬──────┘
                           │ WebSocket (subscribeRepos)
                           ▼
┌──────────────────────────────────────────────────┐
│                    RELAY                          │
│  ┌─────────┐   ┌─────────┐   ┌─────────────────┐ │
│  │ Ingest  │──▶│ Verify  │──▶│   Sequencer     │ │
│  │ Manager │   │ Pipeline│   │ (PostgreSQL)    │ │
│  └─────────┘   └─────────┘   └────────┬────────┘ │
│       │                               │          │
│       │ Extract post content          │          │
│       │ (CBOR decode)                 │          │
│       │                               │          │
│  ┌────▼────────┐    ┌─────────────────▼────────┐ │
│  │  CarStore   │    │       Firehose           │ │
│  │  (Disk)     │    │  (WebSocket broadcast)   │ │
│  └─────────────┘    └──────────────────────────┘ │
└──────────────────────────────────────────────────┘
                           │
                           ▼
              ┌────────────────────────┐
              │   Downstream Clients   │
              │  (Feed Viewer, etc)    │
              └────────────────────────┘
```

## Current Implementation Status

| Phase | Status | Description |
|-------|--------|-------------|
| Phase 1 | Complete | Foundation (config, logging, health) |
| Phase 2 | Complete | Ingest Pipeline (WebSocket client, manager, backpressure) |
| Phase 3 | Complete | Verification (DID resolution, signatures) |
| Phase 4 | Complete | Storage (PostgreSQL events, cursor persistence) |
| Phase 5 | Complete | Firehose broadcast (WebSocket server, client management) |
| Phase 6 | Complete | Observability (Prometheus metrics, requestCrawl endpoint) |
| Phase 7 | Complete | Production deployment to <YOUR_SERVER_IP> |
| Phase 8 | Complete | Feed viewer with post content extraction |

**Production URL:** https://relay.service.dashofextra.com

**What works now:**
- Connects to PDS and receives events
- Parses commits, identity, account, sync events
- **Extracts post content from CBOR-encoded commits**
- Exponential backoff reconnection
- Health and stats endpoints
- DID resolution with 100k entry LRU cache
- Signature verification for commit events
- PostgreSQL event storage with global sequence numbers
- Cursor persistence (resume from last position on restart)
- Firehose WebSocket broadcast to clients
- **Web-based feed viewer at /feed**
- Prometheus metrics at `:9091/metrics`
- requestCrawl endpoint for dynamic PDS addition

## Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/xrpc/_health` | GET | Health check with component status |
| `/health` | GET | Alias for health check |
| `/ready` | GET | Readiness probe (200 if ready) |
| `/live` | GET | Liveness probe (always 200) |
| `/stats` | GET | JSON stats (events, hosts, buffer) |
| `/feed` | GET | **Web-based feed viewer** |
| `/xrpc/com.atproto.sync.subscribeRepos` | WS | Firehose WebSocket endpoint |
| `/xrpc/com.atproto.sync.requestCrawl` | POST | Add new PDS to crawl |
| `/metrics` | GET | Prometheus metrics (port 9091) |

## Database Schema

```sql
-- Events table includes post content
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

## Deployment

### Quick Deploy
```bash
# From local machine (relay/ directory)

# 1. Build for Linux
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o deploy/relay-linux-amd64 ./cmd/relay

# 2. Stop, deploy, start
ssh root@<YOUR_SERVER_IP> 'systemctl stop relay'
scp deploy/relay-linux-amd64 root@<YOUR_SERVER_IP>:/srv/relay/app/relay
ssh root@<YOUR_SERVER_IP> 'chown relay:relay /srv/relay/app/relay && systemctl start relay'

# 3. Deploy feed viewer (if updated)
scp web/feed.html root@<YOUR_SERVER_IP>:/var/www/html/relay-feed.html

# 4. Verify
curl https://relay.service.dashofextra.com/xrpc/_health
```

### Troubleshooting
```bash
# Check logs
ssh root@<YOUR_SERVER_IP> 'journalctl -u relay -f'

# Check database
ssh root@<YOUR_SERVER_IP> 'sudo -u postgres psql -d relay -c "SELECT seq, record_path, record_content FROM events ORDER BY seq DESC LIMIT 5;"'

# Test firehose
ssh root@<YOUR_SERVER_IP> 'curl -s http://127.0.0.1:2470/stats | jq .'
```

## Important Notes

1. **Secrets**: Never commit `/etc/relay/relay.env` - contains database password
2. **Backups**: Database contains event history; back up regularly
3. **nginx**: Uses HTTP/1.1 (not HTTP/2) for WebSocket compatibility with nginx < 1.25.1
4. **Content Extraction**: Uses fxamacker/cbor library for CBOR decoding
5. **Deduplication**: Feed viewer deduplicates events by sequence number

## Code Structure

```
relay/
├── cmd/relay/main.go       # Entry point, CLI flags
├── internal/
│   ├── config/             # Configuration loading (Viper)
│   ├── ingest/             # PDS connection, CBOR parsing, content extraction
│   ├── verify/             # DID resolution, signature verification
│   ├── storage/            # PostgreSQL (events with content)
│   ├── firehose/           # WebSocket broadcast server
│   └── metrics/            # Prometheus metrics
├── pkg/health/             # Health check handlers
├── web/
│   └── feed.html           # Feed viewer web page
├── deploy/                 # systemd, nginx, setup scripts
├── relay.yaml              # Default configuration
└── go.mod                  # Go module definition
```
