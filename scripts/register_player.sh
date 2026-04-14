#!/usr/bin/env bash
set -e

API_BASE="${API_BASE:-https://inhousee4-production.up.railway.app}"

read -rp "Display name: " display_name
read -rp "Steam ID:      " steam_id

if [[ -z "$display_name" || -z "$steam_id" ]]; then
  echo "Error: both display name and Steam ID are required." >&2
  exit 1
fi

echo "POST $API_BASE/api/register"
response=$(curl -s -w "\n%{http_code}" -X POST "$API_BASE/api/register" \
  -H "Content-Type: application/json" \
  -d "{\"display_name\": \"$display_name\", \"steam_id\": \"$steam_id\"}")

body=$(echo "$response" | head -n -1)
status=$(echo "$response" | tail -n 1)

echo "HTTP $status"
echo "$body" | jq . 2>/dev/null || echo "$body"

[[ "$status" =~ ^2 ]] || exit 1
