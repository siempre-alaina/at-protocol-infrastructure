# Build-From-Scratch AT Protocol PDS (Personal Data Server) for Bluesky
**Project plan + step-by-step implementation guide (learning-focused, single-user)**

**Target Domain:** `dashofextra.com`

## Overview
This project sets up a self-hosted **AT Protocol PDS (Personal Data Server)** that can be used as a custom hosting provider for Bluesky. The goal is learning and understanding how the pieces fit together:
- Domain + DNS identity
- A web application (Node-based PDS)
- Data storage (SQLite + blob storage)
- Secrets/cryptographic keys
- Reverse proxy + HTTPS
- Process supervision (systemd)
- Basic bootstrap (invite + account) and client login

**Non-goals**
- Production-grade scaling, multi-user operations, high reliability
- Running the full Bluesky stack (relay, appview, moderation services)
- Heavy posting or large media hosting

> **Alternative (faster path):** If you want a simpler Docker-based setup, the official installer is available:
> ```bash
> curl https://raw.githubusercontent.com/bluesky-social/pds/main/installer.sh | sudo bash
> ```
> This guide takes the manual approach for learning purposes.

---

## Architecture (mental model)
- **PDS app (Node)** runs on `127.0.0.1:3000` (not directly exposed to the internet)
- **Nginx** listens on ports **80/443** and proxies requests to the PDS
- **TLS/HTTPS** managed by Let's Encrypt via **certbot**
- **Data directory** holds all persistent state (DB files, repo state, blobs)
- **/etc** holds configuration and secrets (locked down permissions)
- **systemd service** ensures PDS starts on boot and restarts on failure

---

## Assumptions / Requirements
### Domain & DNS
- You own a domain (e.g., `dashofextra.com`)
- You can create DNS records (A records, wildcard)

### Server
- VPS running Ubuntu 22.04 LTS (or 24.04 LTS)
- Public IPv4 address
- Ports open: **22**, **80**, **443**

### Tools
- Node.js 22 LTS (Active LTS — better performance than Node.js 20)
- Nginx + certbot
- OpenSSL
- systemd (default on Ubuntu)

---

## Deliverables / Success Criteria
- `https://dashofextra.com` serves PDS endpoints (via nginx proxy to local PDS)
- Valid **wildcard** TLS certificate from Let's Encrypt (covers `*.dashofextra.com`)
- PDS running under a **non-root** user via systemd
- PDS persists data under `/srv/pds/data`
- You can create an account (preferably via admin CLI; curl fallback acceptable)
- You can sign into Bluesky using custom hosting provider `https://dashofextra.com`

---

## Project Phases
1. Foundation: VPS + Domain + DNS
2. System prep: packages, users, directories, permissions
3. Runtime: Node.js install
4. App layer: install `@atproto/pds` + minimal wrapper
5. Secrets: generate keys + create env config
6. Service: systemd unit + logs
7. Edge: nginx reverse proxy + TLS (certbot)
8. Bootstrap: invite + account creation
9. Client: login from Bluesky app/web
10. Ops: logs, troubleshooting, backups

---

# Phase 1 — Foundation (VPS + DNS)

## 1.1 Buy a domain
Example: `dashofextra.com`.

**Tip (learning-friendly):**
Start with a subdomain handle first (e.g., `me.dashofextra.com`) and later move to apex handle (`dashofextra.com`) once everything is stable.

## 1.2 Provision a VPS
- Ubuntu 22.04 LTS (or 24.04 LTS)
- 1–2 GB RAM is enough for personal use
- Ensure inbound firewall allows:
  - 22 (SSH)
  - 80 (HTTP)
  - 443 (HTTPS)

## 1.3 Set DNS records
Create the following A records in your DNS provider (e.g., GoDaddy):

- `A  dashofextra.com      -> <YOUR_VPS_IP>`
- `A  *.dashofextra.com    -> <YOUR_VPS_IP>`  (wildcard)

**Why wildcard?**
It makes subdomain handles (like `me.dashofextra.com`) and typical PDS workflows reliable and reduces DNS friction.

---

# Phase 2 — System Preparation (secure defaults)

## 2.1 SSH into the server
```bash
ssh root@<YOUR_VPS_IP>
```

## 2.2 Update the OS

```bash
apt update && apt upgrade -y
```

## 2.3 Configure firewall

Set up the firewall to allow only necessary ports:

```bash
ufw allow 22/tcp   # SSH (keep this first so you don't lock yourself out!)
ufw allow 80/tcp   # HTTP (needed for certificate challenges)
ufw allow 443/tcp  # HTTPS (main traffic)
ufw enable
```

## 2.4 Create a dedicated service user (avoid root)

```bash
adduser --system --group --home /srv/pds --shell /usr/sbin/nologin pds
```

## 2.5 Create directories

We will use:

* `/srv/pds/app`   → application code
* `/srv/pds/data`  → persistent data (DBs, repos, blobs)
* `/etc/pds`       → config/secrets (restricted)

```bash
mkdir -p /srv/pds/app /srv/pds/data
mkdir -p /etc/pds

chown -R pds:pds /srv/pds
chmod 700 /etc/pds
```

---

# Phase 3 — Install Node.js 22 LTS + build tools

**Why Node.js 22 instead of 20?**
- Node.js 20 is now in maintenance mode (security fixes only)
- Node.js 22 is Active LTS with 30% faster startup and built-in WebSocket support

```bash
apt install -y curl ca-certificates gnupg build-essential
curl -fsSL https://deb.nodesource.com/setup_22.x | bash -
apt install -y nodejs
node -v   # Should show v22.x.x
npm -v
```

---

# Phase 4 — Build the PDS Application Wrapper

## 4.1 Initialize Node project

```bash
cd /srv/pds/app
sudo -u pds npm init -y
sudo -u pds npm install @atproto/pds
```

> **Note:** We're NOT installing `dotenv` because systemd will inject environment variables for us (see Phase 6). This is more secure and simpler.

## 4.2 Create startup script

Create `/srv/pds/app/index.js`:

```bash
nano /srv/pds/app/index.js
```

Paste:

```js
const { PDS, envToCfg, envToSecrets, readEnv } = require('@atproto/pds');

async function main() {
  const env = readEnv();      // Reads from process.env (populated by systemd)
  const cfg = envToCfg(env);
  const secrets = envToSecrets(env);

  const pds = await PDS.create(cfg, secrets);
  await pds.start();

  console.log('PDS is running');
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
```

Ensure ownership:

```bash
chown -R pds:pds /srv/pds/app
```

---

# Phase 5 — Configuration + Cryptography (Secrets)

## 5.1 Generate secrets

Run these and store them safely (offline password manager recommended):

**JWT secret**

```bash
openssl rand -hex 32
```

**Admin password**

```bash
openssl rand -hex 16
```

**PLC rotation key (K-256 private key hex)**

```bash
openssl ecparam -name secp256k1 -genkey -noout -outform DER \
  | tail -c +8 | head -c 32 | xxd -p -c 32
```

> **Important:** The rotation key output should be exactly **64 hex characters** (32 bytes). Count them! If it's shorter or longer, run the command again.

> **Critical backup warning:** The PLC rotation key is **irreplaceable**. If you lose it:
> - You permanently lose control of your DID identity
> - You cannot reset it or recover it
> - Anyone with this key can take over your identity
>
> Back it up securely (encrypted password manager, offline storage).

## 5.2 Create env file

Create `/etc/pds/pds.env`:

```bash
nano /etc/pds/pds.env
```

Paste and edit values:

```ini
# --- NETWORK ---
PDS_HOSTNAME=dashofextra.com
PDS_PORT=3000

# --- DATA LOCATIONS ---
PDS_DATA_DIRECTORY=/srv/pds/data
PDS_BLOBSTORE_DISK_LOCATION=/srv/pds/data/blobs

# --- SECRETS (paste your generated values) ---
PDS_JWT_SECRET=<paste jwt hex>
PDS_ADMIN_PASSWORD=<paste admin hex>
PDS_PLC_ROTATION_KEY_K256_PRIVATE_KEY_HEX=<paste rotation key hex>

# --- FEDERATION (REQUIRED for Bluesky network!) ---
# Without these, your PDS runs but is isolated from the network
PDS_DID_PLC_URL=https://plc.directory
PDS_BSKY_APP_VIEW_URL=https://api.bsky.app
PDS_BSKY_APP_VIEW_DID=did:web:api.bsky.app
PDS_REPORT_SERVICE_URL=https://mod.bsky.app
PDS_REPORT_SERVICE_DID=did:plc:ar7c4by46qjdydhdevvrndac
PDS_CRAWLERS=https://bsky.network

# --- HANDLE DOMAINS ---
# The leading dot means "allow any subdomain of dashofextra.com"
PDS_SERVICE_HANDLE_DOMAINS=.dashofextra.com

# --- LOGGING ---
LOG_ENABLED=true

# --- EMAIL (optional for learning) ---
# Add later if you want full email verification flows.
# PDS_EMAIL_FROM_ADDRESS=admin@dashofextra.com
# PDS_EMAIL_SMTP_URL=smtps://user:pass@smtp.example.com:465
```

Set file permissions (systemd will read this as root before dropping privileges):

```bash
chmod 600 /etc/pds/pds.env
```

> **Note:** We do NOT need to make this file readable by the `pds` user because systemd reads it as root and injects the variables into the environment before starting the PDS process.

## 5.3 Create blob directory + permissions

```bash
mkdir -p /srv/pds/data/blobs
chown -R pds:pds /srv/pds/data
```

---

# Phase 6 — systemd Service (keep it alive)

## 6.1 Create systemd unit

```bash
nano /etc/systemd/system/pds.service
```

Paste:

```ini
[Unit]
Description=ATProto PDS (Node)
After=network.target

[Service]
Type=simple
User=pds
Group=pds
WorkingDirectory=/srv/pds/app

Environment=NODE_ENV=production
EnvironmentFile=/etc/pds/pds.env

ExecStart=/usr/bin/node /srv/pds/app/index.js
Restart=on-failure
RestartSec=5

# Hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/srv/pds

[Install]
WantedBy=multi-user.target
```

> **Key change:** We use `EnvironmentFile=` instead of having Node.js read the file directly. systemd reads the secrets file as root, then drops privileges to the `pds` user. This is more secure and avoids permission issues.

## 6.2 Start the service

```bash
systemctl daemon-reload
systemctl enable pds
systemctl start pds
systemctl status pds --no-pager
```

## 6.3 Check logs

```bash
journalctl -u pds -f
```

## 6.4 Verify PDS is running (health check)

**Checkpoint 1:** PDS should be listening on port 3000:

```bash
ss -lntp | grep 3000
```

**Checkpoint 2:** Health endpoint should respond:

```bash
curl http://127.0.0.1:3000/xrpc/_health
```

This should return a JSON response with the PDS version. If it fails, check the logs with `journalctl -u pds -f`.

---

# Phase 7 — Reverse Proxy + HTTPS (nginx + certbot)

## 7.1 Install nginx + certbot

```bash
apt install -y nginx certbot python3-certbot-nginx
```

## 7.2 Issue WILDCARD TLS certificate

**Why wildcard?** Your PDS creates subdomain handles like `me.dashofextra.com`. Without a wildcard cert, those subdomains get TLS errors.

**GoDaddy limitation (2024):** GoDaddy restricted their API to accounts with 50+ domains, so we must use manual DNS-01 challenge:

```bash
certbot certonly --manual \
  -d dashofextra.com -d "*.dashofextra.com" \
  --preferred-challenges dns-01 \
  --server https://acme-v02.api.letsencrypt.org/directory
```

**When prompted:**
1. Certbot will ask you to create a TXT record: `_acme-challenge.dashofextra.com`
2. Log into GoDaddy DNS settings
3. Add TXT record: Name = `_acme-challenge`, Value = (the value certbot gives you)
4. Wait 2-5 minutes for DNS propagation
5. Press Enter in certbot

You'll be prompted **twice** (once for the base domain, once for the wildcard).

> **Certificate renewal:** Let's Encrypt certs expire after 90 days. With GoDaddy, you'll need to manually renew. Set a calendar reminder for day 60. Consider moving DNS to Cloudflare (free) for automated renewal.

## 7.3 Create nginx config

```bash
nano /etc/nginx/sites-available/pds
```

Paste:

```nginx
server {
    server_name dashofextra.com *.dashofextra.com;

    client_max_body_size 50M;

    # Helpful for handle/DID verification workflows
    location = /.well-known/atproto-did {
        proxy_pass http://127.0.0.1:3000;
        proxy_set_header Host $host;
    }

    location / {
        proxy_pass http://127.0.0.1:3000;
        proxy_http_version 1.1;

        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";

        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # CRITICAL: Prevent WebSocket timeout (needed for federation sync)
        # Without this, the relay connection drops after 60 seconds
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
    }

    listen 443 ssl;
    ssl_certificate /etc/letsencrypt/live/dashofextra.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/dashofextra.com/privkey.pem;
}

server {
    listen 80;
    server_name dashofextra.com *.dashofextra.com;
    return 301 https://$host$request_uri;
}
```

Enable and reload:

```bash
ln -s /etc/nginx/sites-available/pds /etc/nginx/sites-enabled/pds
rm -f /etc/nginx/sites-enabled/default
nginx -t && systemctl reload nginx
```

**Checkpoint:**

```bash
curl -I https://dashofextra.com
```

---

# Phase 8 — Bootstrap: Create invite + account

## Recommended approach: use admin tooling (preferred)

Depending on the PDS version/package, there may be an admin CLI (often referenced as `pdsadmin`).
Use it to:

* Create invite code
* Create first account

If the installed PDS package does NOT provide a CLI or the CLI is unclear, use the curl fallback below.

## Curl fallback: create invite code

Replace `YOUR_ADMIN_PASSWORD` with the value from your `pds.env` file:

```bash
curl -X POST -u "admin:YOUR_ADMIN_PASSWORD" \
  -H "Content-Type: application/json" \
  -d '{"useCount": 1}' \
  "https://dashofextra.com/xrpc/com.atproto.server.createInviteCode"
```

Copy the returned `code`.

## Curl fallback: create account (start with subdomain handle)

Create an account as a subdomain handle first:

* `me.dashofextra.com` (recommended initial handle)

```bash
curl -X POST \
  -H "Content-Type: application/json" \
  -d '{
    "email": "you@example.com",
    "handle": "me.dashofextra.com",
    "password": "PickAStrongPassword123!",
    "inviteCode": "PASTE_INVITE_CODE_HERE"
  }' \
  "https://dashofextra.com/xrpc/com.atproto.server.createAccount"
```

**Why not apex handle immediately?**
Apex (`dashofextra.com`) handle assignment can be finicky; start with a subdomain and switch later once stable.

## Verify DID registration

After creating your account, verify your identity registered correctly with the global directory:

```bash
# The createAccount response includes your DID. Use it here:
curl https://plc.directory/did:plc:YOUR_DID_HERE
```

If it returns a JSON document with your DID information, federation is working. If it fails, check your federation environment variables in `pds.env`.

---

# Phase 9 — Sign in from Bluesky

On Bluesky mobile app or web:

1. Sign in (or Create Account → choose custom hosting)
2. Hosting provider / service: `https://dashofextra.com`
3. Handle: `me.dashofextra.com`
4. Password: the one you set above

**Expected behavior**

* The client talks to your PDS for auth and repo actions
* Your PDS stores posts/data in its DBs and blobs directory
* Your content becomes visible on Bluesky through federation mechanisms

---

# Phase 10 — Optional: Switch to apex domain handle later

Once everything works with `me.dashofextra.com`, you can move to `dashofextra.com`.

Common pattern:

1. Determine your DID (from account info / admin tooling)
2. Add DNS TXT record in GoDaddy:

   * Name: `_atproto`
   * Value: `did=did:plc:YOUR_DID_HERE`
3. Update handle to `dashofextra.com` using the appropriate endpoint/tooling

> This step varies by tooling/client flow. Do it only after your base setup is stable.

---

# Operations: Troubleshooting & Maintenance

## View logs

```bash
journalctl -u pds -f
```

## Check PDS is listening

```bash
ss -lntp | grep 3000
```

## Check PDS health

```bash
curl http://127.0.0.1:3000/xrpc/_health
```

## Check nginx is healthy

```bash
nginx -t
systemctl status nginx --no-pager
```

## Check certificate expiration

```bash
certbot certificates
```

## Test certificate renewal (dry run)

```bash
certbot renew --dry-run
```

> **Note:** With GoDaddy, manual DNS challenge is required. Set a calendar reminder every 60 days to renew before the 90-day expiration.

## Common failure modes

* **PDS crashes on startup**: env var name mismatch for your installed version

  * Fix: inspect logs, adjust env var names accordingly
* **PDS starts but not federated**: missing federation env vars

  * Fix: ensure `PDS_BSKY_APP_VIEW_URL`, `PDS_CRAWLERS`, etc. are set in `pds.env`
* **TLS/cert issues**: port 80 blocked, DNS not propagated, certbot challenge fails

  * Fix: confirm DNS, confirm firewall, ensure nginx serving challenge path
* **Subdomain handles fail with TLS error**: missing wildcard certificate

  * Fix: issue wildcard cert with DNS-01 challenge (see Phase 7)
* **Client cannot login**: wrong service URL, proxy headers, or PDS not reachable

  * Fix: `curl -I https://dashofextra.com`, confirm 200/302 and no proxy loops
* **Federation sync drops after ~60 seconds**: missing WebSocket timeout in nginx

  * Fix: add `proxy_read_timeout 3600s;` and `proxy_send_timeout 3600s;` to nginx config

---

# Backups (minimal but correct)

Back up:

* `/srv/pds/data/`  (all DBs + blobs + repo state)
* `/etc/pds/pds.env` (secrets + identity keys)

Example backup:

```bash
tar -czf /root/pds-backup-$(date +%F).tar.gz /srv/pds/data /etc/pds/pds.env
```

Copy the tarball off the server (SCP, object storage, etc.).

> **CRITICAL WARNING:** The `PDS_PLC_ROTATION_KEY_K256_PRIVATE_KEY_HEX` in your `pds.env` is **irreplaceable**:
> - If you lose this key, you **permanently lose control** of your DID identity
> - You cannot reset it, recover it, or get a new one
> - Anyone with this key can take over your identity
>
> **Store this key in multiple secure locations** (encrypted password manager, offline backup, etc.). Treat `/etc/pds/pds.env` as your most critical file.

---

# Rollback / Uninstall (if you want to start over)

Stop services:

```bash
systemctl stop pds
systemctl disable pds
```

Remove unit:

```bash
rm -f /etc/systemd/system/pds.service
systemctl daemon-reload
```

Remove app/data (destructive):

```bash
rm -rf /srv/pds
rm -rf /etc/pds
```

Remove nginx site:

```bash
rm -f /etc/nginx/sites-enabled/pds
rm -f /etc/nginx/sites-available/pds
nginx -t && systemctl reload nginx
```

---

## Quick Reference: Key Files

| File | Purpose |
|------|---------|
| `/srv/pds/app/index.js` | PDS startup script |
| `/srv/pds/data/` | All persistent data (DBs, blobs) |
| `/etc/pds/pds.env` | Configuration and secrets (**CRITICAL - backup this!**) |
| `/etc/systemd/system/pds.service` | systemd service definition |
| `/etc/nginx/sites-available/pds` | nginx reverse proxy config |
| `/etc/letsencrypt/live/dashofextra.com/` | TLS certificates |

---

## Notes for an AI Agent executing this plan

* Be cautious with secrets: never print them into shared logs.
* If PDS fails due to unknown env vars, check the installed `@atproto/pds` package documentation for the exact env keys it expects (variable names can change between releases).
* Prefer smallest changes that restore forward progress; keep a running log of modifications.
* Validate each phase with a checkpoint before moving on.
