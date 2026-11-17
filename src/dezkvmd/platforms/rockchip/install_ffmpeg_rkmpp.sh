#!/usr/bin/env bash
#
# install_ffmpeg_rkmpp.sh
#
# Builds and installs ffmpeg-rockchip (https://github.com/nyanmisaka/ffmpeg-rockchip),
# an ffmpeg fork that adds the Rockchip MPP hardware encoders (h264_rkmpp,
# hevc_rkmpp). dezkvmd picks it up automatically and shows the
# "Rockchip MPP hardware encoder" option in Settings > WebRTC.
#
# It is installed to its own prefix (default /opt/ffmpeg-rkmpp) and does NOT
# replace the system ffmpeg, so apt upgrades and other programs are not
# affected. dezkvmd looks for /opt/ffmpeg-rkmpp/bin/ffmpeg first.
#
# Requirements:
#   - A Rockchip board running the vendor (BSP) kernel, which provides
#     /dev/mpp_service (RK3528, RK3566/RK3568, RK3576, RK3588...)
#   - Debian / Ubuntu (apt) for the build dependencies
#
# Usage:
#   sudo ./install_ffmpeg_rkmpp.sh [options]
#
# Options:
#   --prefix DIR     Install prefix (default /opt/ffmpeg-rkmpp). dezkvmd only
#                    finds the default prefix automatically.
#   --jobs N         Parallel build jobs (default: based on CPU count and free RAM)
#   --full           Build every ffmpeg component instead of the minimal set
#                    dezkvmd needs (much slower, ~1 hour on a Cortex-A53 board)
#   --with-rga       Also build librga and enable the RGA filters (scale_rkrga...)
#   --build-mpp      Always build Rockchip MPP from source instead of using the
#                    system librockchip-mpp-dev package
#   --ref REF        ffmpeg-rockchip git branch or tag (default master)
#   --src DIR        Source/build directory (default /usr/local/src/ffmpeg-rkmpp)
#   --skip-deps      Do not install build dependencies with apt
#   --keep-src       Keep the source tree after a successful install
#   -h, --help       Show this help
#
set -euo pipefail

PREFIX=/opt/ffmpeg-rkmpp
SRC_DIR=/usr/local/src/ffmpeg-rkmpp
FFMPEG_REPO=https://github.com/nyanmisaka/ffmpeg-rockchip.git
FFMPEG_REF=master
# Rockchip MPP / RGA mirrors maintained by the ffmpeg-rockchip author
RK_MIRRORS_REPO=https://github.com/nyanmisaka/rk-mirrors.git
MPP_REF=jellyfin-mpp
RGA_REF=jellyfin-rga
JOBS=""
FULL=0
WITH_RGA=0
BUILD_MPP=0
SKIP_DEPS=0
KEEP_SRC=0

log()  { printf '\033[1;32m[rkmpp]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[rkmpp] warning:\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31m[rkmpp] error:\033[0m %s\n' "$*" >&2; exit 1; }
usage() { sed -n '2,/^set -euo/p' "$0" | sed -e '/^set -euo/d' -e 's/^# \{0,1\}//'; exit 0; }

while [[ $# -gt 0 ]]; do
    case "$1" in
        --prefix)    PREFIX="${2:?}"; shift 2 ;;
        --jobs)      JOBS="${2:?}"; shift 2 ;;
        --full)      FULL=1; shift ;;
        --with-rga)  WITH_RGA=1; shift ;;
        --build-mpp) BUILD_MPP=1; shift ;;
        --ref)       FFMPEG_REF="${2:?}"; shift 2 ;;
        --src)       SRC_DIR="${2:?}"; shift 2 ;;
        --skip-deps) SKIP_DEPS=1; shift ;;
        --keep-src)  KEEP_SRC=1; shift ;;
        -h|--help)   usage ;;
        *)           die "unknown option: $1 (see --help)" ;;
    esac
done

# ---------------------------------------------------------------- checks

[[ $EUID -eq 0 ]] || die "run this script as root: sudo $0 $*"

case "$(uname -m)" in
    aarch64|armv7l|armv8l) ;;
    *) die "this script must run on the Rockchip board itself (found $(uname -m))" ;;
esac

# Note: never pipe into "grep -q" in this script. grep -q exits on the first
# match, the writer gets SIGPIPE and pipefail turns the match into a failure.
COMPATIBLE=$(tr '\0' '\n' < /proc/device-tree/compatible 2>/dev/null || true)
if ! grep -q '^rockchip,' <<<"$COMPATIBLE"; then
    die "this does not look like a Rockchip board (no rockchip,* in /proc/device-tree/compatible)"
fi
SOC=$(sed -n 's/^rockchip,//p' <<<"$COMPATIBLE" | head -n1)
log "Rockchip SoC: ${SOC}"

if [[ ! -e /dev/mpp_service ]]; then
    warn "/dev/mpp_service not found. The hardware encoder needs a Rockchip vendor (BSP) kernel."
    warn "ffmpeg will still be built, but h264_rkmpp will not work on this kernel."
fi

if [[ -z "$JOBS" ]]; then
    cpus=$(nproc)
    # ~350 MB per compiler job keeps 1 GB boards out of the OOM killer
    mem_jobs=$(( $(awk '/MemAvailable/ {print int($2/1024)}' /proc/meminfo) / 350 ))
    (( mem_jobs < 1 )) && mem_jobs=1
    JOBS=$(( cpus < mem_jobs ? cpus : mem_jobs ))
fi
log "Building with ${JOBS} parallel job(s), installing to ${PREFIX}"

# ---------------------------------------------------------- dependencies

if [[ $SKIP_DEPS -eq 0 ]]; then
    command -v apt-get >/dev/null || die "apt-get not found, install the build dependencies yourself and pass --skip-deps"
    log "Installing build dependencies..."
    pkgs=(git build-essential pkg-config libdrm-dev)
    [[ $WITH_RGA -eq 1 ]] && pkgs+=(meson ninja-build)
    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "${pkgs[@]}"
    # Prefer the distro MPP (e.g. FriendlyElec / Armbian ship librockchip-mpp-dev)
    if [[ $BUILD_MPP -eq 0 ]] && apt-cache show librockchip-mpp-dev >/dev/null 2>&1; then
        DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends librockchip-mpp-dev || true
    fi
fi

mkdir -p "$SRC_DIR"
export PKG_CONFIG_PATH="${PREFIX}/lib/pkgconfig${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}"

# ffmpeg-rockchip needs MPP >= 1.3.9 exporting mpp_buffer_sync_begin_f
system_mpp_ok() {
    pkg-config --atleast-version=1.3.9 rockchip_mpp 2>/dev/null || return 1
    local libdir syms
    libdir=$(pkg-config --variable=libdir rockchip_mpp)
    syms=$(nm -D "${libdir}/librockchip_mpp.so" 2>/dev/null) || return 1
    grep -q ' mpp_buffer_sync_begin_f$' <<<"$syms"
}

# Shallow clone, or update an existing clone, of repo@ref into dir
fetch() {
    local repo=$1 ref=$2 dir=$3
    if [[ -d "$dir/.git" ]]; then
        log "Updating $(basename "$dir") (${ref})..."
        git -C "$dir" fetch --depth 1 origin "$ref"
        git -C "$dir" checkout -q --force FETCH_HEAD
    else
        log "Cloning $(basename "$dir") (${ref})..."
        rm -rf "$dir"
        git clone --depth 1 --branch "$ref" "$repo" "$dir"
    fi
}

# ------------------------------------------------------------------ MPP

if [[ $BUILD_MPP -eq 0 ]] && system_mpp_ok; then
    log "Using system Rockchip MPP $(pkg-config --modversion rockchip_mpp)"
else
    log "Building Rockchip MPP from source..."
    if [[ $SKIP_DEPS -eq 0 ]]; then
        DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends cmake
    fi
    command -v cmake >/dev/null || die "cmake is required to build MPP"
    fetch "$RK_MIRRORS_REPO" "$MPP_REF" "$SRC_DIR/mpp"
    cmake -S "$SRC_DIR/mpp" -B "$SRC_DIR/mpp/build" \
        -DCMAKE_INSTALL_PREFIX="$PREFIX" \
        -DCMAKE_INSTALL_LIBDIR=lib \
        -DCMAKE_BUILD_TYPE=Release \
        -DBUILD_SHARED_LIBS=ON \
        -DBUILD_TEST=OFF
    cmake --build "$SRC_DIR/mpp/build" -j "$JOBS"
    cmake --install "$SRC_DIR/mpp/build"
    system_mpp_ok || die "MPP was built but pkg-config still cannot find a usable rockchip_mpp"
fi

# ------------------------------------------------------------------ RGA

if [[ $WITH_RGA -eq 1 ]]; then
    log "Building librga..."
    fetch "$RK_MIRRORS_REPO" "$RGA_REF" "$SRC_DIR/rga"
    rm -rf "$SRC_DIR/rga/build"
    meson setup "$SRC_DIR/rga/build" "$SRC_DIR/rga" \
        --prefix="$PREFIX" \
        --libdir=lib \
        --buildtype=release \
        --default-library=shared \
        -Dcpp_args=-fpermissive \
        -Dlibdrm=false \
        -Dlibrga_demo=false
    ninja -C "$SRC_DIR/rga/build" -j "$JOBS" install
fi

# --------------------------------------------------------------- ffmpeg

fetch "$FFMPEG_REPO" "$FFMPEG_REF" "$SRC_DIR/ffmpeg"

conf=(
    --prefix="$PREFIX"
    --enable-version3          # MPP / RGA are Apache 2.0 licensed
    --enable-libdrm
    --enable-rkmpp
    --disable-doc
    --disable-ffplay
    --extra-ldflags="-Wl,-rpath,${PREFIX}/lib"
)
[[ $WITH_RGA -eq 1 ]] && conf+=(--enable-rkrga)

if [[ $FULL -eq 0 ]]; then
    # Only what dezkvmd's WebRTC pipeline needs (MJPEG in -> H.264 out) plus
    # the Rockchip codecs and common containers for manual testing.
    # Filters, parsers and bitstream filters are small and kept complete.
    conf+=(
        --disable-encoders --disable-decoders
        --disable-muxers --disable-demuxers
        --disable-protocols --disable-devices
        --enable-decoder=mjpeg,rawvideo,wrapped_avframe,h264_rkmpp,hevc_rkmpp,mjpeg_rkmpp
        --enable-encoder=h264_rkmpp,hevc_rkmpp,mjpeg_rkmpp,rawvideo,wrapped_avframe
        --enable-demuxer=mjpeg,image2pipe,h264,hevc,rawvideo,mov,matroska
        --enable-muxer=h264,hevc,mjpeg,rawvideo,null,mp4,matroska
        --enable-protocol=file,pipe
        --enable-indev=lavfi
    )
fi

log "Configuring ffmpeg-rockchip..."
cd "$SRC_DIR/ffmpeg"
./configure "${conf[@]}" || die "configure failed, see $SRC_DIR/ffmpeg/ffbuild/config.log"

log "Building ffmpeg-rockchip (this takes a while)..."
make -j "$JOBS"
make install

FFMPEG_BIN="${PREFIX}/bin/ffmpeg"
[[ -x "$FFMPEG_BIN" ]] || die "build finished but ${FFMPEG_BIN} is missing"

# ------------------------------------------------------------ self test

log "Checking the installed ffmpeg..."
ENCODERS=$("$FFMPEG_BIN" -hide_banner -encoders 2>/dev/null || true)
grep -q ' h264_rkmpp ' <<<"$ENCODERS" \
    || die "${FFMPEG_BIN} has no h264_rkmpp encoder"

if [[ -e /dev/mpp_service ]]; then
    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' EXIT
    # Same encoder settings dezkvmd uses (see mod/videnc/rkmpp.go)
    if "$FFMPEG_BIN" -hide_banner -loglevel error -y \
        -f lavfi -i nullsrc=s=1280x720:r=30 -frames:v 60 \
        -vf format=nv12 -c:v h264_rkmpp -profile:v baseline -rc_mode CBR \
        -bf 0 -g 30 -b:v 4000k -f h264 "$tmp/test.h264" \
        && [[ -s "$tmp/test.h264" ]]; then
        log "Hardware encode test passed ($(stat -c %s "$tmp/test.h264") bytes of H.264)"
    else
        warn "h264_rkmpp test encode failed. Check that /dev/mpp_service is accessible (dezkvmd runs as root)."
    fi
else
    warn "Skipping the hardware encode test, /dev/mpp_service is missing"
fi

if [[ $KEEP_SRC -eq 0 ]]; then
    log "Removing build tree ${SRC_DIR} (use --keep-src to keep it)"
    # Only remove what this script cloned, --src may point at a shared folder
    rm -rf "$SRC_DIR/ffmpeg" "$SRC_DIR/mpp" "$SRC_DIR/rga"
    rmdir "$SRC_DIR" 2>/dev/null || true
fi

log "Done: ${FFMPEG_BIN}"
[[ "$PREFIX" != /opt/ffmpeg-rkmpp ]] && \
    warn "dezkvmd only looks in /opt/ffmpeg-rkmpp, put ${PREFIX}/bin first in its PATH to use this build"
log "Restart dezkvmd and select \"Rockchip MPP hardware encoder\" (or Auto) in Settings > Streaming Mode > WebRTC."
