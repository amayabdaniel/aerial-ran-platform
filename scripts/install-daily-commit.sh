#!/bin/bash
# install-daily-commit.sh — install a launchd job that runs scripts/daily-commit.sh
# every day at 21:00 local time. Idempotent: re-running reinstalls cleanly.
#
#   ./scripts/install-daily-commit.sh          # install / reinstall
#   ./scripts/install-daily-commit.sh remove   # uninstall
#
# launchd (not cron) is used because it survives reboots, runs at LOCAL time, and
# if the Mac is asleep at 21:00 it runs the job at next wake (cron would skip it).
set -euo pipefail

LABEL="com.amayabdaniel.aerial-daily-commit"
REPO="/Users/amayabdaniel/projectz/aerial-ran-platform"
PLIST="$HOME/Library/LaunchAgents/${LABEL}.plist"
LOG="$HOME/Library/Logs/aerial-daily-commit.log"
HOUR=21
MINUTE=0
UID_NUM="$(id -u)"

uninstall() {
  launchctl bootout "gui/${UID_NUM}/${LABEL}" 2>/dev/null || launchctl unload "$PLIST" 2>/dev/null || true
  rm -f "$PLIST"
  echo "removed $LABEL"
}

if [ "${1:-}" = "remove" ]; then
  uninstall
  exit 0
fi

mkdir -p "$HOME/Library/LaunchAgents" "$HOME/Library/Logs"

cat > "$PLIST" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>${LABEL}</string>
    <key>ProgramArguments</key>
    <array>
        <string>/bin/bash</string>
        <string>${REPO}/scripts/daily-commit.sh</string>
    </array>
    <key>StartCalendarInterval</key>
    <dict>
        <key>Hour</key>
        <integer>${HOUR}</integer>
        <key>Minute</key>
        <integer>${MINUTE}</integer>
    </dict>
    <key>RunAtLoad</key>
    <false/>
    <key>StandardOutPath</key>
    <string>${LOG}</string>
    <key>StandardErrorPath</key>
    <string>${LOG}</string>
    <key>ProcessType</key>
    <string>Background</string>
</dict>
</plist>
PLIST

# Reload cleanly.
launchctl bootout "gui/${UID_NUM}/${LABEL}" 2>/dev/null || true
launchctl bootstrap "gui/${UID_NUM}" "$PLIST"
launchctl enable "gui/${UID_NUM}/${LABEL}" 2>/dev/null || true

echo "installed $LABEL"
echo "  runs daily at $(printf '%02d:%02d' "$HOUR" "$MINUTE") local"
echo "  script: ${REPO}/scripts/daily-commit.sh"
echo "  log:    ${LOG}"
echo
echo "verify:  launchctl print gui/${UID_NUM}/${LABEL} | grep -E 'state|runs'"
echo "run now: launchctl kickstart -k gui/${UID_NUM}/${LABEL}"
echo "remove:  ./scripts/install-daily-commit.sh remove"
