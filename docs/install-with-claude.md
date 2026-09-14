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
  kills every agent turn in progress on it; sessions themselves survive. Check
  `agentique sessions` for running ones and ask before any restart. If
  `AGENTIQUE_OWNER_DATADIR` is set in your environment, you are yourself a session
  of that server, and restarting it ends this conversation's turn: finish every
  other step first, say so, and make the restart the last command.
- **Existing config is the person's.** Before changing `config.toml`, show its
  current contents with secret values masked (`embed-key`, `api-key`), copy it to
  `config.toml.bak`, and show the diff you intend. Never print a secret back.
- **One server per data directory.** Never start `agentique serve` in the
  foreground while a service is running. They share one database and the second
  one reaps the first one's processes.

## 1. Survey

Collect the current state. Run each probe and tolerate failures — a missing tool
is a finding, not an error. The step is done when every row of the table below
holds a value or the word `unknown`.

Linux and macOS:

```bash
uname -sm; echo "shell=$SHELL"; env | grep -q AGENTIQUE_OWNER_DATADIR && echo "inside an agentique session"
command -v agentique && agentique --version
agentique doctor            # dependency table; last line says whether required checks passed
agentique service status
agentique sessions 2>/dev/null | head -20
cat ~/.config/agentique/config.toml 2>/dev/null \
  || cat ~/Library/Application\ Support/agentique/config.toml 2>/dev/null   # mask secrets before showing
curl -fsS  http://localhost:9201/api/health; echo
curl -fsSk https://localhost:9201/api/health; echo
command -v tailscale && tailscale status --json | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d["BackendState"], d["Self"]["DNSName"].rstrip("."), "certs:", d.get("CertDomains"))'
command -v tailscale && tailscale serve status
command -v node npx gh codex just
```

Windows (PowerShell): `Get-Command agentique,claude,git,gh,node,codex,tailscale`,
`agentique doctor`, `agentique service status`, the config at
`$env:LOCALAPPDATA\agentique\config.toml`, and the same health and `tailscale`
calls. Only amd64 binaries are published.

If `agentique` is absent, `doctor` is too: check `claude --version`,
`claude auth status`, `git --version`, `gh auth status` directly.

| Row | What to record |
|---|---|
| Platform | OS and arch; whether a published binary exists (linux amd64/arm64, darwin arm64, windows amd64) |
| agentique | not installed, or version and install path; latest release from `curl -fsSL https://api.github.com/repos/mdjarv/agentique/releases/latest` |
| Server | running as a service, running in the foreground, or stopped; health response; busy sessions |
| Config | path, or none; listen address, TLS, `rp-id`/`rp-origin`, `machine-label` |
| Reachability | localhost only, LAN, tailnet (by `tailscale serve` or its own TLS), or a reverse proxy |
| Registered | whether a passkey user exists — `agentique auth status` once a server runs |
| claude | installed version (>= 2.0.0 required) and authenticated account |
| git | version |
| Tailscale | not installed, logged out, running; MagicDNS name; HTTPS certificates enabled (`CertDomains` non-empty) |
| Extras | `gh` and its auth, `codex` and its auth, `node`/`npx`, shell completions |
| Constraints | running inside an agentique session; root available; headless or desktop |

## 2. Ask

Show the table, then give a short read of it: what is missing, what is broken,
and what looks unfinished (a network listener with no user registered is urgent —
the first browser to register becomes the admin). Then ask what the person wants
help with, as a multi-select question (`AskUserQuestion` in Claude Code). Offer
only the options that apply to what you found, and mark the ones you recommend:

- **Install or upgrade agentique**
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

- **claude** — if absent, install with the official native installer from
  https://claude.com/product/claude-code, then the person runs
  `claude auth login`. If one is installed but old, update it the way it was
  installed: `doctor` prints the right command, and an npm install over a native
  one leaves two copies where the older one keeps running.
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
initial-project = "/absolute/path/to/a/repo"   # ask which repo; omitted, the service registers $HOME
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
confirm with `agentique auth status`.

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
a registered user first (`agentique auth status`).

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
- `agentique auth status` shows a registered user on every network-reachable
  server.
- Each pairing shows up in `agentique auth sessions`.

Then report: what changed (files written, packages installed, services
restarted), the URL or URLs to open, any step still waiting on the person, and
anything from the survey you chose not to touch.
