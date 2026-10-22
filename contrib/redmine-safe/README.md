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
  `sudo -n true`, which must fail. If the agent runs as the same user as the
  human, a still-valid sudo timestamp after the human types a password would
  reopen the hole, so set `Defaults:<agent-user> timestamp_timeout=0` (see
  `sudoers.example`). A separate agent user is the stronger setup and is
  recommended.
- **The locked binary, `run`, and their directory are root-owned** and not
  writable by the agent or `redmine-svc`. Never point `run` at a binary in
  the agent's home (such as the one bundled with the redmine-cli skill): the
  agent could replace it and have it run as `redmine-svc`.
- **`no_new_privs` breaks it, safely.** If the agent's shell runs with
  `no_new_privs` (for example the Claude Code Bash sandbox), sudo cannot
  switch users and `redmine-safe` fails. It does not leak anything.
- **Process isolation.** The key exists in the memory of a `redmine-svc`
  process only. The agent cannot read its `/proc/<pid>/environ` or memory, or
  ptrace it, because it runs under a different UID.

## Install

All steps as root, except where noted. `<agent-user>` is the user the agent
runs as (on Jens's machine: `jk`).

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

3. Create the root-owned directory:

       install -d -o root -g root -m 0755 /usr/local/libexec/redmine-safe

4. Install the locked binary (a release binary or your own build) and the
   `run` script, both root:root 0755:

       install -o root -g root -m 0755 /path/to/redmine-cli /usr/local/libexec/redmine-safe/redmine-cli
       install -o root -g root -m 0755 run /usr/local/libexec/redmine-safe/run

5. Install the sudoers rule. Copy `sudoers.example` to a temporary file,
   replace `<agent-user>`, then:

       visudo -c -f /tmp/zz-redmine-safe
       install -o root -g root -m 0440 /tmp/zz-redmine-safe /etc/sudoers.d/zz-redmine-safe

   Or edit directly with `visudo -f /etc/sudoers.d/zz-redmine-safe`. The
   `zz-` prefix matters: sudo uses the last matching rule, so the file must
   sort after any general rule for the agent's user.

6. Put the shim on the agent's PATH, for example:

       install -m 0755 redmine-safe /usr/local/bin/redmine-safe

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

Reinstall the binary as root, as in install step 4. `npx skills update` and
the skill's `bootstrap.sh` do not touch it, on purpose: it is root-owned so
the agent cannot swap it.

## Limitation

Under `no_new_privs` (for example the Claude Code Bash sandbox) sudo cannot
change user, so `redmine-safe` fails with a sudo error. This is safe, but it
means the agent must run `redmine-safe` outside such a sandbox.
