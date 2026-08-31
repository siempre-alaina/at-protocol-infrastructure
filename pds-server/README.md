# PDS Server

Deployment setup for a self-hosted AT Protocol Personal Data Server.

The PDS software itself is [Bluesky's](https://github.com/bluesky-social/pds); this
directory holds everything around it — an automated install script, nginx and
systemd configuration, and a step-by-step build guide.

- **[Implementation plan](pds-server/pds-implementation-plan.md)** — full walkthrough
  from a bare Ubuntu server to a running PDS.
- **[Server notes](pds-server/README.md)** — configuration reference for the
  deployment described in that plan.

See the [root README](../README.md) for how this fits together with the relay, and
for the prototype caveats that apply to this repository as a whole.

## Secrets

Configuration templates end in `.example`. Copy them, drop the suffix, and fill in
your own values — the real files are gitignored.

```bash
cp pds-server/server-config/pds.env.example pds-server/server-config/pds.env
```
