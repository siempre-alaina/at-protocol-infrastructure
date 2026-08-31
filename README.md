# AT Protocol Infrastructure

An experiment in running your own corner of a social network.

Most social platforms keep three things welded together: your identity, your data,
and the app you view them through. Leave the platform and you lose all three. The
[AT Protocol](https://atproto.com) — the open standard underneath Bluesky — pulls
them apart, so your identity and your posts can live on a server you control while
any number of apps read from it.

This repository is a hands-on exploration of what that actually takes. It contains
a working self-hosted server, a from-scratch implementation of the piece that
distributes data between servers, and a custom data format for the idea it sets out
to test: **using a social protocol as the substrate for a marketplace where AI agents
hire each other.**

---

## The concepts, in plain English

If you're new to the protocol, these five terms are most of it.

| Term | What it actually is |
|---|---|
| **PDS** (Personal Data Server) | Your own filing cabinet. It stores your posts and holds your account. Ours runs the official Bluesky server software. |
| **DID** (Decentralized Identifier) | Your identity, as a portable ID that isn't owned by any company. Because it's separate from the PDS, you can move servers and keep your followers. |
| **Firehose** | A live stream of everything happening on a server, published as it happens. Anything that wants to react to new posts drinks from it. |
| **Relay** | A switchboard. It subscribes to many PDS firehoses, merges them into one ordered stream, and rebroadcasts it. Without relays, every app would have to connect to every server individually. |
| **Lexicon** | A schema. It defines what a record looks like — a post has text, a like points at something. Anyone can define new record types, which is what makes the protocol extensible rather than fixed. |

The short version: **a PDS holds your data, a relay moves it around, and lexicons
describe its shape.**

---

## How the pieces fit

```
      ┌──────────────┐        ┌──────────────┐
      │  Bluesky App │        │  Other apps  │
      └──────┬───────┘        └──────┬───────┘
             │ read posts            │ subscribe to firehose
             ▼                       ▼
   ┌─────────────────────────────────────────────┐
   │              Your infrastructure            │
   │                                             │
   │   ┌───────────┐         ┌───────────────┐   │
   │   │    PDS    │────────▶│     Relay     │   │
   │   │           │  events │               │   │
   │   │ • posts   │         │ • merges      │   │
   │   │ • account │         │ • orders      │   │
   │   │ • DID     │         │ • rebroadcasts│   │
   │   └───────────┘         └───────┬───────┘   │
   │                                 │           │
   │                         ┌───────▼───────┐   │
   │                         │  Feed viewer  │   │
   │                         │  (web page)   │   │
   │                         └───────────────┘   │
   └─────────────────────────────────────────────┘
```

A post is written to the PDS. The PDS announces it on its firehose. The relay picks
it up, gives it a global sequence number, saves it, and pushes it to anyone
listening — including the small web page in this repo that renders the stream live.

---

## What's in this repository

**`pds-server/`** — Deployment setup for a Personal Data Server: an automated
install script, web server config, a service definition, and a step-by-step build
guide. The PDS software itself is Bluesky's; this is everything around it needed to
get one running on a plain Linux box.

**`relay/`** — A relay written from scratch in Go (~2,800 lines). This is the
substantial piece. It connects to one or more PDS firehoses, verifies cryptographic
signatures on incoming data, assigns global sequence numbers via PostgreSQL, lets
clients resume from any point in the stream, exposes operational metrics, and serves
a browser-based feed viewer.

**`lexicons/`** — A custom record type, `com.dashofextra.marketplace.jobCard`. It
describes a job posting: what the task is, what it pays, when it's due, and what
service levels are expected. The idea being tested is that if AI agents each have a
protocol identity, they can advertise work and bid on it over open social
infrastructure rather than through a private API owned by one company. The relay
knows how to decode and display these alongside ordinary posts.

---

## Status: this is a prototype

**It is not production software, and it should not be deployed as-is.**

It was built to learn how the protocol works from the inside — the fastest way to
understand a specification is to implement it — and it succeeds at that. It runs, it
carries real data, and the architecture is sound.

The gaps below are listed because they were identified and scoped deliberately,
rather than discovered in production. Each is the difference between something
that demonstrates a mechanism and something safe to operate:

- **The firehose output isn't spec-compliant.** It emits JSON rather than the
  binary format the standard requires, so real AT Protocol software can't consume
  it. The feed viewer in this repo can. Closing this gap is the main work remaining.
- **Signature verification is off by default**, and when enabled it logs failures
  rather than rejecting the data. That's fine for observing a stream you trust; it
  is not sufficient for a relay carrying data from servers you don't.
- **There are no automated tests** and no continuous integration.
- **Several endpoints lack authentication and rate limiting**, and there are known
  concurrency bugs that can crash the process under load.
- **Stored data grows without bound** — there's no retention or cleanup policy.

Treat this as a reference implementation and a learning artifact. Read it, run it
locally, borrow from it. Don't put it on the open internet.

The deployment documentation throughout describes a live instance that has since
been decommissioned. URLs and server addresses in those guides are historical.

---

## Running it locally

The relay needs Go 1.25+ and, optionally, PostgreSQL 16 for persistence.

```bash
cd relay
go build -o relay ./cmd/relay
cp relay.yaml my-config.yaml     # then edit: set a PDS host and a database password
./relay --config my-config.yaml --debug
```

It will connect to whatever PDS hosts you list under `ingest.initial_hosts` and
stream their events. Add `--db` to persist to PostgreSQL, and `--verify` to turn on
signature checking. `relay/README.md` covers configuration, endpoints, and
deployment in full; `pds-server/pds-server/pds-implementation-plan.md` walks through
standing up a PDS from a bare server.

## Configuration and secrets

No credentials are stored in this repository. Files ending in `.example` are
templates — copy them, remove the suffix, and fill in your own values. The real
files are gitignored.

```bash
cp pds-server/pds-server/server-config/pds.env.example \
   pds-server/pds-server/server-config/pds.env
```

One warning worth repeating: `PDS_PLC_ROTATION_KEY_K256_PRIVATE_KEY_HEX` is the
private key controlling your identity. Anyone who has it can take over your DID
permanently, and it cannot be recovered if lost. Generate it, back it up offline,
and never commit it.
