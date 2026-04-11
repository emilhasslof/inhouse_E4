#!/usr/bin/env bash
# create-lobby.sh — Register HACKERMAN (if needed) and create a lobby.
# Usage: ./create-lobby.sh [--prod]

API_BASE="http://localhost:8080"
if [ "${1:-}" = "--prod" ]; then
    API_BASE="https://inhousee4-production.up.railway.app"
fi

STEAM_ID="76561197990491029"
DISPLAY_NAME="HACKERMAN"

echo "Registering $DISPLAY_NAME..."
REG_STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
    -X POST "$API_BASE/api/register" \
    -H "Content-Type: application/json" \
    -d "{\"steam_id\":\"$STEAM_ID\",\"display_name\":\"$DISPLAY_NAME\"}")

if [ "$REG_STATUS" -eq 201 ]; then
    echo "Registered."
elif [ "$REG_STATUS" -eq 409 ]; then
    echo "Already registered."
else
    echo "Registration failed (HTTP $REG_STATUS)"
    exit 1
fi

echo "Creating lobby..."
curl -s -X POST "$API_BASE/api/lobby/create" \
    -H "Content-Type: application/json" \
    -d "{\"steam_ids\":[\"$STEAM_ID\"]}" | python3 -m json.tool
