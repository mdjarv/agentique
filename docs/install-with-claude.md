# Installing agentique with an agent

You are a coding agent helping someone install, upgrade or extend an agentique
setup on the machine you are running on. This file is your whole brief. The
[README](https://github.com/mdjarv/agentique#readme) is the reference behind it:
fetch it with `curl` when you need a config key or a command's detail this file
does not carry.

Agentique is one Go binary that serves a web UI for running coding agents
(Claude Code, optionally Codex) in parallel git worktrees. It can run
localhost-only, or be reachable over a tailnet so a phone, a laptop and other
machines drive it.

Work in four steps: **survey**, **ask**, **act**, **verify**. The survey is
read-only. Nothing is installed, written or restarted until the person has
picked what they want from what you found.

Expect a **half-finished install**. People often start by hand — the install
script, `agentique setup`, a foreground `agentique serve`, `tailscale up` — and
abort partway before asking you. Treat every step below as **reconcile**, not
install: find what already exists, keep what is valid, repair what is
inconsistent, and add only what is missing.

## Rules that hold in every step

- **Root is consent-gated.** Userspace installs you can run. Anything needing
  `sudo`, a system package manager or an installer that asks for elevation: show
  the exact command, say why, and run it only after a yes. If you cannot run
  interactive `sudo`, hand the command over for the person to run.
- **Human-only steps are handed over, never worked around.** Browser logins
  (`claude auth login`, `gh auth login`, `codex login`, `tailscale up`), the
  Tailscale admin console, registering a passkey and the **Add machine** click all
  need the person. Say exactly what to do and what they will see, then wait. In
  Claude Code they can run a command inside this conversation by prefixing it with
  `!`.
- **A restart ends in-flight turns.** Restarting or upgrading a running server
  kills every agent turn in progress on it; sessions themselves survive. Ask
  before any restart. With authentication on, the CLI cannot see sessions
  (`agentique sessions` answers 401), so ask the person whether anything is
  running in the UI rather than concluding nothing is. If
  `AGENTIQUE_OWNER_DATADIR` is set in your environment, you are yourself a session
  of that server, and restarting it ends this conversation's turn: finish every
  other step first, say so, and make the restart the last command.
- **Existing config is the person's.** Before changing `config.toml`, show its
  current contents with secret values masked (`embed-key`, `api-key`), copy it to
  `config.toml.bak`, and show the diff you intend. Never print a secret back.
- **One server per data directory.** Never start `agentique serve` in the
  foreground while a service is running. They share one database and the second
  one reaps the first one's processes.
- **Survey through the running server, not the database.** `agentique auth
  status`, `rekey` and `reset` open the database directly and run migrations, so
  a newly installed binary pointed at an older server's data upgrades its schema
  underneath it. Read state over HTTP (`/api/health`, `/api/auth/status`) and
  reserve the database commands
  for a stopped server whose binary is the one that will run next.
- **The data directory is kept.** It holds the database — a registered passkey,
  projects, possibly sessions from a run you did not see. Never delete it, the
  database or `machine-id`/`machine-identity-key.pem` to get a clean slate; a
  leftover is repaired in place. If the person explicitly wants to start over,
  move the directory aside with a timestamp rather than removing it.

## 1. Survey

Collect the current state. Run each probe and tolerate failures — a missing tool
is a finding, not an error. The step is done when every row of the table below
holds a value or the word `unknown`.

The probes assume the default port 9201. If a config exists, read its `addr`
first and use that port instead; the service may be answering somewhere else.

Linux and macOS:

```bash
uname -sm; echo "shell=$SHELL"; env | grep -q AGENTIQUE_OWNER_DATADIR && echo "inside an agentique session"
command -v agentique && agentique --version
agentique doctor            # dependency table; last line says whether required checks passed
agentique service status
cat ~/.config/agentique/config.toml 2>/dev/null \
  || cat ~/Library/Application\ Support/agentique/config.toml 2>/dev/null   # mask secrets before showing
curl -fsS  http://localhost:9201/api/health; echo
curl -fsSk https://localhost:9201/api/health; echo
command -v tailscale && tailscale status --json | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d["BackendState"], d["Self"]["DNSName"].rstrip("."), "certs:", d.get("CertDomains"))'
command -v tailscale && tailscale serve status
command -v node npx gh codex just

# Leftovers from an aborted attempt
which -a agentique; ls -la ~/.local/bin/agentique* /usr/local/bin/agentique* 2>/dev/null
ls -la ~/.local/share/agentique ~/Library/Application\ Support/agentique 2>/dev/null
pgrep -af 'agentique (serve|tray)'                       # a serve whose PID is not the service's runs outside it
cat ~/.local/share/agentique/agentique.pid 2>/dev/null   # compare with pgrep: a pid that is gone is stale
curl -fsS http://localhost:9201/api/auth/status; echo  # authEnabled false = auth off; credentialCount 0 = nobody registered
systemctl --user cat agentique 2>/dev/null | grep -E '^(ExecStart|Environment=PATH)'
systemctl --user is-failed agentique; journalctl --user -u agentique -n 30 --no-pager
loginctl show-user "$USER" -p Linger
launchctl list 2>/dev/null | grep -i agentique           # macOS
(ss -ltnp 2>/dev/null || lsof -nP -iTCP -sTCP:LISTEN) | grep -E ':(9201|443)\b'
```

Windows (PowerShell): `Get-Command agentique,claude,git,gh,node,codex,tailscale`,
`agentique doctor`, `agentique service status`, the config at
`$env:LOCALAPPDATA\agentique\config.toml`, and the same health and `tailscale`
calls; for leftovers, `schtasks /Query /TN agentique`,
`Get-Process agentique`, and whether
`$env:LOCALAPPDATA\Programs\agentique` is on the user `PATH` yet (the installer
adds it, but only new shells see it). Only amd64 binaries are published.

If `agentique` is absent, `doctor` is too: check `claude --version`,
`claude auth status`, `git --version`, `gh auth status` directly.

| Row | What to record |
|---|---|
| Platform | OS and arch; whether a published binary exists (linux amd64/arm64, darwin arm64, windows amd64) |
| agentique | not installed, or version and install path; latest release from `curl -fsSL https://api.github.com/repos/mdjarv/agentique/releases/latest` |
| Server | running as a service, running in the foreground, or stopped; health response |
| Config | path, or none; listen address, TLS, `rp-id`/`rp-origin`, `machine-label` |
| Reachability | localhost only, LAN, tailnet (by `tailscale serve` or its own TLS), or a reverse proxy |
| Registered | `credentialCount` from `/api/auth/status` on a running server |
| claude | installed version (>= 2.0.0 required) and authenticated account |
| git | version |
| Tailscale | not installed, logged out, running; MagicDNS name; HTTPS certificates enabled (`CertDomains` non-empty) |
| Extras | `gh` and its auth, `codex` and its auth, `node`/`npx`, shell completions |
| Leftovers | every row of the table below that matches, or none |
| Constraints | running inside an agentique session; root available; headless or desktop |

### Leftovers

A leftover is state that exists but does not add up to a working setup. Match the
survey against this table; each row names what it looks like and how it is
repaired, and repairs go through the ask step like everything else. When
something matches no row, describe it plainly and ask whether it was deliberate
before touching it.

| Looks like | Repair |
|---|---|
| Binary present but `command -v agentique` fails | Not on `PATH` yet. Add `~/.local/bin` (or the `INSTALL_DIR` used) to the shell config, and call the binary by absolute path meanwhile. |
| Two or more binaries on `PATH` (`which -a`, `/usr/local/bin` and `~/.local/bin`) | Pick one with the person, normally the newest in `~/.local/bin`. The service unit's `ExecStart` must name that one; reinstall the service after removing the other. |
| `agentique --version` newer than the `version` in `/api/health` | The installer ran but the running server was never restarted onto it. Restart under the restart rule. |
| Config written, no service | Usually `agentique setup` aborted after saving. Review the config against the goal, then install the service. |
| `disable-auth = true` in config | What setup writes for "localhost only". Fine for localhost; it must become `false` before any tailnet or proxy access, which it refuses by design. |
| `tls-cert` under the data dir's `certs/` | Setup's self-signed `localhost` certificate. Browsers will not run passkeys on it under any other name; replace it with Tailscale when remote access is wanted. |
| `rp-id`/`rp-origin` not matching the address the person uses | Passkeys fail with what looks like a broken authenticator. Correct both, then restart. Credentials registered under the old `rp-id` stop working: `agentique auth rekey` (server stopped) prints recovery codes. |
| A foreground `agentique serve` running | It holds the data directory. Ask the person to stop it (Ctrl-C in its terminal) before installing or restarting a service; kill it yourself only after a yes. |
| Stale `agentique.pid` or `agentique.lock` | Harmless: the lock is released by the OS when a process dies, and the pid file is ignored when its process is gone. Leave both. |
| Something else listening on 9201 | Not agentique if no agentique process owns it. Choose another port in `addr` rather than stopping the other program. |
| Service installed but failed or restarting | Read the journal lines. Common causes: `claude` missing from the unit's `Environment=PATH` (reinstall the service from a shell where `claude` is on `PATH`), a config error, or the binary path in `ExecStart` no longer existing. |
| `Linger=no` on a headless machine | The service stops at logout. `loginctl enable-linger "$USER"`, which `service install` normally runs. |
| Server answering, `credentialCount` 0 | Unfinished first run. On a network listener this is urgent: the first browser to register becomes admin. Register now, or bind to `localhost` until the person can. |
| A user registered, passkey lost | `agentique auth rekey` with the server stopped; register again with the printed code. |
| `agentique.prev` beside the binary | The copy an in-app upgrade keeps for `agentique rollback`. Deliberate; leave it. |
| The home directory shows up as a project in the UI | Left by an older release, whose first start registered the service's working directory when `initial-project` was unset. Harmless; the person can remove it in the UI and add the repo they meant. It does not come back. |
| Tailscale `BackendState` is `NeedsLogin` or `Stopped` | `tailscale up` was aborted or the node logged out. Rerun it; the person opens the URL. |
| `tailscale serve status` proxies a different port, or a stale entry | Point it at the port in `addr`. Remove only the entry that belongs to agentique; serve can carry other services. |
| `tailscale cert` files present, expired or for another name | Regenerate for the current MagicDNS name, or move to `tailscale serve` and drop `tls-cert`/`tls-key` from the config. |

## 2. Ask

Show the table, then give a short read of it: what is missing, what is broken,
which leftovers you found and what you think happened ("the installer finished,
setup was abandoned before the service step"). Lead with anything urgent, such
as a network listener nobody has registered on. Then ask what the person wants
help with, as a multi-select question (`AskUserQuestion` in Claude Code). Offer
only the options that apply to what you found, and mark the ones you recommend:

- **Install or upgrade agentique**
- **Repair leftovers** — each matched row, named, so the person can accept some and not others
- **Fix required dependencies** — whatever `doctor` failed
- **Run it as a background service**, localhost only
- **Reach it from other devices over Tailscale**
- **Pair this machine into another agentique UI**, or pair another machine into this one
- **Extras** — `gh` for pull requests, Codex as a second provider, the agent
  browser's system libraries, experimental features

The step is done when the person has chosen. If they name something outside the
list, fold it in. Then state the plan as the ordered commands you will run, each
marked with who performs it (you, you after a yes for root, or them), and wait
for a yes.

## 3. Act

Run only the branches that were chosen, in this order. Each ends on its own
check; do not start the next branch until it passes.

### Dependencies

- **claude** — if `doctor` reports it too old, update it the way it was
  installed: `doctor` prints the right command, and installing a second copy by
  another method leaves the older one running.
- **git** — the system package manager (root).
- **node/npx** — every session's agent browser runs `npx @playwright/mcp`, so a
  machine without `npx` gets agents with no browser. Userspace via
  `fnm`/`nvm` is fine.

Check: `agentique doctor` (or the direct probes) shows every required row passing.

### Install or upgrade

```bash
curl -fsSL https://raw.githubusercontent.com/mdjarv/agentique/master/install.sh | bash
```

Windows: `irm https://raw.githubusercontent.com/mdjarv/agentique/master/install.ps1 | iex`.

The installer verifies the release checksum, installs completions for `$SHELL`,
refreshes an existing service unit and runs `doctor`. It does not restart a
running service; that is `agentique service restart`, under the restart rule.
Put `~/.local/bin` on `PATH` in the person's shell config if the installer warns.

Check: `agentique --version` prints the latest tag.

### Configure and run as a service

`agentique setup` is an interactive wizard you cannot drive. Write `config.toml`
directly: `~/.config/agentique/config.toml` on Linux, the data directory on macOS
(`~/Library/Application Support/agentique/`) and Windows. `chmod 600` it. The
service runs a bare `agentique serve`, so every setting it needs lives in this
file.

Localhost only:

```toml
[server]
addr = "localhost:9201"

[setup]
initial-project = "/absolute/path/to/a/repo"   # ask which repo; omitted, no project until one is added in the UI
```

Then install the service **from a shell where `claude` is on `PATH`** — the Linux
unit pins `PATH` at install time:

```bash
agentique service install
agentique service status
```

On a desktop machine, `agentique service install --tray` also autostarts a tray
controller. On a small VPS, suggest `[session] idle-evict-timeout = "2h"` so idle
sessions give their memory back.

Check: `curl -fsS http://localhost:9201/api/health` returns `"status":"ok"`.
Then the person opens http://localhost:9201 and registers a passkey right away;
confirm `credentialCount` is at least 1 on `/api/auth/status`.

### Tailscale

Any origin other than `localhost` needs real HTTPS, and WebAuthn passkeys only
validate when `rp-id` and `rp-origin` match the hostname in the browser's address
bar. Tailscale supplies both the hostname and the certificate.

1. **Install** if absent — https://tailscale.com/download. On Linux that is
   `curl -fsSL https://tailscale.com/install.sh | sh` (root).
2. **Log in** — `sudo tailscale up`; the person opens the printed URL.
3. **Enable MagicDNS and HTTPS certificates** — the person does this once per
   tailnet at https://login.tailscale.com/admin/dns. Check: `CertDomains` in
   `tailscale status --json` is non-empty.
4. **Expose agentique.** Two shapes. Recommend A unless the person asks for B's
   one advantage.

**A. `tailscale serve` in front of a loopback listener (recommended).** Agentique
stays on `localhost`, so nothing is exposed on the LAN, and Tailscale terminates
TLS and renews the certificate itself.

```bash
tailscale serve --bg 9201      # on Linux, may need sudo or `sudo tailscale set --operator=$USER`
```

```toml
[server]
addr          = "localhost:9201"
rp-id         = "box.tailXXXX.ts.net"            # the MagicDNS name from the survey
rp-origin     = "https://box.tailXXXX.ts.net"
machine-label = "box"
```

The cost: another agentique's **Add machine** dialog discovers tailnet peers by
probing port 9201 directly, so it will not suggest this machine. Pairing still
works by pasting `https://box.tailXXXX.ts.net`.

**B. agentique serves TLS itself on the tailnet.**

```bash
mkdir -p ~/.config/agentique/tls
sudo tailscale cert --cert-file ~/.config/agentique/tls/cert.pem \
  --key-file ~/.config/agentique/tls/key.pem box.tailXXXX.ts.net
sudo chown "$USER" ~/.config/agentique/tls/*.pem && chmod 600 ~/.config/agentique/tls/key.pem
```

```toml
[server]
addr          = "0.0.0.0:9201"
tls-cert      = "/home/you/.config/agentique/tls/cert.pem"
tls-key       = "/home/you/.config/agentique/tls/key.pem"
rp-id         = "box.tailXXXX.ts.net"
rp-origin     = "https://box.tailXXXX.ts.net:9201"
machine-label = "box"
```

It is discoverable by peers, and it listens on every interface (authentication
stays on). The certificate lasts 90 days and the server reads it only at start,
so it needs a renewal job that re-runs `tailscale cert` and restarts the service —
and every renewal restart ends in-flight turns. Say that before choosing B.

Never use `tailscale funnel` here: it puts the login page on the public internet.

After either shape, restart the service (restart rule) or install it if it is not
installed.

Check: from this machine, `curl -fsS https://box.tailXXXX.ts.net/api/health`
(add `:9201` for B) returns ok. Then the person opens that URL on another device
and, if nobody has registered yet, registers a passkey immediately.

### Pairing

One agentique UI can drive several machines. The machine whose page the person
opens is the **primary**; ask which one that is. Both machines must be reachable
over HTTPS from the device running the browser, and the machine being added needs
a registered user first (`credentialCount` on `/api/auth/status`).

On the machine being added:

```bash
agentique pair --ttl 15m
```

It prints candidate addresses and a single-use token. The person then opens the
primary's UI, clicks the server icon in the sidebar footer, **Add machine**,
pastes the HTTPS address and the token. Each token pairs once; running it twice
makes two entries.

Check: `agentique auth sessions` on the added machine lists a `bearer` session.

### Extras

- **gh** — install from https://cli.github.com, then the person runs
  `gh auth login`. Enables pull requests from the UI.
- **Codex** — `npm install -g @openai/codex`, then the person runs `codex login`.
  Sessions can then pick Codex as their provider.
- **Agent browser libraries (Linux)** — Chromium downloads itself on first use,
  but a bare host can lack its shared libraries. `npx playwright install-deps
  chromium` installs them (root).
- **Experimental features** — teams, the browser panel, voice, the assistant and
  the brain are switches under `[experimental]` and `[brain]` in `config.toml`.
  Fetch the README's Configuration section and walk through only the ones the
  person asks about; voice needs a speech API key, which the person pastes
  into the file themselves.

## 4. Verify

The install is done when all of these hold for what was chosen:

- `agentique doctor` exits 0.
- `agentique service status` reports running (if a service was chosen).
- The health endpoint answers ok on every address the person means to use.
- Re-running the leftover probes matches no row except those the person chose to keep.
- `/api/auth/status` reports `credentialCount` of at least 1 on every network-reachable
  server.
- Each pairing shows up in `agentique auth sessions`.

Then report: what changed (files written, packages installed, services
restarted), the URL or URLs to open, any step still waiting on the person, and
anything from the survey you chose not to touch.
