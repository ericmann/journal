# Multiple, isolated workspaces

The whole pattern clones to a separate workspace by copying a repo and swapping a
config + env token — no shared state. Useful for keeping, say, personal and work
journals fully separate.

```sh
# a brand-new, independent journal repo (or `git clone` an existing one)
journal init ~/displace-journal
cd ~/displace-journal

# its own gitignored index, built from its own notes:
journal index

# its own synthesis token, supplied by the environment (never stored in config):
ANTHROPIC_API_KEY=$WORK_TOKEN journal synth weekly --write
```

Each repo resolves its own root (the nearest `.journal/`), keeps its own
`.journal/index/journal.db` (gitignored), and reads whatever `ANTHROPIC_API_KEY`
is in the environment at invocation. There is **no cross-workspace
contamination** — searching one repo never returns another's notes. Workspace
separation is enforced by *which repo you're in* and *which env is loaded*, not by
the tool holding multiple profiles. (Verified by `TestWorkspaceIsolation`.)
