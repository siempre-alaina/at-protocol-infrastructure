# PDS Server Setup Documentation

## Overview

Self-hosted AT Protocol Personal Data Server (PDS) for Bluesky, deployed on DigitalOcean.

**Domain:** service.dashofextra.com
**Server IP:** <YOUR_SERVER_IP>
**Bluesky Profile:** https://bsky.app/profile/alaina.service.dashofextra.com

---

## Credentials & Secrets

### Server Access
- **IP Address:** <YOUR_SERVER_IP>
- **SSH User:** root
- **SSH Password:** `<YOUR_SERVER_PASSWORD>`

### PDS Secrets (stored in /etc/pds/pds.env)
- **JWT Secret:** `<GENERATE_ME>`
- **Admin Password:** `<GENERATE_ME>`
- **PLC Rotation Key:** `<GENERATE_ME>`

### User Account
- **Handle:** alaina.service.dashofextra.com
- **DID:** did:plc:rfks7pkdllginmr6wbqyhvh5
- **Password:** `PdsAcc0unt#2026!xK`
- **Email:** alaina.adam10@gmail.com

---

## Technical Setup Summary

1. **Provisioned an Ubuntu 24.04 droplet** on DigitalOcean and installed the `@atproto/pds` Node.js package

2. **Configured wildcard DNS and TLS certificates** to enable handle verification for any user on the server

3. **Set up nginx as a reverse proxy** with WebSocket support for real-time federation between servers

4. **Connected to Bluesky's federation infrastructure** (identity registry, app view, relay, moderation service) and deployed as a hardened systemd service

---

## Server File Locations

| Purpose | Path |
|---------|------|
| PDS Application | `/srv/pds/app/` |
| PDS Data | `/srv/pds/data/` |
| Environment Config | `/etc/pds/pds.env` |
| systemd Service | `/etc/systemd/system/pds.service` |
| nginx Config | `/etc/nginx/sites-available/pds` |
| TLS Certificates | `/etc/letsencrypt/live/service.dashofextra.com/` |

---

## DNS Records (GoDaddy)

| Type | Name | Value |
|------|------|-------|
| A | service | <YOUR_SERVER_IP> |
| A | *.service | <YOUR_SERVER_IP> |
| TXT | _atproto.alaina.service | did=did:plc:rfks7pkdllginmr6wbqyhvh5 |

---

## Useful Commands

### Server Management
```bash
# SSH into server
ssh root@<YOUR_SERVER_IP>

# Check PDS status
systemctl status pds

# Restart PDS
systemctl restart pds

# View PDS logs
journalctl -u pds -f
```

### API Endpoints
```bash
# List your posts
curl "https://service.dashofextra.com/xrpc/com.atproto.repo.listRecords?repo=did:plc:rfks7pkdllginmr6wbqyhvh5&collection=app.bsky.feed.post"

# Check PDS health
curl "https://service.dashofextra.com/xrpc/_health"

# View your profile data
curl "https://service.dashofextra.com/xrpc/com.atproto.repo.listRecords?repo=did:plc:rfks7pkdllginmr6wbqyhvh5&collection=app.bsky.actor.profile"
```

### Create New User
```bash
# Generate invite code (run on server)
curl -X POST -u admin:<GENERATE_ME> \
  "http://localhost:3000/xrpc/com.atproto.server.createInviteCode" \
  -H "Content-Type: application/json" \
  -d '{"useCount": 1}'

# Create account
curl -X POST "https://service.dashofextra.com/xrpc/com.atproto.server.createAccount" \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@example.com",
    "handle": "username.service.dashofextra.com",
    "password": "securepassword",
    "inviteCode": "INVITE_CODE_HERE"
  }'
```

---

## Environment Configuration

Full contents of `/etc/pds/pds.env`:

```
PDS_HOSTNAME=service.dashofextra.com
PDS_PORT=3000
PDS_DATA_DIRECTORY=/srv/pds/data
PDS_BLOBSTORE_DISK_LOCATION=/srv/pds/data/blobs
PDS_JWT_SECRET=<GENERATE_ME>
PDS_ADMIN_PASSWORD=<GENERATE_ME>
PDS_PLC_ROTATION_KEY_K256_PRIVATE_KEY_HEX=<GENERATE_ME>
PDS_DID_PLC_URL=https://plc.directory
PDS_BSKY_APP_VIEW_URL=https://api.bsky.app
PDS_BSKY_APP_VIEW_DID=did:web:api.bsky.app
PDS_REPORT_SERVICE_URL=https://mod.bsky.app
PDS_REPORT_SERVICE_DID=did:plc:ar7c4by46qjdydhdevvrndac
PDS_CRAWLERS=https://bsky.network
PDS_SERVICE_HANDLE_DOMAINS=.service.dashofextra.com
LOG_ENABLED=true
```

---

## Important Notes

- **Keep this file secure** - it contains sensitive credentials
- **TLS certificates** expire every 90 days and need renewal (certbot can auto-renew)
- **PLC Rotation Key** is critical - it's the only way to recover your DID if you lose access
- Your data is stored on your own server at `/srv/pds/data/`

---

## Setup Date
February 2026
