---
title: Self-host with Docker Compose
description: Run Goal on one machine with task up, which starts PostgreSQL and RustFS in Docker Compose and runs goald and the auth service on the machine, or task up:compose, which runs them in Compose too.
---

You end with Goal running on your machine at `http://127.0.0.1:8080`, with its database, object storage and sign-in working. Everything runs on that one machine. `task up` runs Goal's own services as processes on the machine. `task up:compose` runs them as Compose services.

## What you need

- Docker with Compose, running on the machine.
- A checkout of this repository, with [Task](https://taskfile.dev) and the tools in `.mise.toml` installed. `task up` builds everything else it needs.
- Node.js, for the auth service, when sign-in is on (the default).

## 1. Set the secrets

`task up` loads `.env` from the repository root when it exists. Copy `.env.example` to `.env` and set:

- the database passwords (`GOAL_PG_PASSWORD`, `GOAL_PG_OWNER_PASSWORD`, `GOAL_PG_APP_PASSWORD`, `GOAL_PG_AUTH_PASSWORD`). Goal refuses the passwords this repository publishes, so set your own.
- `BETTER_AUTH_SECRET`, the sign-in secret, and `GOAL_BOOTSTRAP_OWNER_EMAIL`, the email of the first owner. Both are needed while nobody has signed in yet.

`.env.example` lists every variable. Do not commit `.env`.

## 2. Start the platform

```sh
task up
```

`task up` first builds the `goal` and `goald` binaries into `bin/`, the auth service, the egress proxy image and the agent image. Then it runs `goal up`, which starts PostgreSQL and RustFS in Compose, creates the database roles, applies the migrations, creates the bucket, starts the egress proxy, and runs `goald` and the auth service on this machine. With sign-in on, it also starts Mailpit. It keeps running until you press Ctrl-C.

Open `http://127.0.0.1:8080` and sign in with the owner's email. The sign-in code arrives in Mailpit, at `http://127.0.0.1:8025`. Its user and password are in the file `goal up` names when it starts.

`goal up` does not push any factory. Create one with [Change a factory definition](../change-a-factory-definition/).

To use the single local user without sign-in, set `GOAL_AUTH=none` before `task up`. Then the web app and the CLI act as that user and need no sign-in. Mailpit does not start.

If you run `bin/goal up` directly instead of `task up`, nothing builds for you and `.env` is not loaded. Export the variables from `.env` into the shell first, and build the prerequisites with `task build auth:build egress:image worker:image:build`.

## 3. Or run goald and the auth service in Compose

```sh
task up:compose
```

`task up:compose` runs `goal up --compose`. It starts `goald` and the auth service as Compose services (`goald` and `auth`, in the `stack` profile), and returns once they are healthy. This needs sign-in on (`GOAL_AUTH=session`, the default). The services keep running afterwards, so you do not need to keep the terminal open.

Follow their logs, and stop them when you are done:

```sh
docker compose logs -f goald
docker compose stop goald auth
```

## Stop

With `task up`, press Ctrl-C where it runs. Then stop the Compose services:

```sh
docker compose down
```

`docker compose down` keeps the data in the Compose volumes; it does not delete the database or the bucket.

## If it does not start

- A refusal that names `GOAL_AUTH=none` means the sign-in settings are missing. Set the variables in step 1, or use `GOAL_AUTH=none`.
- A refusal about a published password means `.env` still has one of the repository's example passwords.
- A refusal that names the auth service's build means `apps/auth/dist/server.js` is missing. Run `task up`, which builds it, or `task auth:build`.
- If the web app does not answer on port 8080, check the `goal up` output for the address it printed. `GOAL_PUBLIC_URL` changes the address that sign-in links use.

## Related

- [Change a factory definition](../change-a-factory-definition/)
- [Concepts](/docs/concepts/) for the parts of the platform.
