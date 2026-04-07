# TODO

Anyone (human or agent) can add items here at any time. No ceremony required.
Mark items done with `[x]` when complete, or remove them.

---

## Up next

- [ ] Run a real match with GSI active and inspect the `allplayers` block — does it include enemy stats for a non-spectator?
- [ ] Build a player registration flow (right now players are added manually via raw SQL)
- [ ] Distribute personalised GSI config files to all league members

## Backlog

- [ ] Deploy to Fly.io and verify the full pipeline with a real match
- [ ] Gold-over-time graph on the match detail page (data is already in `gsi_snapshots`)
- [ ] Player profile page (`/players/:id`) with match history
- [ ] Win/loss record on the leaderboard
- [ ] Hero stats — most played, best KDA per hero

## Done

- [x] Prove GSI data can be received and parsed locally (`gsi/main.go`)
- [x] Scaffold Go server with SQLite, GSI ingest, and HTMX web pages
- [x] Dev datagen tool for testing the pipeline without a real match
