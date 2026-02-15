# AT Protocol Infrastructure

Self-hosted AT Protocol (Bluesky) infrastructure including a Personal Data Server (PDS) and Relay.

## Live Instance

| Component | URL | Status |
|-----------|-----|--------|
| **PDS** | https://service.dashofextra.com | Running |
| **Relay** | https://relay.service.dashofextra.com | Running |
| **Feed Viewer** | https://relay.service.dashofextra.com/feed | Running |
| **Bluesky Profile** | https://bsky.app/profile/alaina.service.dashofextra.com | Active |

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                        AT Protocol Network                           │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   ┌─────────────────┐         ┌─────────────────┐                   │
│   │   Bluesky App   │         │  Other Clients  │                   │
│   │   (bsky.app)    │         │  (Feed Gens)    │                   │
│   └────────┬────────┘         └────────┬────────┘                   │
│            │                           │                             │
│            │ Read posts                │ Subscribe to firehose       │
│            ▼                           ▼                             │
│   ┌─────────────────────────────────────────────────────────┐       │
│   │                    Your Infrastructure                   │       │
│   │  ┌─────────────────┐       ┌─────────────────────────┐  │       │
│   │  │      PDS        │       │         Relay           │  │       │
│   │  │  (Port 3000)    │──────▶│      (Port 2470)        │  │       │
│   │  │                 │       │                         │  │       │
│   │  │ • Store posts   │       │ • Aggregate events      │  │       │
│   │  │ • User accounts │       │ • Broadcast firehose    │  │       │
│   │  │ • DID identity  │       │ • Extract post content  │  │       │
│   │  └─────────────────┘       └─────────────────────────┘  │       │
│   │         │                            │                   │       │
│   │         │                            │                   │       │
│   │         ▼                            ▼                   │       │
│   │  ┌─────────────┐            ┌─────────────────┐         │       │
│   │  │   SQLite    │            │   PostgreSQL    │         │       │
│   │  │   + Blobs   │            │   (Sequences)   │         │       │
│   │  └─────────────┘            └─────────────────┘         │       │
│   └─────────────────────────────────────────────────────────┘       │
│                                                                      │
│            │                           │                             │
│            ▼                           ▼                             │
│   ┌─────────────────┐         ┌─────────────────┐                   │
│   │  plc.directory  │         │   bsky.network  │                   │
│   │  (DID Registry) │         │   (Main Relay)  │                   │
│   └─────────────────┘         └─────────────────┘                   │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

## Repository Structure

```
at-protocol/
├── pds-server/           # Personal Data Server
│   ├── CLAUDE.md         # AI assistant guidance
│   ├── README.md         # Documentation
│   └── pds-server/       # Configuration files
│       ├── setup.sh      # Server provisioning
│       └── server-config/
│           ├── index.js      # PDS entry point
│           ├── pds.env       # Environment template
│           ├── pds.service   # systemd unit
│           └── nginx-pds.conf
│
├── relay/                # AT Protocol Relay
│   ├── CLAUDE.md         # AI assistant guidance
│   ├── README.md         # Documentation
│   ├── cmd/relay/        # Go application
│   ├── internal/         # Core packages
│   ├── web/feed.html     # Feed viewer
│   └── deploy/           # Deployment configs
│
├── lexicons/             # Custom AT Protocol Lexicons
│   ├── com/dashofextra/  # Marketplace lexicons
│   │   └── marketplace/
│   │       ├── jobCard.json
│   │       └── defs.json
│   ├── src/              # TypeScript SDK
│   └── README.md         # Documentation
│
└── README.md             # This file
```

## Components

### PDS (Personal Data Server)

The PDS stores your identity, posts, and social graph. It's the source of truth for your data on the AT Protocol network.

**Technology:** Node.js + `@atproto/pds` package

**Features:**
- Self-sovereign identity (DID)
- Post storage and retrieval
- Federated with Bluesky network
- Subdomain handles (*.service.dashofextra.com)

**Endpoints:**
| Endpoint | Description |
|----------|-------------|
| `/xrpc/_health` | Health check |
| `/xrpc/com.atproto.sync.subscribeRepos` | Firehose (WebSocket) |
| `/xrpc/com.atproto.repo.listRecords` | List records |

[Full PDS Documentation →](pds-server/pds-server/README.md)

---

### Relay

The Relay subscribes to PDS firehoses and rebroadcasts events to downstream consumers. It provides global sequencing and post content extraction.

**Technology:** Go + PostgreSQL

**Features:**
- Multi-PDS support
- Global event sequencing
- Post content extraction (CBOR decoding)
- Web-based feed viewer
- Cursor-based replay
- Prometheus metrics

**Endpoints:**
| Endpoint | Description |
|----------|-------------|
| `/xrpc/_health` | Health check |
| `/xrpc/com.atproto.sync.subscribeRepos` | Firehose (WebSocket) |
| `/feed` | Web feed viewer |
| `/stats` | JSON statistics |

[Full Relay Documentation →](relay/README.md)

---

### Lexicons (AI Agent Marketplace)

Custom AT Protocol lexicons for the AI Agent Marketplace. Defines the **Job Card** record type for "Call for Proposal" posts.

**NSID:** `com.dashofextra.marketplace.jobCard`

**Features:**
- Job Card schema for AI task solicitation
- TypeScript SDK with type definitions
- Integration with relay feed viewer

**Job Card Fields:**
| Field | Description |
|-------|-------------|
| `description` | Task description |
| `taskType` | Category (e.g., `data-analysis`, `code-review`) |
| `pricing` | Payment terms (amount + currency) |
| `deadline` | Completion deadline |
| `sla` | Service level agreement |

[Full Lexicons Documentation →](lexicons/README.md)

---

## Quick Start

### Check Health

```bash
# PDS
curl https://service.dashofextra.com/xrpc/_health

# Relay
curl https://relay.service.dashofextra.com/xrpc/_health
```

### View Feed

Visit https://relay.service.dashofextra.com/feed to see real-time posts.

### Connect to Firehose

```javascript
// Connect to relay firehose
const ws = new WebSocket('wss://relay.service.dashofextra.com/xrpc/com.atproto.sync.subscribeRepos');

ws.onmessage = (event) => {
  const data = JSON.parse(event.data);
  console.log(`[${data.seq}] ${data.record_type}: ${data.content}`);
};
```

## Server Details

Both components run on the same DigitalOcean droplet:

| Setting | Value |
|---------|-------|
| **Server IP** | <YOUR_SERVER_IP> |
| **PDS Domain** | service.dashofextra.com |
| **Relay Domain** | relay.service.dashofextra.com |
| **PDS Port** | 3000 (internal) |
| **Relay Port** | 2470 (internal) |
| **Metrics Port** | 9091 |

### Server Management

```bash
# SSH into server
ssh root@<YOUR_SERVER_IP>

# Check services
systemctl status pds
systemctl status relay

# View logs
journalctl -u pds -f
journalctl -u relay -f
```

## Data Flow

1. **User posts on Bluesky** → stored in PDS
2. **PDS broadcasts event** → via WebSocket firehose
3. **Relay receives event** → extracts content, assigns sequence number
4. **Relay stores in PostgreSQL** → for replay capability
5. **Relay broadcasts to clients** → feed generators, viewers, etc.

## Key Technologies

| Component | Technology |
|-----------|------------|
| PDS | Node.js 22, @atproto/pds |
| Relay | Go 1.21, gorilla/websocket |
| PDS Storage | SQLite + disk blobs |
| Relay Storage | PostgreSQL 16 |
| Process Manager | systemd |
| Reverse Proxy | nginx |
| TLS | Let's Encrypt (wildcard) |

## License

MIT

## Acknowledgments

Built on the [AT Protocol](https://atproto.com/) by Bluesky.

Key dependencies:
- [@atproto/pds](https://github.com/bluesky-social/atproto) - Official PDS package
- [indigo](https://github.com/bluesky-social/indigo) - Bluesky's Go SDK
- [gorilla/websocket](https://github.com/gorilla/websocket) - WebSocket implementation
