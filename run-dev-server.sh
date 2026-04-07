#!/bin/bash
# Start the stats server in development mode.
# Automatically seeds the 10 datagen players into the database.
# Open http://localhost:8080 once running.

APP_ENV=development go run ./cmd/server
