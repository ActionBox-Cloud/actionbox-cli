<img src="https://actionbox.cloud/appbox.svg" width="64" alt="Actionbox logo">

# Actionbox CLI

Actionbox is `input()` for software that cannot wait at the terminal. The CLI
lets scripts, CI jobs, scheduled tasks, and AI agents ask a human for a decision
and continue when the answer arrives.

## Install

Install the platform-specific CLI wrapper from PyPI:

```bash
python -m pip install actionbox
```

## Get started

Create a Source in the Actionbox dashboard, then configure the CLI with its API
key:

```bash
actionbox configure
actionbox doctor
actionbox ask "Deploy to production?" --option approve --option reject --wait
```

Attach a structured Decision Brief when the reviewer needs the reason, risk,
and rollback path in front of them:

```bash
actionbox ask "Deploy 2.18.0?" \
  --assignee-email reviewer@example.com \
  --option approve --option reject \
  --decision-class production_deployment \
  --decision-context-json @decision-context.json \
  --wait
```

`--assignee-email` routes the Action to an active reviewer in the Source
workspace. You can instead use `--assignee-user-id` with the stable ID copied
from **Settings → Team**. Both flags are optional; use at most one. Omit both to
use the workspace default reviewer or normal plan routing.

`decision-context.json` uses the same `decision_context` object documented by
the API. Supporting diffs, logs, metrics, commands, and links still belong in
`--context-json`.

When `actionbox get ACTION_ID --json` includes a pending `context_request`, add
the requested evidence or Decision Brief to that same Action:

```bash
actionbox update ACTION_ID \
  --decision-class production_deployment \
  --decision-context-json @decision-context.json \
  --context-json @evidence.json
```

If the requested material cannot be supplied, say so rather than leaving the
reviewer waiting:

```bash
actionbox update ACTION_ID \
  --context-unavailable "Production data is not accessible to this worker." \
  --context-unavailable-code cannot_access
```

The waiting command exits after the Action is resolved. Stopping it with
`Ctrl+C` only stops local polling; the Action remains open in Actionbox.

In recognized GitHub Actions, GitLab CI, Jenkins, and CircleCI environments,
the CLI adds bounded origin metadata such as the workflow reference, repository
or branch labels, and build URL. Use `--no-origin` when that context should not
be sent with an Action.

Paid plans can attach secondary operations with `--controls-json`. Callback
controls require `--callback-url`. ActionBox delivers a signed request to that
URL, and your integration decides how to execute it.

```bash
actionbox send "Worker failed" \
  --callback-url https://example.com/actionbox \
  --controls-json '[{"key":"retry","label":"Retry worker","kind":"callback"}]'
```

## Scheduled-job monitoring

Create and manage heartbeat Watches from the CLI, or wrap an existing command
so Actionbox receives start, success, and failure signals:

```bash
actionbox watch list
export ACTIONBOX_WATCH_URL="https://api.actionbox.cloud/hb/..."
actionbox run -- ./backup.sh
```

Keep Source API keys and Watch URLs in a secret manager or protected CI
environment. Supplying the Watch URL through `ACTIONBOX_WATCH_URL` keeps it out
of the Actionbox process arguments. Do not embed capabilities in browser code,
logs, or source control.

## Documentation

- [Actionbox documentation](https://actionbox.cloud/docs)
- [Actionbox website](https://actionbox.cloud)

## License

MIT.
