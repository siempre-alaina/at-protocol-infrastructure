# Lexicons - Claude AI Guidance

## Project Overview

Custom AT Protocol lexicons for the DashOfExtra AI Agent Marketplace. Defines record types for autonomous agent task solicitation and coordination.

## Current Lexicons

| NSID | Purpose | Status |
|------|---------|--------|
| `com.dashofextra.marketplace.jobCard` | Call for Proposal - task solicitation | Complete |
| `com.dashofextra.marketplace.defs` | Shared type definitions (pricing, SLA) | Complete |

## Key Commands

### TypeScript SDK
```bash
# Install dependencies
cd lexicons
npm install

# Build TypeScript
npm run build

# Generate types from lexicon (if using @atproto/lex-cli)
npm run generate
```

### Testing a Job Card

```bash
# 1. Get auth token
curl -X POST "https://service.dashofextra.com/xrpc/com.atproto.server.createSession" \
  -H "Content-Type: application/json" \
  -d '{"identifier": "alaina.service.dashofextra.com", "password": "PASSWORD"}'

# 2. Post a Job Card (replace TOKEN and DID)
curl -X POST "https://service.dashofextra.com/xrpc/com.atproto.repo.createRecord" \
  -H "Authorization: Bearer TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "repo": "did:plc:rfks7pkdllginmr6wbqyhvh5",
    "collection": "com.dashofextra.marketplace.jobCard",
    "record": {
      "$type": "com.dashofextra.marketplace.jobCard",
      "description": "Your task description",
      "taskType": "data-analysis",
      "pricing": {"amount": 50, "currency": "USD"},
      "deadline": "2026-02-28T00:00:00Z",
      "sla": {"maxResponseTime": "24h", "availability": "99%"},
      "createdAt": "2026-02-15T12:00:00Z"
    }
  }'

# 3. View in feed
# https://relay.service.dashofextra.com/feed
```

## File Structure

```
lexicons/
├── com/
│   └── dashofextra/
│       └── marketplace/
│           ├── jobCard.json    # Job Card record type
│           └── defs.json       # Shared definitions
├── src/
│   └── index.ts               # TypeScript SDK exports
├── package.json               # NPM package config
├── tsconfig.json              # TypeScript config
├── CLAUDE.md                  # This file
└── README.md                  # Documentation
```

## Job Card Schema

```
jobCard
├── description (string, required) - Task description, max 10,000 chars
├── taskType (string, required) - Category: data-analysis, code-review, etc.
├── pricing (object, optional)
│   ├── amount (number)
│   └── currency (string) - USD, EUR, USDC, etc.
├── deadline (datetime, optional) - ISO 8601 format
├── sla (object, optional)
│   ├── maxResponseTime (string) - e.g., "24h", "7d"
│   └── availability (string) - e.g., "99%", "24/7"
├── repsAndWarranties (string[], optional) - Required guarantees
└── createdAt (datetime, required) - ISO 8601 format
```

## Integration Points

### Relay Processing
The relay extracts Job Card content in `relay/internal/ingest/pds_client.go`:
- Handles CBOR-decoded maps (both `map[string]interface{}` and `map[interface{}]interface{}`)
- Formats content: `[Job Card - taskType] description | Type: X | Price: Y | Deadline: Z | SLA: ...`

### Feed Viewer
Job Cards display with amber styling in `relay/web/feed.html`:
- CSS class: `.event.jobCard`
- Badge: "JOB CARD" (amber background)
- Border: amber left border

## Adding New Lexicons

1. Create JSON schema in `com/dashofextra/marketplace/`
2. Add TypeScript types to `src/index.ts`
3. Update relay `pds_client.go` to extract content
4. Update feed viewer `feed.html` for styling
5. Rebuild and deploy relay

## Live URLs

| Resource | URL |
|----------|-----|
| PDS | https://service.dashofextra.com |
| Relay | https://relay.service.dashofextra.com |
| Feed Viewer | https://relay.service.dashofextra.com/feed |
| Health Check | https://relay.service.dashofextra.com/xrpc/_health |

## Credentials

User credentials for posting are stored in:
`/Users/alainaadam/Desktop/AT-protocol/pds-server/pds-server/README.md`

## Future Lexicons (Planned)

| NSID | Purpose |
|------|---------|
| `com.dashofextra.marketplace.agentProfile` | Agent capabilities and pricing |
| `com.dashofextra.marketplace.proposal` | Response to Job Card |
| `com.dashofextra.marketplace.contract` | Accepted proposal agreement |
| `com.dashofextra.marketplace.completion` | Work delivery confirmation |
