# Droplet deploy runbook

Everything the production droplet needs lives in this directory. A merge to `main` in
`oniwebsite` or `oniwebsite_bk` builds in CI, then that same artifact is copied to one
DigitalOcean droplet: Caddy terminates HTTPS on 443 and proxies to the Go server on
`127.0.0.1:8080`, which also serves the frontend `dist/`.

| File | Installed as | Purpose |
|---|---|---|
| `bootstrap.sh` | run once (idempotent) | Sets up the whole host from a fresh Ubuntu LTS droplet |
| `oniweb.service` | `/etc/systemd/system/oniweb.service` | Runs `server -port 8080` as user `oni` |
| `Caddyfile` | `/etc/caddy/Caddyfile` (domain rendered in) | HTTPS reverse proxy |
| `deploy-frontend` | `/usr/local/sbin/deploy-frontend` | Atomic `dist` symlink swap, keeps last 3 releases |
| `deploy-backend` | `/usr/local/sbin/deploy-backend` | Binary + locales swap, restart, health check, auto-rollback |

Droplet layout:

```
/opt/oni/oniwebsite/dist -> releases/<sha>      # frontend, swapped per deploy
/opt/oni/oniwebsite/releases/<sha>/
/opt/oni/oniwebsite_bk/{server,server.prev,locales,locales.prev}
/etc/oni/oniweb.env                              # SENTRY_DSN, set by hand, never touched by CI
```

## One-time setup (manual)

1. Create the smallest-tier droplet (Ubuntu LTS) with your SSH key.
2. Add a DNS `A` record for the domain pointing at the droplet IP.
3. Generate a dedicated CI key pair (never reuse a personal key):
   ```sh
   ssh-keygen -t ed25519 -N '' -C ci-deploy -f oni-deploy
   ```
4. On the droplet, as root, from a checkout of `oniwebsite_bk`:
   ```sh
   ONI_DOMAIN=example.com DEPLOY_PUBKEY="$(cat oni-deploy.pub)" ./deploy/bootstrap.sh
   ```
   Optional `ADMIN_USER` (default `oniadmin`). The script creates that sudo user from root's
   `authorized_keys` **before** it disables root login and password auth, and refuses to
   touch sshd if that user has no key. The name must not match an existing group, so
   `admin` is rejected on Ubuntu, which already ships a group of that name. If you ever
   lock yourself out, use the DigitalOcean console. From now on log in as that user
   (`oniadmin` by default), not `root`.
5. Put the error-tracking DSN in the env file, then restart later via a deploy:
   ```sh
   echo 'SENTRY_DSN=https://...' | sudo tee /etc/oni/oniweb.env >/dev/null
   ```
   GlitchTip itself is out of scope: point the DSN at a hosted or separate instance.
6. In **each** repo create the GitHub environment `production` (Settings → Environments),
   restrict it to the `main` branch, and add:

   | Name | Kind | Value |
   |---|---|---|
   | `DROPLET_HOST` | environment secret | droplet IP or hostname CI connects to |
   | `DEPLOY_SSH_KEY` | environment secret | contents of the private key `oni-deploy` |
   | `DEPLOY_KNOWN_HOSTS` | environment secret | output of `ssh-keyscan -t ed25519 <host>` (see below) |
   | `DOMAIN` | environment variable | public hostname, e.g. `example.com`, used by the smoke test |

   `DEPLOY_KNOWN_HOSTS` pins the host key so CI never trusts on first use. Generate it and
   compare the fingerprint with the droplet's before saving:
   ```sh
   ssh-keyscan -t ed25519 <host> | tee known_hosts.txt
   ssh-keygen -lf known_hosts.txt                          # on your machine
   ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub        # on the droplet: must match
   ```
7. In `oniwebsite` only, add the repository-level Actions **variable** `VITE_SENTRY_DSN`
   (not an environment secret: the CI build job is outside `production`, and the value
   ships in the public bundle anyway).

## First deploy

Deploy the **frontend before the backend**. A backend deployed first has no `dist/` to
serve, so `/` returns 500 and the smoke test fails.

1. Merge to `main` in `oniwebsite`, wait for its `deploy` job. Expect its **Smoke test** step
   to fail with a 502 on this very first deploy: the release is already uploaded and `dist`
   already swapped, but `oniweb` is enabled and not running yet (it needs the backend build),
   so Caddy has nothing to proxy to.
2. Merge to `main` in `oniwebsite_bk`, wait for its `deploy` job. It starts the service, and
   its smoke test should pass.
3. Re-run the failed frontend `deploy` job (Actions → the run → **Re-run failed jobs**) so it
   turns green now that the service is up.

Afterwards, frontend and backend deploy independently on their own merges. A frontend that
ships slightly before its backend may show raw translation keys until the backend deploy
finishes, and an already-open tab may 404 on old hashed assets until reload; both are accepted.

## Rollback

Run on the droplet as the admin user (`ADMIN_USER`, default `oniadmin`).

**Frontend** (needs the release to still be among the last 3 in `releases/`):
```sh
ls -t /opt/oni/oniwebsite/releases
sudo -u deploy /usr/local/sbin/deploy-frontend <sha>
```

**Backend** (only needed after a bad release that passed the health check; a failed health
check already rolls back by itself and turns the job red):
```sh
cd /opt/oni/oniwebsite_bk
sudo -u deploy sh -c 'cp -p server.prev server.rollback && mv -f server.rollback server && rm -rf locales && cp -a locales.prev locales'
sudo systemctl restart oniweb
```

Useful checks: `journalctl -u oniweb -n 50`, `systemctl status oniweb caddy`,
`curl -s http://127.0.0.1:8080/api/translations?lang=en`.

## Acceptance checklist (after the first deploys)

- `https://<domain>/`, `/pt/` and `/sv/` return 200 with a valid certificate.
- `curl -I http://<domain>` redirects to HTTPS.
- Port 8080 is not reachable from outside (`curl http://<droplet-ip>:8080` fails); UFW allows only 22/80/443.
- `ssh root@<host>` and password login are refused.
- `free -m` after acceptance: confirm 512 MB RAM is enough with the swapfile as a safety net
  (resize the droplet if it swaps heavily).

### Bad-binary drill (proves automatic rollback)

1. On a scratch branch merged to `main` (or by running the deploy manually), ship a
   deliberately broken `server` (for example a tiny program that exits immediately).
2. Expect the `deploy` job to go red at the health check, log "rolling back", and leave the
   previous version serving: `https://<domain>/` still returns 200 and `server` matches
   `server.prev`.
3. Deploy a good build to return to normal.

## Testing the scripts without a droplet

`deploy-frontend` and `deploy-backend` read `ONI_ROOT` (default `/opt/oni`); `deploy-backend`
also reads `HEALTH_URL` and `HEALTH_ATTEMPTS`. Point them at a temp directory, put stub
`sudo` and `systemctl` executables first on `PATH`, and serve a fake
`/api/translations` with `python3 -m http.server` to exercise swap, prune and rollback.
CI runs `shellcheck` on all three scripts.
