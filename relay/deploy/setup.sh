#!/bin/bash
# AT Protocol Relay - Server Setup Script
# Run on the DigitalOcean droplet (<YOUR_SERVER_IP>)

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

# Check if running as root
if [[ $EUID -ne 0 ]]; then
   log_error "This script must be run as root"
   exit 1
fi

log_info "Starting AT Protocol Relay setup..."

# ============================================
# 1. Install PostgreSQL 16
# ============================================
log_info "Installing PostgreSQL 16..."

# Add PostgreSQL APT repository
if ! command -v psql &> /dev/null; then
    apt-get update
    apt-get install -y gnupg2 lsb-release

    # Add PostgreSQL repo
    sh -c 'echo "deb http://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main" > /etc/apt/sources.list.d/pgdg.list'
    wget --quiet -O - https://www.postgresql.org/media/keys/ACCC4CF8.asc | apt-key add -

    apt-get update
    apt-get install -y postgresql-16 postgresql-contrib-16

    systemctl enable postgresql
    systemctl start postgresql

    log_info "PostgreSQL 16 installed"
else
    log_info "PostgreSQL already installed"
fi

# ============================================
# 2. Create relay database and user
# ============================================
log_info "Setting up PostgreSQL database..."

# Generate a random password
RELAY_DB_PASS=$(openssl rand -hex 16)

sudo -u postgres psql <<EOF
-- Create user if not exists
DO \$\$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'relay') THEN
        CREATE ROLE relay WITH LOGIN PASSWORD '${RELAY_DB_PASS}';
    END IF;
END
\$\$;

-- Create database if not exists
SELECT 'CREATE DATABASE relay OWNER relay'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'relay')\gexec

-- Grant permissions
GRANT ALL PRIVILEGES ON DATABASE relay TO relay;
EOF

log_info "Database 'relay' created with user 'relay'"

# ============================================
# 3. Install Go 1.21+
# ============================================
log_info "Installing Go..."

GO_VERSION="1.21.6"
if ! command -v go &> /dev/null; then
    wget -q "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -O /tmp/go.tar.gz
    rm -rf /usr/local/go
    tar -C /usr/local -xzf /tmp/go.tar.gz
    rm /tmp/go.tar.gz

    # Add to PATH
    echo 'export PATH=$PATH:/usr/local/go/bin' > /etc/profile.d/go.sh
    source /etc/profile.d/go.sh

    log_info "Go ${GO_VERSION} installed"
else
    log_info "Go already installed: $(go version)"
fi

# ============================================
# 4. Create relay user and directories
# ============================================
log_info "Creating relay user and directories..."

# Create relay user
if ! id -u relay &>/dev/null; then
    useradd --system --shell /bin/false --home /srv/relay relay
    log_info "Created system user 'relay'"
fi

# Create directories
mkdir -p /srv/relay/{app,data/cars,logs}
mkdir -p /etc/relay

# Set ownership
chown -R relay:relay /srv/relay
chmod 750 /srv/relay

log_info "Directories created"

# ============================================
# 5. Create environment file
# ============================================
log_info "Creating environment file..."

cat > /etc/relay/relay.env <<EOF
# AT Protocol Relay Environment Configuration
# Generated on $(date)

# Database
RELAY_DATABASE_POSTGRES_URL=postgres://relay:${RELAY_DB_PASS}@localhost/relay?sslmode=disable

# Override config values via environment if needed
# RELAY_SERVER_PORT=2470
# RELAY_INGEST_WORKER_COUNT=4
EOF

chmod 600 /etc/relay/relay.env
chown root:relay /etc/relay/relay.env

log_info "Environment file created at /etc/relay/relay.env"

# ============================================
# 6. Configure firewall
# ============================================
log_info "Configuring firewall..."

# Allow PostgreSQL local connections only
ufw allow from 127.0.0.1 to any port 5432 proto tcp

# Relay port (internal only, nginx will proxy)
# ufw allow 2470/tcp  # Uncomment if direct access needed

log_info "Firewall configured"

# ============================================
# 7. Create DNS record reminder
# ============================================
log_info ""
log_info "============================================"
log_info "MANUAL STEPS REQUIRED:"
log_info "============================================"
log_info ""
log_info "1. Add DNS A record:"
log_info "   relay.service.dashofextra.com -> <YOUR_SERVER_IP>"
log_info ""
log_info "2. Copy nginx config:"
log_info "   cp nginx-relay.conf /etc/nginx/sites-available/relay"
log_info "   ln -s /etc/nginx/sites-available/relay /etc/nginx/sites-enabled/"
log_info "   nginx -t && systemctl reload nginx"
log_info ""
log_info "3. Deploy relay binary (already cross-compiled):"
log_info "   scp relay-linux-amd64 root@<YOUR_SERVER_IP>:/srv/relay/app/relay"
log_info "   ssh root@<YOUR_SERVER_IP> 'chown relay:relay /srv/relay/app/relay && chmod +x /srv/relay/app/relay'"
log_info ""
log_info "4. Copy systemd service:"
log_info "   cp relay.service /etc/systemd/system/"
log_info "   systemctl daemon-reload"
log_info "   systemctl enable relay"
log_info "   systemctl start relay"
log_info ""
log_info "5. Database password saved in /etc/relay/relay.env"
log_info "   Password: ${RELAY_DB_PASS}"
log_info ""
log_info "============================================"
log_info "Setup complete!"
log_info "============================================"
