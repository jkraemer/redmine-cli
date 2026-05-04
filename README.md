# redmine-cli

Agent-friendly CLI for the Redmine/Planio API.

## Build

    make build

## Configure

Set env vars:

    export REDMINE_URL=https://your.redmine.example
    export REDMINE_API_KEY=...

Or create `~/.config/redmine-cli/config.toml`:

    url = "https://your.redmine.example"
    api_key = "..."

## Use

    ./redmine-cli projects list
    ./redmine-cli issues list --project myproj --status open --limit 10
    ./redmine-cli issues get 1459
    ./redmine-cli attachments download 42

See `./redmine-cli --agent --help` for machine-readable help.
