# redmine-safe

A locked, key-hiding way to let an LLM agent read one Redmine instance.

The agent can read the instance through `redmine-cli` but never sees the API
key and can never write to it. The key matters because an agent that holds
it could skip the CLI and call the REST API directly, bypassing every
restriction the CLI enforces, including read-only mode. So the key is kept
under a different UID (`redmine-svc`) that the kernel keeps out of the
agent's reach, and the agent reaches the CLI through one narrow `sudo` rule.
The policy itself (reads only, no `auth`, no `--config`, attachments to
stdout only) is enforced by redmine-cli's locked mode (`REDMINE_LOCKED`), not
by the wrapper scripts, which do no argument parsing. The ordinary
`redmine-cli` stays available to the agent for other instances.

This is a one-time, by-hand setup per machine. It is not installed by the
release or the skill bootstrap.

```
agent --> redmine-safe (shim) --> sudo -u redmine-svc --> run --> redmine-cli (locked) --> Redmine
```

## Security assumptions

The design holds only if all of these are true. Nothing in the code checks
them.

- **The agent cannot become root or `redmine-svc` by any other route.** In
  particular the agent's user must not have `NOPASSWD: ALL` sudo; check with
  `sudo -n true`, which must fail.
- **Run the agent as its own user.** If the agent runs as the same user as
  the human, a still-valid sudo timestamp after the human types a password
  would let the agent use sudo; `Defaults:<agent-user> timestamp_timeout=0`
  (see `sudoers.example`) closes that. It does not close the bigger hole: an
  agent running as the human's user can also edit the human's shell startup
  files (`~/.bashrc`, `~/.zshrc`, ...) or alias `sudo` and capture the
  password the next time the human types it. Only a separate agent user
  prevents that. A shared user is a convenience setup, not a secure one.
- **The locked binary, `run`, their directory, and its parents
  (`/usr/local`, `/usr/local/libexec`) are root-owned** and not writable by
  the agent or `redmine-svc`. Never point `run` at a binary in the agent's
  home (such as the one bundled with the redmine-cli skill): the agent could
  replace it and have it run as `redmine-svc`.
- **`run` uses absolute paths only.** sudo keeps the caller's `PATH` unless
  `secure_path` is set (it is off in sudo's upstream default), so a bare
  command name in `run` (`env` instead of `/usr/bin/env`) would let the agent
  put its own program first in `PATH` and have it run as `redmine-svc`. Keep
  this in mind if you edit `run`.
- **`no_new_privs` breaks it, safely.** If the agent's shell runs with
  `no_new_privs` (for example the Claude Code Bash sandbox), sudo cannot
  switch users and `redmine-safe` fails. It does not leak anything.
- **Process isolation.** The key exists in the memory of a `redmine-svc`
  process only. The agent cannot read its `/proc/<pid>/environ` or memory, or
  ptrace it, because it runs under a different UID.

## Install

All steps as root, except where noted. `<agent-user>` is the user the agent
runs as.

1. Create the service user. Refuse if the home directory already exists, so
   you do not adopt a directory someone else can write to:

       test ! -e /home/redmine-svc || { echo "/home/redmine-svc exists" >&2; exit 1; }
       useradd -r -m -d /home/redmine-svc -s /usr/sbin/nologin redmine-svc

2. Create the config, owned by `redmine-svc`, directory 0700, file 0600:

       install -d -o redmine-svc -g redmine-svc -m 0700 /home/redmine-svc/.config/redmine-cli
       install -o redmine-svc -g redmine-svc -m 0600 /dev/null /home/redmine-svc/.config/redmine-cli/config.toml
       $EDITOR /home/redmine-svc/.config/redmine-cli/config.toml

   Content (API key only; locked mode refuses OAuth):

       url = "https://your.redmine.example"
       api_key = "..."

3. Create the root-owned directory, and check that its parents are
   root-owned and not writable by anyone else:

       install -d -o root -g root -m 0755 /usr/local/libexec/redmine-safe
       ls -ld /usr/local /usr/local/libexec /usr/local/libexec/redmine-safe

4. Install the locked binary and the `run` script, both root:root 0755.

   Nothing the agent can write to may end up here. If the agent runs as the
   same user as the human, that user's home is agent-writable, so a checkout
   of this repository or a binary built there is **not** trusted, and
   neither is anything in `/tmp`. As root, download the release binary into
   a root-only directory and verify it against the release's
   `checksums.txt` (releases: https://github.com/jkraemer/redmine-cli/releases;
   pick one that includes locked mode, see `CHANGELOG.md`):

       VERSION=x.y.z    # without the leading v
       ARCH=amd64       # or arm64
       ARCHIVE=redmine-cli_${VERSION}_linux_${ARCH}.tar.gz
       BASE=https://github.com/jkraemer/redmine-cli/releases/download/v${VERSION}
       install -d -o root -g root -m 0700 /root/redmine-safe-download
       cd /root/redmine-safe-download
       curl -fsSL -o "$ARCHIVE" "$BASE/$ARCHIVE"
       curl -fsSL -o checksums.txt "$BASE/checksums.txt"
       grep " $ARCHIVE\$" checksums.txt | sha256sum -c -
       tar -xzf "$ARCHIVE" redmine-cli
       install -o root -g root -m 0755 /root/redmine-safe-download/redmine-cli /usr/local/libexec/redmine-safe/redmine-cli

   `sha256sum -c` must print `OK`; stop otherwise. Then create `run` by
   typing its contents (do not copy it from a checkout the agent can write
   to); compare with `run` in this directory if you like, it is short:

       $EDITOR /usr/local/libexec/redmine-safe/run
       chown root:root /usr/local/libexec/redmine-safe/run
       chmod 0755 /usr/local/libexec/redmine-safe/run

   Contents:

       #!/bin/sh
       cd / || exit 1
       exec /usr/bin/env -i HOME=/home/redmine-svc PATH=/usr/bin:/bin REDMINE_LOCKED=1 \
         /usr/local/libexec/redmine-safe/redmine-cli "$@"

   Every command in `run` must be an absolute path (see Security
   assumptions); `cd` is a shell builtin.

5. Install the sudoers rule by editing in place; visudo validates the file
   before saving it. Paste the `Defaults` line and the rule from
   `sudoers.example` and replace `<agent-user>`:

       visudo -f /etc/sudoers.d/zz-redmine-safe

   Do not stage the file in `/tmp` or any other world-writable directory:
   another user could replace it between the check and the copy. The `zz-`
   prefix matters: sudo uses the last matching rule, so the file must sort
   after any general rule for the agent's user.

6. Put the shim on the agent's PATH, for example as
   `/usr/local/bin/redmine-safe` (root:root 0755) with these contents:

       #!/bin/sh
       exec sudo -n -u redmine-svc /usr/local/libexec/redmine-safe/run "$@"

   (The shim is harmless if modified, since it only calls sudo, which is
   governed by the sudoers rule, but keeping it root-owned avoids surprises.)

7. Install the agent skill, as the agent's user. For example with the
   skills CLI from the repository root:

       npx skills add -g ./contrib/redmine-safe/skill

   or copy `skill/SKILL.md` to the agent's skills directory
   (e.g. `~/.agents/skills/redmine-safe/SKILL.md`).

## Verify

Run as the agent's user:

- `sudo -n -l` lists the `redmine-safe/run` rule as the only `NOPASSWD`
  entry. If you see `NOPASSWD: ALL`, stop and fix it.
- `redmine-safe users me` prints the account of the API key.
- `cat /home/redmine-svc/.config/redmine-cli/config.toml` fails with
  "Permission denied".
- `redmine-safe issues update 1 --notes x` exits 8 (check with `echo $?`).

## Updating the locked binary

Reinstall the binary as root, as in install step 4 (download, verify
against `checksums.txt`, `install`). `npx skills update` and the skill's
`bootstrap.sh` do not touch it, on purpose: it is root-owned so the agent
cannot swap it.

## Limitation

Under `no_new_privs` (for example the Claude Code Bash sandbox) sudo cannot
change user, so `redmine-safe` fails with a sudo error. This is safe, but it
means the agent must run `redmine-safe` outside such a sandbox.
