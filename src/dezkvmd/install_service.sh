#!/usr/bin/env bash
#
# install_service.sh
#
# Installs dezkvmd as a systemd service that runs from the folder this script
# is in, so it works wherever the repository was cloned. Re-run it after
# moving the folder to point the service at the new location.
#
# It can also set up hardware video encoding for the WebRTC streaming mode
# when the board supports it (asks first):
#   - Rockchip SoCs: builds ffmpeg-rockchip (platforms/rockchip/install_ffmpeg_rkmpp.sh)
#   - Intel CPUs:    installs the VA-API drivers for Quick Sync
#
# Usage:
#   sudo ./install_service.sh [options]
#
# Options:
#   -y, --yes          Answer yes to every question (unattended install)
#   --no-encoder       Skip the hardware encoder setup
#   --no-start         Install and enable the service but do not start it now
#   --dry-run          Show what would be done without changing anything
#   -h, --help         Show this help
#
set -euo pipefail

SERVICE_NAME=dezkvmd
UNIT_FILE=/etc/systemd/system/${SERVICE_NAME}.service
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
RKMPP_SCRIPT="${SCRIPT_DIR}/platforms/rockchip/install_ffmpeg_rkmpp.sh"
RKMPP_FFMPEG=/opt/ffmpeg-rkmpp/bin/ffmpeg

ASSUME_YES=0
SKIP_ENCODER=0
NO_START=0
DRY_RUN=0

log()  { printf '\033[1;32m[dezkvmd]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[dezkvmd] warning:\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31m[dezkvmd] error:\033[0m %s\n' "$*" >&2; exit 1; }
usage() { sed -n '2,/^set -euo/p' "$0" | sed -e '/^set -euo/d' -e 's/^# \{0,1\}//'; exit 0; }

# run prints the command in dry-run mode instead of executing it
run() {
    if [[ $DRY_RUN -eq 1 ]]; then
        printf '  would run: %s\n' "$*"
    else
        "$@"
    fi
}

# ask "question" default(y|n) -> returns 0 for yes. Reads from the terminal so
# it also works when the script is piped; without a terminal the default wins.
ask() {
    local question=$1 default=$2 reply hint
    if [[ $ASSUME_YES -eq 1 ]]; then
        return 0
    fi
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
        -y|--yes)     ASSUME_YES=1; shift ;;
        --no-encoder) SKIP_ENCODER=1; shift ;;
        --no-start)   NO_START=1; shift ;;
        --dry-run)    DRY_RUN=1; shift ;;
        -h|--help)    usage ;;
        *)            die "unknown option: $1 (see --help)" ;;
    esac
done

# ---------------------------------------------------------------- checks

if [[ $DRY_RUN -eq 0 && $EUID -ne 0 ]]; then
    die "run this script as root: sudo $0 $*"
fi
command -v systemctl >/dev/null || die "systemctl not found, this system does not use systemd"
[[ $SCRIPT_DIR != *$'\n'* ]] || die "the install path contains a newline, move the folder first"
[[ -d "$SCRIPT_DIR/www" ]] || die "$SCRIPT_DIR does not look like the dezkvmd folder (no www/ next to this script)"

log "Installing the ${SERVICE_NAME} service from ${SCRIPT_DIR}"

# ---------------------------------------------------------------- binary

# dezkvmd reads ./config and ./www relative to its working directory, so the
# binary must live in (and run from) this folder.
BINARY="${SCRIPT_DIR}/dezkvmd"
if [[ -x "$BINARY" && -x "${SCRIPT_DIR}/build/dezkvmd" && "${SCRIPT_DIR}/build/dezkvmd" -nt "$BINARY" ]]; then
    log "build/dezkvmd is newer than ./dezkvmd, using the new build"
    run install -m 0755 "${SCRIPT_DIR}/build/dezkvmd" "$BINARY"
fi
if [[ ! -x "$BINARY" ]]; then
    if [[ -x "${SCRIPT_DIR}/build/dezkvmd" ]]; then
        log "Using the binary from build/ (copied to ${BINARY})"
        run install -m 0755 "${SCRIPT_DIR}/build/dezkvmd" "$BINARY"
    elif command -v go >/dev/null && command -v make >/dev/null; then
        if ask "No dezkvmd binary found. Build it now with make (takes a few minutes)?" y; then
            # Build as the folder's owner so build files are not owned by root
            owner=$(stat -c %U "$SCRIPT_DIR")
            if [[ $owner != root ]] && command -v sudo >/dev/null; then
                run sudo -u "$owner" -H make -C "$SCRIPT_DIR" build
            else
                run make -C "$SCRIPT_DIR" build
            fi
            run install -m 0755 "${SCRIPT_DIR}/build/dezkvmd" "$BINARY"
        else
            die "no dezkvmd binary, build it with \"make\" first"
        fi
    else
        die "no dezkvmd binary in ${SCRIPT_DIR}, and Go is not installed to build one (see install_dependencies.sh)"
    fi
fi

# ------------------------------------------------------- hardware encoder

# detect_platform prints rockchip, intel or other
detect_platform() {
    if tr '\0' '\n' </proc/device-tree/compatible 2>/dev/null | grep -q '^rockchip,'; then
        echo rockchip
    elif grep -q 'GenuineIntel' /proc/cpuinfo 2>/dev/null; then
        echo intel
    else
        echo other
    fi
}

ffmpeg_has_encoder() { # bin encoder
    local list
    list=$("$1" -hide_banner -encoders 2>/dev/null || true)
    grep -q " $2 " <<<"$list"
}

setup_rockchip_encoder() {
    if [[ -x $RKMPP_FFMPEG ]] && ffmpeg_has_encoder "$RKMPP_FFMPEG" h264_rkmpp; then
        log "Rockchip hardware encoder already installed (${RKMPP_FFMPEG})"
        return
    fi
    if [[ ! -e /dev/mpp_service ]]; then
        warn "This Rockchip board has no /dev/mpp_service: the hardware encoder needs the board vendor's kernel. Skipping."
        return
    fi
    [[ -f $RKMPP_SCRIPT ]] || { warn "missing ${RKMPP_SCRIPT}, skipping"; return; }
    log "Rockchip SoC detected. WebRTC streaming can use the hardware H.264 encoder"
    log "(about 2 CPU cores less than software encoding at 1080p). This builds"
    log "ffmpeg-rockchip into /opt/ffmpeg-rkmpp (system ffmpeg is untouched), ~10-30 min."
    if ask "Install hardware encoder support now?" y; then
        if [[ $ASSUME_YES -eq 1 ]]; then
            run bash "$RKMPP_SCRIPT"
        else
            run bash "$RKMPP_SCRIPT" </dev/tty
        fi
    else
        log "Skipped. You can install it later with: sudo ${RKMPP_SCRIPT}"
    fi
}

setup_intel_encoder() {
    if ! command -v apt-get >/dev/null; then
        warn "Intel CPU detected but apt-get is missing: install your distribution's VA-API driver (intel-media-va-driver or i965-va-driver) for hardware encoding"
        return
    fi
    local pkgs=(ffmpeg vainfo intel-media-va-driver)
    # Pre-Broadwell iGPUs (e.g. Celeron 1037U, Atom Z8350) need the older i965 driver
    if apt-cache show i965-va-driver >/dev/null 2>&1; then
        pkgs+=(i965-va-driver)
    fi
    # intel-media-va-driver-non-free has the full encoder on Debian
    if apt-cache show intel-media-va-driver-non-free >/dev/null 2>&1; then
        pkgs=("${pkgs[@]/intel-media-va-driver/intel-media-va-driver-non-free}")
    fi
    local missing=()
    for p in "${pkgs[@]}"; do
        dpkg -s "$p" >/dev/null 2>&1 || missing+=("$p")
    done
    if [[ ${#missing[@]} -eq 0 ]]; then
        log "Intel VA-API drivers already installed"
        return
    fi
    log "Intel CPU detected. WebRTC streaming can use the Quick Sync hardware encoder through VA-API."
    if ask "Install the VA-API drivers (${missing[*]})?" y; then
        run apt-get update
        run env DEBIAN_FRONTEND=noninteractive apt-get install -y "${missing[@]}"
    else
        log "Skipped. dezkvmd falls back to software encoding in WebRTC mode."
    fi
}

if [[ $SKIP_ENCODER -eq 0 ]]; then
    case "$(detect_platform)" in
        rockchip) setup_rockchip_encoder ;;
        intel)    setup_intel_encoder ;;
        *)        log "No supported hardware encoder for this board; WebRTC mode uses software encoding (MJPEG mode is unaffected)." ;;
    esac
fi

# ---------------------------------------------------------------- service

# A dezkvmd started by hand (e.g. ./start.sh) holds the port and devices
if [[ $DRY_RUN -eq 0 ]]; then
    manual_pids=$(pgrep -x dezkvmd || true)
    service_pid=$(systemctl show -p MainPID --value "$SERVICE_NAME" 2>/dev/null || echo 0)
    for pid in $manual_pids; do
        if [[ $pid != "$service_pid" ]]; then
            warn "dezkvmd is already running outside systemd (PID $pid). Stop it before the service starts."
        fi
    done
fi

# systemd needs paths with spaces quoted in ExecStart
unit_content="[Unit]
Description=DezKVM daemon (dezkvmd)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
# dezkvmd reads ./config and ./www relative to this folder
WorkingDirectory=${SCRIPT_DIR}
ExecStart=\"${BINARY}\" -mode=ipkvm
Restart=on-failure
RestartSec=3
# Needs root for mounting the USB drive and accessing the capture devices
User=root
# Give WebRTC encoders and USB devices time to close
TimeoutStopSec=15

[Install]
WantedBy=multi-user.target
"

if [[ $DRY_RUN -eq 1 ]]; then
    log "Would write ${UNIT_FILE}:"
    printf '%s' "$unit_content" | sed 's/^/    /'
else
    printf '%s' "$unit_content" > "$UNIT_FILE"
    chmod 0644 "$UNIT_FILE"
    log "Wrote ${UNIT_FILE}"
fi

run systemctl daemon-reload
run systemctl enable "$SERVICE_NAME"
if [[ $NO_START -eq 1 ]]; then
    log "Service installed and enabled, start it with: sudo systemctl start ${SERVICE_NAME}"
else
    # restart picks up a new binary or path when the service already ran
    run systemctl restart "$SERVICE_NAME"
    if [[ $DRY_RUN -eq 0 ]]; then
        sleep 2
        if systemctl is-active --quiet "$SERVICE_NAME"; then
            port=$(grep -o '"ListeningAddress"[^,}]*' "${SCRIPT_DIR}/config/usbkvm.json" 2>/dev/null | grep -o ':[0-9]\+' | tail -n1)
            log "dezkvmd is running. Open https://$(hostname -I 2>/dev/null | awk '{print $1}')${port:-:9000}"
        else
            warn "the service did not start, check: journalctl -u ${SERVICE_NAME} -n 50"
            exit 1
        fi
    fi
fi
log "Logs: journalctl -u ${SERVICE_NAME} -f    Remove: sudo ${SCRIPT_DIR}/uninstall_service.sh"
