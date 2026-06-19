# Backup & Sync

journal auto-commits your notes to git the moment you capture them. `journal sync` is the next step: it gets those commits off your machine to a git remote, and pulls in notes you may have captured from another device.

> **Sync is off by default.** Nothing happens until you explicitly enable it. This is by design — pushing to and pulling from a remote is a bigger operation, and you should opt in deliberately.

## What sync does

When you run `journal sync`, it:

1. Commits any pending note changes
2. Fetches the remote
3. Pushes your local commits if you're ahead
4. Merges remote changes if you're behind (and re-indexes new notes)
5. Handles divergence according to your `sync_conflict` setting

## Setting it up

### 1. Add a remote

Point your journal repo at a git remote (a private GitHub repo works well):

```sh
git remote add origin git@github.com:you/your-journal.git
git push -u origin HEAD
```

### 2. Enable sync in config

In `.journal/config.yaml`:

```yaml
sync_enabled: true
sync_conflict: manual    # see below
```

### 3. Test it

```sh
journal sync --dry-run   # preview: shows what would happen without doing it
journal sync             # do it for real
```

---

## Conflict modes

If you capture notes on more than one machine, the remote and your local clone can diverge (both have new commits). How journal resolves that is up to you:

| Mode | What happens on a conflict | Best for |
|---|---|---|
| `manual` (default) | Aborts cleanly, tells you to run `git pull` | Multiple machines, you want control |
| `prefer-upstream` | Takes the remote version on any conflict | Single authoritative remote |
| `prefer-local` | Keeps your local version on any conflict | This machine is the source of truth |

`prefer-upstream` and `prefer-local` resolve conflicts automatically. The losing side's changes disappear from the working tree (they're still in git history). Only opt in if you know what you're doing.

A clean fast-forward (one side is simply ahead) always just works, regardless of this setting.

---

## Running sync on a schedule

`journal init` drops a helper script at `.journal/sync.sh`. Wire it to a cron job for hourly backups:

```cron
# back up the journal every hour
0 * * * * /path/to/journal/.journal/sync.sh >> /path/to/journal/.journal/sync.log 2>&1
```

**macOS (launchd):** see `.journal/README.md` in your repo for the full plist recipe.

**Linux (systemd timer):**

```sh
# ~/.config/systemd/user/journal-sync.timer
[Timer]
OnCalendar=hourly
Persistent=true     # run a missed backup after the machine wakes up
```

```sh
systemctl --user enable --now journal-sync.timer
```

While `sync_enabled: false`, each run just prints a "sync is disabled" notice and exits harmlessly — safe to wire up before you're ready to enable it.

---

## Sync and privacy

`journal sync` pushes your notes to a git remote you control — it's not sending data to a cloud AI service. If your remote is a private GitHub repository, your notes are as private as that repo.

It's independent of `local_only`. You can have `local_only: true` (no cloud AI) and `sync_enabled: true` (backup to your own remote) at the same time.
