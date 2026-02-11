#!/bin/bash
#
# AT Protocol PDS Setup Script
# Automates Phases 2-3 of the implementation guide
#
# This script prepares a fresh Ubuntu server for running a PDS:
# - Updates the system
# - Configures the firewall
# - Creates the pds user and directories
# - Installs Node.js 22 LTS and build tools
# - Installs nginx and certbot
#
# Usage: sudo bash setup.sh
#

set -e  # Exit on any error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Helper functions
info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    error "This script must be run as root. Use: sudo bash setup.sh"
fi

echo ""
echo "=============================================="
echo "  AT Protocol PDS Server Setup Script"
echo "=============================================="
echo ""
echo "This script will:"
echo "  1. Update the system packages"
echo "  2. Configure the firewall (ufw)"
echo "  3. Create the 'pds' service user"
echo "  4. Create required directories"
echo "  5. Install Node.js 22 LTS"
echo "  6. Install nginx and certbot"
echo ""
read -p "Continue? (y/n) " -n 1 -r
echo ""
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Aborted."
    exit 0
fi

# ============================================
# Phase 2.2: Update the OS
# ============================================
info "Updating system packages..."
apt update && apt upgrade -y

# ============================================
# Phase 2.3: Configure firewall
# ============================================
info "Configuring firewall (ufw)..."

# Check if ufw is installed
if ! command -v ufw &> /dev/null; then
    info "Installing ufw..."
    apt install -y ufw
fi

# Configure firewall rules
ufw allow 22/tcp comment 'SSH'
ufw allow 80/tcp comment 'HTTP'
ufw allow 443/tcp comment 'HTTPS'

# Enable firewall (non-interactive)
echo "y" | ufw enable

info "Firewall configured. Current status:"
ufw status

# ============================================
# Phase 2.4: Create dedicated service user
# ============================================
info "Creating 'pds' service user..."

if id "pds" &>/dev/null; then
    warn "User 'pds' already exists, skipping creation"
else
    adduser --system --group --home /srv/pds --shell /usr/sbin/nologin pds
    info "User 'pds' created"
fi

# ============================================
# Phase 2.5: Create directories
# ============================================
info "Creating directories..."

mkdir -p /srv/pds/app /srv/pds/data /srv/pds/data/blobs
mkdir -p /etc/pds

chown -R pds:pds /srv/pds
chmod 700 /etc/pds

info "Directories created:"
echo "  /srv/pds/app   - Application code"
echo "  /srv/pds/data  - Persistent data (DBs, blobs)"
echo "  /etc/pds       - Configuration and secrets"

# ============================================
# Phase 3: Install Node.js 22 LTS + build tools
# ============================================
info "Installing Node.js 22 LTS and build tools..."

apt install -y curl ca-certificates gnupg build-essential

# Check if Node.js is already installed
if command -v node &> /dev/null; then
    CURRENT_NODE=$(node -v)
    warn "Node.js is already installed: $CURRENT_NODE"
    read -p "Do you want to reinstall/upgrade to Node.js 22? (y/n) " -n 1 -r
    echo ""
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        info "Keeping existing Node.js installation"
    else
        curl -fsSL https://deb.nodesource.com/setup_22.x | bash -
        apt install -y nodejs
    fi
else
    curl -fsSL https://deb.nodesource.com/setup_22.x | bash -
    apt install -y nodejs
fi

info "Node.js version: $(node -v)"
info "npm version: $(npm -v)"

# ============================================
# Install nginx and certbot
# ============================================
info "Installing nginx and certbot..."

apt install -y nginx certbot python3-certbot-nginx

# Start and enable nginx
systemctl enable nginx
systemctl start nginx

info "nginx installed and running"

# ============================================
# Summary
# ============================================
echo ""
echo "=============================================="
echo "  Setup Complete!"
echo "=============================================="
echo ""
echo "What was done:"
echo "  [x] System packages updated"
echo "  [x] Firewall configured (ports 22, 80, 443)"
echo "  [x] User 'pds' created"
echo "  [x] Directories created (/srv/pds, /etc/pds)"
echo "  [x] Node.js $(node -v) installed"
echo "  [x] nginx and certbot installed"
echo ""
echo "Next steps (continue with the implementation guide):"
echo ""
echo "  1. Initialize the Node.js project:"
echo "     cd /srv/pds/app"
echo "     sudo -u pds npm init -y"
echo "     sudo -u pds npm install @atproto/pds"
echo ""
echo "  2. Create /srv/pds/app/index.js (see Phase 4 in guide)"
echo ""
echo "  3. Generate secrets and create /etc/pds/pds.env (see Phase 5)"
echo ""
echo "  4. Create systemd service (see Phase 6)"
echo ""
echo "  5. Issue wildcard TLS certificate (see Phase 7):"
echo "     certbot certonly --manual \\"
echo "       -d YOUR_DOMAIN.com -d \"*.YOUR_DOMAIN.com\" \\"
echo "       --preferred-challenges dns-01 \\"
echo "       --server https://acme-v02.api.letsencrypt.org/directory"
echo ""
echo "  6. Configure nginx (see Phase 7)"
echo ""
echo "See pds-implementation-plan.md for full details."
echo ""
