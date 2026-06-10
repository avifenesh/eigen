# Automation: non-interactive eigen loops

eigen's automation model is deliberately simple: eigen runs ONE task headless
and exits; the **host** (cron, systemd, a shell loop) re-launches it. Each run
re-reads its prompt, so you edit the work file and the next run picks it up.

## Task sources (all imply `-p` headless mode)
- `eigen -p --prompt-file work.md` — re-reads `work.md` every run.
- `echo "do the thing" | eigen -p` — piped stdin.
- `eigen -p "do the thing"` — positional.

Exit code: `0` on success, non-zero on error — so the host can back off.

## Shell loop (eigen restarts itself after each run)
```sh
while :; do
  eigen -p --prompt-file ~/work/next.md || sleep 60
  sleep 300   # wait 5 min, then do the next iteration
done
```

## systemd timer (preferred for "run when not running")
`~/.config/systemd/user/eigen-work.service`:
```ini
[Service]
Type=oneshot
ExecStart=%h/.local/bin/eigen -p --prompt-file %h/work/next.md
```
`~/.config/systemd/user/eigen-work.timer`:
```ini
[Timer]
OnUnitInactiveSec=10min
[Install]
WantedBy=timers.target
```
`systemctl --user enable --now eigen-work.timer`. `OnUnitInactiveSec` only
re-fires after the previous run FINISHED — exactly "start when it closed, do
something, go back."

## Combine with --continue to keep one evolving session
```sh
eigen -p --continue --prompt-file ~/work/next.md
```
