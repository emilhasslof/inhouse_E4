#!/usr/bin/env bash
# register.sh — Inhouse League player registration (Linux)
# Reads your Steam ID from loginusers.vdf, registers with the backend,
# and writes the GSI config to your Dota 2 installation.
set -euo pipefail

API_BASE="https://inhousee4-production.up.railway.app"
BOT_PROFILE="https://steamcommunity.com/profiles/76561198719296562"

# Colours
CYAN='\033[0;36m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
GRAY='\033[0;90m'
NC='\033[0m'

echo ""
echo -e "${CYAN}=== Inhouse League Registration ===${NC}"
echo ""

# --- Locate Steam -----------------------------------------------------------

STEAM_PATH=""
for candidate in \
    "$HOME/.local/share/Steam" \
    "$HOME/.steam/steam" \
    "$HOME/.steam/Steam" \
    "/usr/share/steam"; do
    if [ -f "$candidate/config/loginusers.vdf" ]; then
        STEAM_PATH="$candidate"
        break
    fi
done

if [ -z "$STEAM_PATH" ]; then
    echo -e "${RED}ERROR: Steam not found. Is Steam installed and have you logged in?${NC}"
    exit 1
fi

VDF="$STEAM_PATH/config/loginusers.vdf"

# --- Parse loginusers.vdf ---------------------------------------------------
# Extract 17-digit Steam IDs and their PersonaName values in order.

mapfile -t STEAM_IDS < <(grep -oP '"\K\d{17}(?=")' "$VDF")
mapfile -t PERSONA_NAMES < <(grep -oP '"PersonaName"\s+"\K[^"]+' "$VDF")

if [ ${#STEAM_IDS[@]} -eq 0 ]; then
    echo -e "${RED}ERROR: No Steam accounts found in loginusers.vdf.${NC}"
    exit 1
fi

# --- Pick account -----------------------------------------------------------

if [ ${#STEAM_IDS[@]} -eq 1 ]; then
    CHOSEN_ID="${STEAM_IDS[0]}"
    CHOSEN_NAME="${PERSONA_NAMES[0]:-Unknown}"
    echo "Found Steam account: $CHOSEN_NAME ($CHOSEN_ID)"
else
    echo "Multiple Steam accounts found:"
    for i in "${!STEAM_IDS[@]}"; do
        echo "  [$((i+1))] ${PERSONA_NAMES[$i]:-Unknown} (${STEAM_IDS[$i]})"
    done
    echo ""
    read -rp "Enter the number of your account: " PICK
    IDX=$((PICK - 1))
    if [ "$IDX" -lt 0 ] || [ "$IDX" -ge "${#STEAM_IDS[@]}" ]; then
        echo -e "${RED}Invalid selection.${NC}"
        exit 1
    fi
    CHOSEN_ID="${STEAM_IDS[$IDX]}"
    CHOSEN_NAME="${PERSONA_NAMES[$IDX]:-Unknown}"
fi

# --- Display name -----------------------------------------------------------

DISPLAY_NAME="$CHOSEN_NAME"
echo "Using Steam name: $DISPLAY_NAME"

# --- Register with backend --------------------------------------------------

echo ""
echo -e "${YELLOW}Registering...${NC}"

HTTP_RESPONSE=$(curl -s -w "\n%{http_code}" \
    -X POST "$API_BASE/api/register" \
    -H "Content-Type: application/json" \
    -d "{\"steam_id\":\"$CHOSEN_ID\",\"display_name\":\"$DISPLAY_NAME\"}")

HTTP_BODY=$(echo "$HTTP_RESPONSE" | head -n -1)
HTTP_STATUS=$(echo "$HTTP_RESPONSE" | tail -n 1)

ALREADY_REGISTERED=false
if [ "$HTTP_STATUS" -eq 409 ]; then
    echo -e "${YELLOW}You are already registered! No need to run this again.${NC}"
    ALREADY_REGISTERED=true
elif [ "$HTTP_STATUS" -ne 201 ]; then
    echo -e "${RED}ERROR: Registration failed (HTTP $HTTP_STATUS)${NC}"
    echo "$HTTP_BODY"
    exit 1
fi

if [ "$ALREADY_REGISTERED" = false ]; then
    TOKEN=$(echo "$HTTP_BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")

    if [ -z "$TOKEN" ]; then
        echo -e "${RED}ERROR: Could not read token from response.${NC}"
        exit 1
    fi

    # --- Write GSI config -------------------------------------------------------

    GSI_DIR="$STEAM_PATH/steamapps/common/dota 2 beta/game/dota/cfg/gamestate_integration"
    mkdir -p "$GSI_DIR"

    GSI_PATH="$GSI_DIR/gamestate_integration_inhouse.cfg"

    cat > "$GSI_PATH" <<EOF
"inhouse"
{
    "uri"        "$API_BASE/gsi"
    "timeout"    "5.0"
    "buffer"     "0.1"
    "throttle"   "1.0"
    "heartbeat"  "30.0"
    "auth"
    {
        "token"  "$TOKEN"
    }
    "data"
    {
        "map"    "1"
        "player" "1"
        "hero"   "1"
    }
}
EOF

    echo ""
    echo -e "${GREEN}All done! You are registered.${NC}"
    echo "GSI config written to: $GSI_PATH"
    echo "Launch Dota 2 and your stats will be tracked automatically."
fi

echo ""
echo -e "${GRAY}--------------------------------------------------------------${NC}"
echo -e "${CYAN}Add the league bot as a Steam friend so it can send you lobby invites.${NC}"
echo ""
echo -e "${CYAN}Bot Steam profile:${NC}"
echo "  $BOT_PROFILE"
echo -e "${CYAN}Click 'Add as Friend' and the bot will accept automatically.${NC}"
echo -e "${GRAY}--------------------------------------------------------------${NC}"
echo ""
read -rp "Open bot Steam profile in browser? (y/n) " OPEN_BROWSER
if [[ "$OPEN_BROWSER" == "y" || "$OPEN_BROWSER" == "Y" ]]; then
    xdg-open "$BOT_PROFILE" 2>/dev/null || true
fi
echo ""
echo -e "${GREEN}Done! You're all set.${NC}"
echo ""
