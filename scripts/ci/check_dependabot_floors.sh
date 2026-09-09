#!/usr/bin/env bash
# check_dependabot_floors.sh — every suite requirement a module declares is
# ignored by that module's Dependabot entry.
#
# In this repository a `require` on Quark, Nucleus or the orbit root is a
# FLOOR, raised on purpose when a set is cut, not a pin anyone else may move.
# `.github/dependabot.yml` says so and ignores them per directory. The failure
# this guard exists for is silent and easy: the ignore lists are written by
# hand, one per directory, and a `dependency-name` pattern that looks right can
# miss. `github.com/jcsvwinston/orbit/*` does NOT match the bare path
# `github.com/jcsvwinston/orbit`, so `quarkdatasource` — the one module that
# requires Quark, Nucleus and the orbit root at once — sat with its root
# requirement unignored, and Dependabot could have proposed a bump of the very
# repository it lives in.
#
# Nothing breaks when that happens. A pull request simply appears, raises a
# floor nobody decided, and the set is cut around it.
#
# What this checks, for every `go.mod` Dependabot covers: each requirement
# whose path is `github.com/jcsvwinston/<quark|nucleus|orbit>` or a module
# under one of them is matched by some `dependency-name` in the ignore list of
# that directory's entry. A directory whose entry ignores `"*"` is covered by
# definition (that is `proto`, a deliberate leaf).
#
# Usage: bash scripts/ci/check_dependabot_floors.sh
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.." || exit 1

conf=.github/dependabot.yml
[ -f "$conf" ] || { echo "FAIL: $conf does not exist" >&2; exit 1; }

status=0
checked=0

# Reads the gomod entries of dependabot.yml as "directory<TAB>pattern,pattern".
entradas=$(awk '
  function flush() {
    if (dir != "" && eco == "gomod") print dir "\t" pats
    dir = ""; pats = ""
  }
  /^  - package-ecosystem:/ {
    flush()
    eco = $3
    next
  }
  $1 == "directory:" { dir = $2; gsub(/"/, "", dir) }
  $1 == "-" && $2 == "dependency-name:" {
    p = $3; gsub(/"/, "", p)
    pats = (pats == "" ? p : pats "," p)
  }
  END { flush() }
' "$conf")

# El awk de arriba emite una línea por entrada gomod; si el fichero cambia de
# forma y no emite nada, eso es un fallo del guard y no un árbol limpio.
if [ -z "$entradas" ]; then
  echo "FAIL: could not read any gomod entry from $conf — has its shape changed?" >&2
  exit 1
fi

casa() { # casa <ruta> <patron>
  local ruta=$1 pat=$2
  case "$pat" in
    '*') return 0 ;;
    *'*')
      local pre=${pat%\*}
      case "$ruta" in "$pre"*) return 0 ;; esac
      return 1 ;;
    *) [ "$ruta" = "$pat" ] ;;
  esac
}

while IFS=$'\t' read -r dir pats; do
  [ -n "$dir" ] || continue
  gomod=".${dir%/}/go.mod"
  gomod=${gomod#./}
  [ "$dir" = "/" ] && gomod="go.mod"
  if [ ! -f "$gomod" ]; then
    echo "FAIL: $conf covers $dir and there is no $gomod — the directory list is stale" >&2
    status=1
    continue
  fi
  propio=$(awk '$1 == "module" { print $2; exit }' "$gomod")
  while read -r req; do
    [ -n "$req" ] || continue
    [ "$req" = "$propio" ] && continue
    checked=$((checked + 1))
    cubierto=0
    IFS=',' read -r -a lista <<< "$pats"
    for pat in ${lista[@]+"${lista[@]}"}; do
      if casa "$req" "$pat"; then cubierto=1; break; fi
    done
    if [ "$cubierto" -eq 0 ]; then
      echo "FAIL: $gomod requires $req and the $dir entry of $conf does not ignore it." >&2
      echo "      A suite requirement here is a floor raised by the release train." >&2
      echo "      Add: - dependency-name: $req" >&2
      status=1
    fi
  done <<EOF
$(awk '/^require|^\t|^ /{ for (i=1;i<=NF;i++) if ($i ~ /^github\.com\/jcsvwinston\/(quark|nucleus|orbit)(\/|$)/) print $i }' "$gomod" | sort -u)
EOF
done <<< "$entradas"

if [ "$status" -eq 0 ]; then
  echo "dependabot floors OK: $checked suite requirement(s) across the covered modules, all ignored"
fi
exit $status
