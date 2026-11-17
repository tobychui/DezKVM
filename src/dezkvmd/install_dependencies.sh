#!/bin/bash
# Check for, and install, the packages dezkvmd needs on Debian/Ubuntu/Armbian.
#
# Usage:
#   ./install_dependencies.sh            Check and install missing packages
#   ./install_dependencies.sh --check    Only report what is missing (no changes)
#   ./install_dependencies.sh --cross    Also install the cross compilers used by "make release"

set -u

PACKAGES=(
    build-essential
    libv4l-dev
    v4l-utils
    alsa-utils
    exfatprogs
    ntfs-3g
    ffmpeg
    dosfstools # mkfs.vfat, used by the USB drive format tool
)

# Only needed on an amd64 build machine for "make release"
CROSS_PACKAGES=(
    gcc-aarch64-linux-gnu
    gcc-arm-linux-gnueabihf
    gcc-i686-linux-gnu
    libc6-dev-arm64-cross
    libc6-dev-armhf-cross
    libc6-dev-i386-cross
    linux-libc-dev-arm64-cross
    linux-libc-dev-armhf-cross
    linux-libc-dev-i386-cross
)

CHECK_ONLY=0
WITH_CROSS=0
for arg in "$@"; do
    case "$arg" in
        --check) CHECK_ONLY=1 ;;
        --cross) WITH_CROSS=1 ;;
        -h|--help) sed -n '2,7p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
        *) echo "Unknown option: $arg" >&2; exit 1 ;;
    esac
done

if ! command -v apt-get >/dev/null 2>&1 || ! command -v dpkg >/dev/null 2>&1; then
    echo "This script supports apt-based systems only (Debian/Ubuntu/Armbian)." >&2
    exit 1
fi

[ "$WITH_CROSS" -eq 1 ] && PACKAGES+=("${CROSS_PACKAGES[@]}")

is_installed() {
    dpkg-query -W -f='${Status}' "$1" 2>/dev/null | grep -q "install ok installed"
}

MISSING=()
for pkg in "${PACKAGES[@]}"; do
    if is_installed "$pkg"; then
        echo "[ok]      $pkg"
    else
        echo "[missing] $pkg"
        MISSING+=("$pkg")
    fi
done

# Go is not installed through apt (Debian's version is usually too old for go.mod)
if command -v go >/dev/null 2>&1; then
    echo "[ok]      go ($(go version | awk '{print $3}'))"
else
    echo "[warn]    go not found in PATH. Needed to build dezkvmd, see https://go.dev/dl/"
fi

if [ "${#MISSING[@]}" -eq 0 ]; then
    echo "All dependencies are installed."
    exit 0
fi

if [ "$CHECK_ONLY" -eq 1 ]; then
    echo "Missing packages: ${MISSING[*]}"
    exit 1
fi

SUDO=""
if [ "$(id -u)" -ne 0 ]; then
    if command -v sudo >/dev/null 2>&1; then
        SUDO="sudo"
    else
        echo "Root privileges are required to install packages. Re-run as root." >&2
        exit 1
    fi
fi

echo "Installing: ${MISSING[*]}"
$SUDO apt-get update || exit 1
$SUDO env DEBIAN_FRONTEND=noninteractive apt-get install -y "${MISSING[@]}" || exit 1
echo "Dependencies installed."
