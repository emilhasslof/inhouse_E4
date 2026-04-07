#!/bin/bash
# Start the fake GSI data generator.
# Requires the server to be running first (run-server.sh).
#
# Commands:
#   start  — begin a simulated match (10 players, 1 payload/sec each)
#   stop   — end the match and trigger post-game stats
#   status — show current match state
#   quit   — exit

go run ./cmd/datagen
