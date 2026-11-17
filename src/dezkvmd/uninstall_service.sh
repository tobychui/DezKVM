#!/usr/bin/env bash
#
# uninstall_service.sh
#
# Stops and removes the dezkvmd systemd service installed by
# install_service.sh. The dezkvmd folder, its config (users, preferences,
# certificates) and the binary are kept.
#
# Usage:
#   sudo ./uninstall_service.sh [options]
#
# Options:
#   -y, --yes          Answer yes to every question
#   --purge-ffmpeg     Also remove the Rockchip ffmpeg build in /opt/ffmpeg-rkmpp
#   --dry-run          Show what would be done without changing anything
#   -h, --help         Show this help
#
set -euo pipefail

SERVICE_NAME=dezkvmd
UNIT_FILE=/etc/systemd/system/${SERVICE_NAME}.service
RKMPP_PREFIX=/opt/ffmpeg-rkmpp

ASSUME_YES=0
PURGE_FFMPEG=0
DRY_RUN=0

log()  { printf '\033[1;32m[dezkvmd]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[dezkvmd] warning:\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31m[dezkvmd] error:\033[0m %s\n' "$*" >&2; exit 1; }
usage() { sed -n '2,/^set -euo/p' "$0" | sed -e '/^set -euo/d' -e 's/^# \{0,1\}//'; exit 0; }
run() { if [[ $DRY_RUN -eq 1 ]]; then printf '  would run: %s\n' "$*"; else "$@"; fi; }
ask() {
    local question=$1 default=$2 reply hint
    [[ $ASSUME_YES -eq 1 ]] && return 0
    [[ $default == y ]] && hint='[Y/n]' || hint='[y/N]'
    if [[ -r /dev/tty ]] && { true </dev/tty; } 2>/dev/null; then
        read -r -p "$question $hint " reply </dev/tty || reply=''
    else
        reply=''
    fi
    reply=${reply:-$default}
    [[ $reply =~ ^[Yy] ]]
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        -y|--yes)       ASSUME_YES=1; shift ;;
        --purge-ffmpeg) PURGE_FFMPEG=1; shift ;;
        --dry-run)      DRY_RUN=1; shift ;;
        -h|--help)      usage ;;
        *)              die "unknown option: $1 (see --help)" ;;
    esac
done

if [[ $DRY_RUN -eq 0 && $EUID -ne 0 ]]; then
    die "run this script as root: sudo $0 $*"
fi
command -v systemctl >/dev/null || die "systemctl not found"

if [[ -f $UNIT_FILE ]] || systemctl list-unit-files "${SERVICE_NAME}.service" 2>/dev/null | grep -q "^${SERVICE_NAME}.service"; then
    installed_from=$(sed -n 's/^WorkingDirectory=//p' "$UNIT_FILE" 2>/dev/null || true)
    log "Removing the ${SERVICE_NAME} service${installed_from:+ (installed from ${installed_from})}"
    run systemctl stop "$SERVICE_NAME" || true
    run systemctl disable "$SERVICE_NAME" || true
    run rm -f "$UNIT_FILE"
    run systemctl daemon-reload
    run systemctl reset-failed "$SERVICE_NAME" 2>/dev/null || true
    log "Service removed. The dezkvmd folder and its config were kept; run it by hand with ./start.sh"
else
    log "The ${SERVICE_NAME} service is not installed"
fi

# The hardware encoder ffmpeg is shared by every checkout, so only remove it when asked
if [[ -d $RKMPP_PREFIX ]]; then
    if [[ $PURGE_FFMPEG -eq 1 ]] || { [[ $ASSUME_YES -eq 0 ]] && ask "Also remove the Rockchip hardware encoder ffmpeg in ${RKMPP_PREFIX}?" n; }; then
        run rm -rf -- "$RKMPP_PREFIX"
        log "Removed ${RKMPP_PREFIX}"
    fi
fi
