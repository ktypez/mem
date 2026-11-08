#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: install-agent-skill.sh [--target all|codex|opencode|claude|hermes] [--replace]

Install the mem-service-ops skill into supported agent skill directories.
Default target is all. Codex and OpenCode share ~/.agents/skills.
Set HERMES_HOME to select a Hermes profile home.
EOF
}

target=all
replace_existing=false
while (($#)); do
  case "$1" in
    --target)
      (($# >= 2)) || { usage >&2; exit 2; }
      target=$2; shift 2 ;;
    --replace) replace_existing=true; shift ;;
    -h|--help) usage; exit 0 ;;
    *) usage >&2; exit 2 ;;
  esac
done

case "$target" in
  all|codex|opencode|claude|hermes) ;;
  *) printf 'Unknown target: %s\n' "$target" >&2; usage >&2; exit 2 ;;
esac

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
source_dir="$repo_root/skills/devops/mem-service-ops"
hermes_home="${HERMES_HOME:-$HOME/.hermes}"
[[ -s "$source_dir/SKILL.md" ]] || { printf 'Skill source missing: %s/SKILL.md\n' "$source_dir" >&2; exit 1; }

install_one() {
  local agent=$1 parent=$2 dest="$2/mem-service-ops" stage backup_root backup
  [[ "$agent" == hermes ]] && parent="$hermes_home/skills/devops" && dest="$parent/mem-service-ops"
  mkdir -p "$parent"
  if [[ -L "$dest" ]]; then
    printf 'Refusing to replace symlinked skill destination: %s\n' "$dest" >&2
    return 1
  fi
  if [[ -d "$dest" ]] && diff -qr -- "$source_dir" "$dest" >/dev/null; then
    printf '%s: already current (%s)\n' "$agent" "$dest"
    return 0
  fi
  if [[ -e "$dest" && "$replace_existing" != true ]]; then
    printf '%s: existing skill differs at %s; review it or rerun with --replace\n' "$agent" "$dest" >&2
    return 2
  fi

  stage=$(mktemp -d "$parent/.mem-service-ops.install.XXXXXX")
  cp -a "$source_dir/." "$stage/"
  if [[ -e "$dest" ]]; then
    if [[ "$agent" == hermes ]]; then
      backup_root="$hermes_home/cache/mem-skill-backups"
    else
      backup_root="$HOME/.cache/mem-skill-backups/$agent"
    fi
    mkdir -p "$backup_root"
    backup="$backup_root/mem-service-ops.$(date +%Y%m%d-%H%M%S).$$"
    mv -- "$dest" "$backup"
  else
    backup=""
  fi
  if ! mv -- "$stage" "$dest"; then
    [[ -z "$backup" ]] || mv -- "$backup" "$dest"
    printf '%s: install failed; prior copy restored when possible\n' "$agent" >&2
    return 1
  fi
  if [[ -n "$backup" ]]; then
    printf '%s: installed %s (previous copy backed up at %s)\n' "$agent" "$dest" "$backup"
  else
    printf '%s: installed %s\n' "$agent" "$dest"
  fi
}

case "$target" in
  all)
    destinations=(
      "codex:$HOME/.agents/skills"
      "claude:$HOME/.claude/skills"
      "hermes:$hermes_home/skills/devops"
    ) ;;
  codex|opencode) destinations=("$target:$HOME/.agents/skills") ;;
  claude) destinations=("claude:$HOME/.claude/skills") ;;
  hermes) destinations=("hermes:$hermes_home/skills/devops") ;;
esac

# Preflight every target so a conflict does not leave a partial multi-agent install.
conflicts=0
for entry in "${destinations[@]}"; do
  agent=${entry%%:*}
  parent=${entry#*:}
  dest="$parent/mem-service-ops"
  [[ "$agent" == hermes ]] && parent="$hermes_home/skills/devops"
  if [[ -L "$dest" ]]; then
    printf '%s: refusing symlinked destination %s\n' "$agent" "$dest" >&2
    conflicts=1
  elif [[ -e "$dest" && ( ! -d "$dest" || ! -e "$dest/SKILL.md" ) ]]; then
    printf '%s: destination exists but is not a skill directory: %s\n' "$agent" "$dest" >&2
    conflicts=1
  elif [[ -d "$dest" ]]; then
    if ! diff -qr -- "$source_dir" "$dest" >/dev/null && [[ "$replace_existing" != true ]]; then
      printf '%s: existing skill differs at %s; review it or rerun with --replace\n' "$agent" "$dest" >&2
      conflicts=1
    fi
  fi
done
((conflicts == 0)) || exit 2

for entry in "${destinations[@]}"; do
  agent=${entry%%:*}
  parent=${entry#*:}
  [[ "$agent" == hermes ]] && parent="$hermes_home/skills/devops"
  install_one "$agent" "$parent"
done
printf 'Start a new agent session if its skill list was already loaded.\n'
