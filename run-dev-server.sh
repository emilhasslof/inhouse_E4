#!/bin/bash
# Start the stats server in development mode.
# Automatically seeds the 10 datagen players into the database.
# Open http://localhost:8080 once running.

GOLANG_PROTOBUF_REGISTRATION_CONFLICT=warn go run ./cmd/server
