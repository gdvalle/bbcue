#!/usr/bin/env bash
set -euo pipefail

retries="${1:?missing retries}"
sleep_time="${2:?missing sleep time}"
shift 2

(( $# > 0 )) || exit 0

# 0 retries means 1 attempt total
attempts=$((retries + 1))

[[ "${DEBUG:-0}" == "1" ]] && printf >&2 '> %s\n' "$(printf '%q ' "$@")"

rc=0
for ((i = 1; i <= attempts; i++)); do
  "$@" && exit 0
  rc=$?

  printf >&2 "attempt %d/%d failed (rc=%d)" "$i" "$attempts" "$rc"
  
  if ((i == attempts)); then
    printf >&2 "\n"
    break
  fi

  printf >&2 ", sleeping %s...\n" "$sleep_time"
  sleep "$sleep_time"
done

printf >&2 "all %d attempts failed\n" "$attempts"
exit "$rc"
