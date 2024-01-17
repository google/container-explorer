#!/bin/sh
#
# ContainerExplorer installation script
#
set -e
SCRIPTNAME=$(basename "$0")

CE_VER=0.1.0
CE_PKG=container-explorer.tar.gz

CE_DIR=/opt/container-explorer
CE_BIN=${CE_DIR}/bin/ce
CE_SUPPORT=${CE_DIR}/etc/supportcontainer.yaml

TMP_DIR=$(mktemp -d -t ce-XXXXXXXXX)
CE_TMP_DIR=${TMP_DIR}/container-explorer

download_release() {
  echo "[+] Downloading container-explorer ${CE_PKG}"
  wget -P "${TMP_DIR}" https://github.com/google/container-explorer/releases/download/"${CE_VER}"/"${CE_PKG}" > /dev/null 2>&1
  tar -zxf "${TMP_DIR}"/container-explorer.tar.gz -C "${TMP_DIR}" > /dev/null 2>&1
}


install_release() {
  echo "[+] Installing contianer-explorer"
  if [ ! -d "${CE_DIR}" ]; then
    mkdir -p "${CE_DIR}"/bin
    mkdir -p "${CE_DIR}"/etc
  fi

  if [ -f "${CE_BIN}" ]; then
    rm -f "${CE_BIN}"
  fi

  if [ -f "${CE_SUPPORT}" ]; then
    rm -f "${CE_SUPPORT}"
  fi

  # Copy new binary and support file
  cp -f "${CE_TMP_DIR}"/ce "${CE_BIN}"
  cp -f "${CE_TMP_DIR}"/supportcontainer.yaml "${CE_SUPPORT}"
}

uninstall_release() {
  echo "[+] Uninstalling container-explorer"
  if [ -d "${CE_DIR}" ]; then
    rm -rf "${CE_DIR}"
  fi
}


cleanup() {
  echo "[+] Cleaning up..."
  rm -rf "${TMP_DIR}" > /dev/null 2>&1
}

check_root() {
  if [ "$(id -u)" -ne 0 ]; then
    echo "This script must be run as root. Use sudo ${SCRIPTNAME}"
    exit 1
  fi
}

case "$1" in
  install|upgrade)
    check_root
    download_release
    install_release
    cleanup
    echo "contianer-explorer installed at ${CE_DIR}"
    echo "For details use ${CE_BIN} -h"
    ;;
  remove|uninstall)
    check_root
    uninstall_release
    ;;
  *)
    echo "USAGE: ${SCRIPTNAME} {install|upgrade|remove}" >&2
    exit 2
    ;;
esac

exit 0
