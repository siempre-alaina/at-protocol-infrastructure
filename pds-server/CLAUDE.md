# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is an **AT Protocol Personal Data Server (PDS)** infrastructure repository for self-hosting Bluesky. It contains configuration templates, setup automation, and documentation—not a traditional application codebase.

- **Primary dependency:** `@atproto/pds` (official npm package)
- **Deployment target:** DigitalOcean droplet at <YOUR_SERVER_IP>
- **Domain:** service.dashofextra.com (wildcard TLS for subdomain handles)

## Repository Structure

```
pds-server/
├── pds-server/
│   ├── README.md                 # Credentials and quick reference
│   ├── pds-implementation-plan.md # 10-phase setup guide
│   ├── setup.sh                  # Server provisioning script
│   └── server-config/
│       ├── index.js              # PDS entry point (Node.js)
│       ├── pds.env               # Environment config template
│       ├── pds.service           # systemd unit file
│       └── nginx-pds.conf        # nginx reverse proxy config
└── relay/                        # Coming soon
```

## Server Management Commands

SSH into server:
```bash
ssh root@<YOUR_SERVER_IP>
```

PDS service control:
```bash
systemctl status pds
systemctl restart pds
journalctl -u pds -f
```

Check PDS health:
```bash
curl http://127.0.0.1:3000/xrpc/_health  # from server
curl https://service.dashofextra.com/xrpc/_health  # from anywhere
```

Generate invite code:
```bash
curl -X POST -u admin:<ADMIN_PASSWORD> \
  "http://localhost:3000/xrpc/com.atproto.server.createInviteCode" \
  -H "Content-Type: application/json" \
  -d '{"useCount": 1}'
```

## Architecture

```
Internet → nginx (443) → localhost:3000 (PDS) → /srv/pds/data (SQLite + blobs)
```

- **nginx** handles TLS termination, WebSocket upgrades, and reverse proxying
- **PDS** runs as a hardened systemd service under the `pds` user (non-root)
- **Federation** connects to Bluesky infrastructure (plc.directory, api.bsky.app, bsky.network)

## Key Server Paths

| Purpose | Path |
|---------|------|
| PDS Application | `/srv/pds/app/` |
| PDS Data | `/srv/pds/data/` |
| Environment Config | `/etc/pds/pds.env` |
| systemd Service | `/etc/systemd/system/pds.service` |
| nginx Config | `/etc/nginx/sites-available/pds` |
| TLS Certificates | `/etc/letsencrypt/live/service.dashofextra.com/` |

## Critical Notes

- **No build/test/lint commands** - this repo is configuration templates, not application code
- **Secrets in pds-server/README.md** - handle with care, do not commit changes that expose credentials
- **PLC Rotation Key** - the most critical secret; losing it means permanent loss of DID identity control
- **TLS certificates** - expire every 90 days; GoDaddy requires manual DNS-01 challenge renewal
- **WebSocket timeouts** - nginx config has 3600s timeouts critical for federation sync
