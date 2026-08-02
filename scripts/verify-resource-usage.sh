#!/usr/bin/env bash
# Copyright (C) 2026 s12ryt
# SPDX-License-Identifier: GPL-3.0-only

set -u

readonly RESOURCE_STABLE_SECONDS="${S12RYT_RESOURCE_STABLE_SECONDS:-60}"
readonly RESOURCE_RSS_LIMIT_KIB=102400
readonly RESOURCE_PROJECT_ROOT="/opt/s12ryt-ipv6"
readonly RESOURCE_SING_BOX_VERSION="1.13.15"
readonly RESOURCE_SING_BOX_ARCHIVE="sing-box-1.13.15-linux-amd64.tar.gz"
readonly RESOURCE_SING_BOX_SHA256="a3a3ff223b23c3f4731d0a17cb0ef94c97ce257c70721a5b07dc7ca079203c9f"
readonly RESOURCE_SING_BOX_URL="https://github.com/SagerNet/sing-box/releases/download/v1.13.15/${RESOURCE_SING_BOX_ARCHIVE}"

RESOURCE_PANEL_PID=""
RESOURCE_TEMPORARY_DIRECTORY=""
RESOURCE_ADDED_ADDRESSES=()

assert_resource_rss() {
  local panel_rss="${1:-}"
  local sing_box_rss="${2:-}"
  local total

  if [[ ! "$panel_rss" =~ ^[0-9]+$ || ! "$sing_box_rss" =~ ^[0-9]+$ ]]; then
    printf '錯誤：RSS 必須是非負整數 KiB。\n' >&2
    return 1
  fi
  total=$((panel_rss + sing_box_rss))
  printf 'Panel RSS：%s KiB；sing-box RSS：%s KiB；合計 RSS：%s KiB / 上限 %s KiB\n' \
    "$panel_rss" "$sing_box_rss" "$total" "$RESOURCE_RSS_LIMIT_KIB"
  if ((total > RESOURCE_RSS_LIMIT_KIB)); then
    printf '錯誤：資源驗收失敗：合計 RSS %s KiB 超過 %s KiB。\n' \
      "$total" "$RESOURCE_RSS_LIMIT_KIB" >&2
    return 1
  fi
}

resource_process_rss() {
  local pid="${1:-}"
  local rss

  if [[ ! "$pid" =~ ^[1-9][0-9]*$ || ! -r "/proc/${pid}/status" ]]; then
    printf '錯誤：程序 %s 不存在或無法讀取。\n' "$pid" >&2
    return 1
  fi
  rss="$(awk '/^VmRSS:/ { print $2; exit }' "/proc/${pid}/status")"
  if [[ ! "$rss" =~ ^[0-9]+$ ]]; then
    printf '錯誤：無法讀取程序 %s 的 RSS。\n' "$pid" >&2
    return 1
  fi
  printf '%s\n' "$rss"
}

resource_find_sing_box_pid() {
  local panel_pid="${1:-}"
  local children_file="/proc/${panel_pid}/task/${panel_pid}/children"
  local children=""
  local child
  local executable

  [[ -r "$children_file" ]] || return 1
  children="$(<"$children_file")"
  for child in $children; do
    executable="$(readlink -f "/proc/${child}/exe" 2>/dev/null || true)"
    if [[ "${executable##*/}" == "sing-box" ]]; then
      printf '%s\n' "$child"
      return 0
    fi
  done
  return 1
}

resource_cleanup() {
  local status=$?
  local index

  if [[ -n "$RESOURCE_PANEL_PID" ]] && kill -0 "$RESOURCE_PANEL_PID" 2>/dev/null; then
    kill "$RESOURCE_PANEL_PID" 2>/dev/null || true
    wait "$RESOURCE_PANEL_PID" 2>/dev/null || true
  fi
  for ((index = ${#RESOURCE_ADDED_ADDRESSES[@]} - 1; index >= 0; index--)); do
    ip -6 addr del "${RESOURCE_ADDED_ADDRESSES[index]}/128" dev lo 2>/dev/null || true
  done
  if [[ -d "$RESOURCE_PROJECT_ROOT" ]]; then
    rm -rf -- "${RESOURCE_PROJECT_ROOT:?}"
  fi
  if [[ -n "$RESOURCE_TEMPORARY_DIRECTORY" && -d "$RESOURCE_TEMPORARY_DIRECTORY" ]]; then
    rm -rf -- "${RESOURCE_TEMPORARY_DIRECTORY:?}"
  fi
  return "$status"
}

require_resource_command() {
  local command_name

  for command_name in "$@"; do
    if ! command -v "$command_name" >/dev/null 2>&1; then
      printf '錯誤：資源驗收缺少命令 %s。\n' "$command_name" >&2
      return 1
    fi
  done
}

verify_resource_usage() {
  local panel_binary="${1:-}"
  local profile_directory="${2:-}"
  local effective_uid="${S12RYT_RESOURCE_EFFECTIVE_UID:-$EUID}"
  local archive_path
  local extracted_binary
  local actual_digest
  local address
  local health_response=""
  local attempt
  local sing_box_pid=""
  local panel_rss
  local sing_box_rss
  local panel_log
  local required_file

  if [[ ! "$effective_uid" =~ ^[0-9]+$ || "$effective_uid" -ne 0 ]]; then
    printf '錯誤：資源驗收必須以 root 執行。\n' >&2
    return 1
  fi
  if [[ ! "$RESOURCE_STABLE_SECONDS" =~ ^[0-9]+$ || "$RESOURCE_STABLE_SECONDS" -lt 60 ]]; then
    printf '錯誤：穩定觀察時間不得少於 60 秒。\n' >&2
    return 1
  fi
  if [[ ! -f "$panel_binary" || ! -x "$panel_binary" ]]; then
    printf '錯誤：資源驗收面板 binary 不存在或不可執行。\n' >&2
    return 1
  fi
  for required_file in config.json runtime.json sing-box.json addresses.txt; do
    if [[ ! -f "${profile_directory}/${required_file}" ]]; then
      printf '錯誤：資源profile缺少 %s。\n' "$required_file" >&2
      return 1
    fi
  done
  if [[ -e "$RESOURCE_PROJECT_ROOT" ]]; then
    printf '錯誤：資源驗收拒絕覆寫既有 %s。\n' "$RESOURCE_PROJECT_ROOT" >&2
    return 1
  fi
  require_resource_command awk cat curl grep install ip kill mktemp readlink rm sha256sum sleep tar || return 1

  RESOURCE_TEMPORARY_DIRECTORY="$(mktemp -d "${TMPDIR:-/tmp}/s12ryt-resource.XXXXXX")" || {
    printf '錯誤：無法建立資源驗收暫存目錄。\n' >&2
    return 1
  }
  trap resource_cleanup EXIT
  archive_path="${RESOURCE_TEMPORARY_DIRECTORY}/${RESOURCE_SING_BOX_ARCHIVE}"
  if ! curl -fsSL --connect-timeout 5 --max-time 60 "$RESOURCE_SING_BOX_URL" -o "$archive_path"; then
    printf '錯誤：無法下載固定 sing-box 資源驗收資產。\n' >&2
    return 1
  fi
  actual_digest="$(sha256sum "$archive_path" | awk '{ print $1 }')"
  if [[ "$actual_digest" != "$RESOURCE_SING_BOX_SHA256" ]]; then
    printf '錯誤：sing-box 資源驗收資產 SHA256 不符。\n' >&2
    return 1
  fi
  extracted_binary="sing-box-${RESOURCE_SING_BOX_VERSION}-linux-amd64/sing-box"
  if ! tar -tzf "$archive_path" | grep -Fxq "$extracted_binary"; then
    printf '錯誤：sing-box 資源驗收壓縮檔結構無效。\n' >&2
    return 1
  fi
  if ! tar -xzf "$archive_path" -C "$RESOURCE_TEMPORARY_DIRECTORY" "$extracted_binary"; then
    printf '錯誤：無法解壓縮 sing-box 資源驗收資產。\n' >&2
    return 1
  fi

  install -d -m 0700 \
    "$RESOURCE_PROJECT_ROOT" \
    "${RESOURCE_PROJECT_ROOT}/bin" \
    "${RESOURCE_PROJECT_ROOT}/config" \
    "${RESOURCE_PROJECT_ROOT}/secrets" \
    "${RESOURCE_PROJECT_ROOT}/state" \
    "${RESOURCE_PROJECT_ROOT}/tmp" || return 1
  install -m 0755 "$panel_binary" "${RESOURCE_PROJECT_ROOT}/bin/s12ryt-ipv6" || return 1
  install -m 0755 "${RESOURCE_TEMPORARY_DIRECTORY}/${extracted_binary}" "${RESOURCE_PROJECT_ROOT}/bin/sing-box" || return 1
  if ! "${RESOURCE_PROJECT_ROOT}/bin/s12ryt-ipv6" init >"${RESOURCE_TEMPORARY_DIRECTORY}/init.log" 2>&1; then
    printf '錯誤：資源驗收面板初始化失敗。\n' >&2
    return 1
  fi
  install -m 0600 "${profile_directory}/config.json" "${RESOURCE_PROJECT_ROOT}/config/config.json" || return 1
  install -m 0600 "${profile_directory}/runtime.json" "${RESOURCE_PROJECT_ROOT}/state/runtime.json" || return 1
  install -m 0600 "${profile_directory}/sing-box.json" "${RESOURCE_PROJECT_ROOT}/config/sing-box.json" || return 1

  mapfile -t RESOURCE_ADDED_ADDRESSES <"${profile_directory}/addresses.txt"
  if [[ "${#RESOURCE_ADDED_ADDRESSES[@]}" -ne 64 ]]; then
    printf '錯誤：資源profile必須精確包含 64 個 IPv6。\n' >&2
    return 1
  fi
  for address in "${RESOURCE_ADDED_ADDRESSES[@]}"; do
    if [[ "$address" != *:* || "$address" == */* ]]; then
      printf '錯誤：資源profile包含無效IPv6 %s。\n' "$address" >&2
      return 1
    fi
    if ! ip -6 addr add "${address}/128" dev lo; then
      printf '錯誤：無法配置資源驗收IPv6 %s。\n' "$address" >&2
      return 1
    fi
  done

  panel_log="${RESOURCE_TEMPORARY_DIRECTORY}/panel.log"
  "${RESOURCE_PROJECT_ROOT}/bin/s12ryt-ipv6" serve >"$panel_log" 2>&1 &
  RESOURCE_PANEL_PID=$!
  for ((attempt = 0; attempt < 30; attempt++)); do
    if ! kill -0 "$RESOURCE_PANEL_PID" 2>/dev/null; then
      printf '錯誤：資源驗收面板提前結束。\n' >&2
      cat "$panel_log" >&2
      return 1
    fi
    health_response="$(curl -fsS --connect-timeout 1 --max-time 2 \
      'http://127.0.0.1:34456/configureme1/healthz' 2>/dev/null || true)"
    if [[ "$health_response" == *'"status":"ok"'* ]]; then
      break
    fi
    sleep 1
  done
  if [[ "$health_response" != *'"status":"ok"'* ]]; then
    printf '錯誤：資源驗收面板未在時限內通過健康檢查。\n' >&2
    cat "$panel_log" >&2
    return 1
  fi
  for ((attempt = 0; attempt < 10; attempt++)); do
    sing_box_pid="$(resource_find_sing_box_pid "$RESOURCE_PANEL_PID" || true)"
    [[ -n "$sing_box_pid" ]] && break
    sleep 1
  done
  if [[ -z "$sing_box_pid" ]]; then
    printf '錯誤：找不到受面板管理的 sing-box 程序。\n' >&2
    return 1
  fi

  printf '資源工作負載已啟動：64 IPv6、28 nodes；穩定觀察 %s 秒。\n' "$RESOURCE_STABLE_SECONDS"
  sleep "$RESOURCE_STABLE_SECONDS"
  if ! kill -0 "$RESOURCE_PANEL_PID" 2>/dev/null || ! kill -0 "$sing_box_pid" 2>/dev/null; then
    printf '錯誤：panel 或 sing-box 未通過穩定觀察。\n' >&2
    cat "$panel_log" >&2
    return 1
  fi
  panel_rss="$(resource_process_rss "$RESOURCE_PANEL_PID")" || return 1
  sing_box_rss="$(resource_process_rss "$sing_box_pid")" || return 1
  assert_resource_rss "$panel_rss" "$sing_box_rss"
}

main() {
  local command="${1:-}"

  case "$command" in
    verify)
      if [[ "$#" -ne 3 ]]; then
        printf '用法：verify-resource-usage.sh verify PANEL_BINARY PROFILE_DIRECTORY\n' >&2
        return 1
      fi
      verify_resource_usage "$2" "$3"
      ;;
    *)
      printf '用法：verify-resource-usage.sh verify PANEL_BINARY PROFILE_DIRECTORY\n' >&2
      return 1
      ;;
  esac
}

if [[ "${BASH_SOURCE[0]}" == "$0" && "${S12RYT_RESOURCE_SOURCE_ONLY:-0}" != "1" ]]; then
  main "$@"
fi
