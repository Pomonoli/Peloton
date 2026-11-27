# Codex Cloud development preview

From the Peloton checkout, run:

```sh
./scripts/dev-preview.sh
```

Use a Linux workspace with Docker Compose, Go and Node matching `go.mod` and
`web/.nvmrc`, plus Python 3, curl, Make, and `flock`. If TypeScript protobuf
stubs are missing, put the protobuf tools on `PATH` first. Run
`make proto-tools` for Buf and the Go plugins, and install the
`@bufbuild/protoc-gen-es` version pinned in `docker/Dockerfile.build-base`.

The launcher uses the local Docker daemon and the existing development Compose
services. It creates a separate `peloton_preview` PostgreSQL database and
`peloton-preview` object bucket. It does not source the working copy's
`dev.env`. The Go server runs under Air on loopback port 8090. Vite runs on
`0.0.0.0:3000` and proxies authentication and API requests to Go. Vite and
Air pick up frontend and Go changes on save. The processes stay up after the
launcher exits.

In Codex, open **Edit Environment → HTTP Previews**, add port **3000**, and
choose **Apply to this chat**. Open the URL Codex shows for that port. Once
you have the URL, pass it to the launcher so Vite accepts its hostname and
Peloton generates share, reset and invite links for the HTTPS preview.

```sh
PELOTON_PREVIEW_URL='<HTTP Preview URL from Codex>' ./scripts/dev-preview.sh
```

After resuming the same workspace, rerun the same command. Compose volumes
persist the database and the gitignored `.dev-preview/` directory holds the
generated passwords, logs and seed marker. If only one process stopped, the
launcher starts it again. A new workspace generates new passwords. If port
3000 is occupied, set `PELOTON_PREVIEW_PORT` and configure that port in Codex.

## Test accounts

| Sign-in email | Role | Password in `.dev-preview/accounts.env` |
| --- | --- | --- |
| `admin@peloton.local` | Admin | `PREVIEW_ADMIN_PASSWORD` |
| `maya@peloton.local` | Rider | `PREVIEW_RIDER_ONE_PASSWORD` |
| `noah@peloton.local` | Rider | `PREVIEW_RIDER_TWO_PASSWORD` |

The launcher generates the passwords locally. The fixtures use synthetic GPX
uploads to populate activities, segments, records and recorded routes. Routes
and other derived data may take a short while to appear after the initial seed.

Mailpit catches outgoing test mail on loopback. Its inbox is available from the
Cloud workspace at `http://127.0.0.1:8025`. Provider integrations that need live
credentials or a separately enrolled worker require their own test setup.
