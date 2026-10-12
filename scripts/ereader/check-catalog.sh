#!/usr/bin/env bash
# Walk a live OPDS catalog the way CrossPoint does (MAD-833): every
# navigation link and every "next" page, each fed through CrossPoint's own
# parser (devices/crosspoint-harness). Fails on a parse error, truncation, a
# feed without the inline search template, or a link that leaves https.
#
#   scripts/ereader/check-catalog.sh https://host/api/opds user bh_token [path/to/cpopds]
set -euo pipefail
root=${1:?catalog url}; user=${2:?username}; token=${3:?token}
parser=${4:-$(dirname "$0")/../../devices/crosspoint-harness/build/cpopds}
[ -x "$parser" ] || { echo "build the parser first: make -C devices/crosspoint-harness" >&2; exit 2; }

queue=("$root"); seen=" "; pages=0; books=0; failed=0
while [ ${#queue[@]} -gt 0 ]; do
  url=${queue[0]}; queue=("${queue[@]:1}")
  case "$seen" in *" $url "*) continue ;; esac
  seen="$seen$url "; pages=$((pages + 1))
  out=$(curl -sf -u "$user:$token" "$url" | "$parser" || true)
  head=$(printf '%s\n' "$out" | head -1)
  problem=""
  [ "$head" = "${head#error=0 truncated=0}" ] && problem="parser: $head"
  printf '%s\n' "$out" | grep -q '^search=.*{searchTerms}' || problem="${problem:+$problem; }no inline search template"
  if [ "${root%%:*}" = https ] && printf '%s\n' "$out" | grep -E '^(next=|prev=|BOOK|NAV)' | grep -q 'http://'; then
    problem="${problem:+$problem; }http:// link in an https catalog"
  fi
  if [ -n "$problem" ]; then echo "FAIL $url: $problem"; failed=$((failed + 1)); fi
  books=$((books + $(printf '%s\n' "$out" | grep -c '^BOOK' || true)))
  while IFS= read -r link; do [ -n "$link" ] && queue+=("$link"); done < <(
    printf '%s\n' "$out" | sed -n -e 's/^next=\(..*\)$/\1/p' -e 's/^NAV  | .* | .* | \(.*\)$/\1/p')
  [ $pages -ge 500 ] && { echo "stopping at 500 pages"; break; }
done
echo "$pages pages, $books book entries, $failed failing"
[ $failed -eq 0 ]
