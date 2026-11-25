#!/usr/bin/env bash
#
# Build a Debian source package (with vendored Go modules)
# for container-explorer from a clean git archive.
#
set -euo pipefail

PKG_NAME="container-explorer"
VERSION=""
DISTRO="resolute"
PPA_REVISION="1ppa1"
GIT_REF="HEAD"
OUTPUT_DIR="../dpkg-build"
UNSIGNED=0
BUILD_BINARY=0
GPG_KEY=""

usage() {
  cat <<EOF
Usage: $(basename "$0") [options]

Build a vendored Debian source package for Launchpad GIFT PPA upload.

Options:
  --version VER         Upstream version (default: parsed from cmd/main.go at --ref)
  --distro SUITE        Target Ubuntu series (default: ${DISTRO})
  --ppa-revision REV    Debian/PPA revision suffix (default: ${PPA_REVISION})
  --ref GIT_REF         Git ref to archive (default: ${GIT_REF})
  --output-dir DIR      Output directory for .dsc/.changes/tarballs (default: ${OUTPUT_DIR})
  --key KEY_ID          GPG key ID passed to dpkg-buildpackage -k
  --unsigned            Do not GPG-sign .dsc/.changes (local testing only)
  --binary              Also run an offline binary build (-b) to test .deb creation
  -h, --help            Show this help message

Environment variables:
  DEBFULLNAME           Uploader name for debian/changelog trailer
  DEBEMAIL              Uploader email for debian/changelog trailer
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)      VERSION="$2"; shift 2 ;;
    --distro)       DISTRO="$2"; shift 2 ;;
    --ppa-revision) PPA_REVISION="$2"; shift 2 ;;
    --ref)          GIT_REF="$2"; shift 2 ;;
    --output-dir)   OUTPUT_DIR="$2"; shift 2 ;;
    --key)          GPG_KEY="$2"; shift 2 ;;
    --unsigned)     UNSIGNED=1; shift ;;
    --binary)       BUILD_BINARY=1; shift ;;
    -h|--help)      usage; exit 0 ;;
    *)              echo "Unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "${REPO_ROOT}"

for tool in git go tar xz dpkg-buildpackage; do
  if ! command -v "${tool}" >/dev/null 2>&1; then
    echo "Error: required tool '${tool}' not found in PATH" >&2
    exit 1
  fi
done

if [[ -z "${VERSION}" ]]; then
  VERSION="$(git show "${GIT_REF}:cmd/main.go" | sed -nE 's/^[[:space:]]*VERSION[[:space:]]*=[[:space:]]*"([^"]+)".*/\1/p' | head -n 1)"
  if [[ -z "${VERSION}" ]]; then
    echo "Error: could not extract VERSION from ${GIT_REF}:cmd/main.go; pass --version explicitly" >&2
    exit 1
  fi
fi

mkdir -p "${OUTPUT_DIR}"
OUTPUT_DIR="$(cd "${OUTPUT_DIR}" && pwd)"

SRC_DIR_NAME="${PKG_NAME}-${VERSION}"
SRC_DIR="${OUTPUT_DIR}/${SRC_DIR_NAME}"
ORIG_TAR="${OUTPUT_DIR}/${PKG_NAME}_${VERSION}.orig.tar.xz"
DEB_VERSION="${VERSION}-${PPA_REVISION}~${DISTRO}"

rm -rf "${SRC_DIR}" "${ORIG_TAR}"
mkdir -p "${SRC_DIR}"

echo "[+] Archiving ${GIT_REF} into ${SRC_DIR}"
git archive --format=tar "${GIT_REF}" | tar -xf - -C "${SRC_DIR}"

echo "[+] Vendoring Go module dependencies into ${SRC_DIR}/vendor"
(cd "${SRC_DIR}" && go mod vendor)

echo "[+] Creating ${ORIG_TAR}"
tar -C "${OUTPUT_DIR}" \
  --sort=name \
  --owner=0 --group=0 --numeric-owner \
  -cJf "${ORIG_TAR}" "${SRC_DIR_NAME}"

echo "[+] Copying config/dpkg/ to ${SRC_DIR}/debian/"
cp -a "${REPO_ROOT}/config/dpkg" "${SRC_DIR}/debian"
chmod 755 "${SRC_DIR}/debian/rules"

# Stamp debian/changelog if version, distro, or maintainer identity is customized.
CHANGELOG_FILE="${SRC_DIR}/debian/changelog"
MAINTAINER_NAME="${DEBFULLNAME:-container-explorer developers}"
MAINTAINER_EMAIL="${DEBEMAIL:-container-explorer-dev@googlegroups.com}"
CURRENT_HEADER="$(head -n 1 "${CHANGELOG_FILE}")"
EXPECTED_HEADER="${PKG_NAME} (${DEB_VERSION}) ${DISTRO}; urgency=low"
if [[ "${CURRENT_HEADER}" != "${EXPECTED_HEADER}" ]] || [[ -n "${DEBFULLNAME:-}" ]] || [[ -n "${DEBEMAIL:-}" ]]; then
  RFC2822_DATE="$(date -R)"
  TMP_CHANGELOG="$(mktemp)"
  {
    printf '%s\n\n  * GIFT PPA package for Ubuntu %s (%s).\n\n -- %s <%s>  %s\n' \
      "${EXPECTED_HEADER}" "${DISTRO}" "${VERSION}" "${MAINTAINER_NAME}" "${MAINTAINER_EMAIL}" "${RFC2822_DATE}"
    if [[ "${CURRENT_HEADER}" != "${EXPECTED_HEADER}" ]]; then
      printf '\n'
      cat "${CHANGELOG_FILE}"
    fi
  } > "${TMP_CHANGELOG}"
  mv "${TMP_CHANGELOG}" "${CHANGELOG_FILE}"
fi

DPKG_ARGS=(-S -sa)
if [[ "${UNSIGNED}" -eq 1 ]]; then
  DPKG_ARGS+=(-us -uc)
elif [[ -n "${GPG_KEY}" ]]; then
  DPKG_ARGS+=("-k${GPG_KEY}")
fi

echo "[+] Building Debian source package (${DEB_VERSION})"
(cd "${SRC_DIR}" && dpkg-buildpackage "${DPKG_ARGS[@]}")

CHANGES_FILE="${OUTPUT_DIR}/${PKG_NAME}_${DEB_VERSION}_source.changes"
echo ""
echo "[+] Source package built successfully:"
ls -lh "${OUTPUT_DIR}/${PKG_NAME}_${VERSION}.orig.tar.xz" \
       "${OUTPUT_DIR}/${PKG_NAME}_${DEB_VERSION}.dsc" \
       "${OUTPUT_DIR}/${PKG_NAME}_${DEB_VERSION}.debian.tar."* \
       "${CHANGES_FILE}"

if [[ "${BUILD_BINARY}" -eq 1 ]]; then
  echo ""
  echo "[+] Running offline binary package test build (-b -us -uc)"
  (cd "${SRC_DIR}" && GOPROXY=off GOTOOLCHAIN=local dpkg-buildpackage -b -us -uc)
  echo "[+] Binary package artifacts:"
  ls -lh "${OUTPUT_DIR}/${PKG_NAME}_${DEB_VERSION}_"*.deb
fi

echo ""
if [[ "${UNSIGNED}" -eq 1 ]]; then
  echo "[!] Built unsigned (--unsigned). Re-run without --unsigned before uploading to Launchpad."
else
  echo "[+] Next step (verify in clean Ubuntu ${DISTRO} chroot first, then upload to testing):"
  echo "    dput ppa:gift/testing ${CHANGES_FILE}"
fi
