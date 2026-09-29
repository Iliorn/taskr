# Syncing between devices

taskr syncs through a small server you run yourself. One machine holds the
shared copy, and there is no third-party service. The same `taskr` program is
both the server and the client.

## Run a server

On the machine that should hold the shared copy (a home server reachable over
Tailscale or your LAN, for example):

```sh
taskr serve --listen 100.x.y.z:8765 --token "$(openssl rand -hex 32)"
# or: TASKR_SYNC_TOKEN=… taskr serve --listen 100.x.y.z:8765
```

A token is **required**; taskr refuses to run a server without one.
`--listen` defaults to `127.0.0.1:8765`; bind to a Tailscale or LAN address
so other devices can reach it. Tailscale already encrypts the connection;
anywhere else, give the server a certificate and it serves https itself:

```sh
taskr serve --listen 0.0.0.0:8765 --tls-cert cert.pem --tls-key key.pem
taskr sync --url https://tasks.example.com:8765 --save
```

The certificate is re-read when its files change, so one renewed in place
(`tailscale cert`, Let's Encrypt) is picked up without a restart.

To keep the server running, wrap it in a `systemd --user` unit with the token
in an `EnvironmentFile` (mode 600) and enable lingering. See
[SECURITY.md](../SECURITY.md#choosing-a-sync-token) for choosing a token.

The server keeps its own `tasks.db` and answers on:

- `POST /v1/sync`: sync (needs the token)
- `GET  /v1/health`: a liveness check
- `GET  /v1/events`: a stream that tells connected clients to pull now

## Point a device at it

```sh
taskr sync --url http://100.x.y.z:8765 --token "<token>" --save
```

`--save` stores the address and token, so later syncs need no flags;
`TASKR_SYNC_URL` / `TASKR_SYNC_TOKEN` work too. From then on the app syncs by
itself (at launch and exit, every few minutes, and as soon as another device
changes something), and CLI commands sync in the background. Set
`"auto_sync": false` in `sync.json` to sync only when you run `taskr sync`.

All of this is also in the app's **Settings** tab: turn auto-sync on or off,
edit the server address and token, "Sync now", or make this machine the
server.

Your own copy of the tasks always comes first: a network failure never blocks
the app.

The first sync from a device that already has tasks asks what to do with
them: add them to the shared copy (`--adopt-local`) or replace them with it
(`--adopt-remote`, which saves a backup first). It asks because neither can
be undone once the other devices have them.

## How changes are merged

Each task is matched by its ID. For a field changed on two devices, the later
edit wins; comments and time entries from both are kept. A deleted task
leaves a marker so the delete reaches other devices instead of the task
coming back. When an edit loses to a delete, taskr says so briefly and writes
the lost version to `sync.log` in the state directory, so nothing is gone for
good.

Because "later" is decided by each device's clock, clocks should be roughly
right (any normal NTP setup is enough). Tasks and the board's column names
sync; other settings stay per device.
