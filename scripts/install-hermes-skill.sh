#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'Usage: %s [--replace]\n' "$0"
  printf 'Install the repository mem-service-ops skill into $HERMES_HOME (default: ~/.hermes).\n'
}

replace_existing=false
case "${1:-}" in
  "") ;;
  --replace) replace_existing=true ;;
  -h|--help) usage; exit 0 ;;
  *) usage >&2; exit 2 ;;
esac
if [[ "$#" -gt 1 ]]; then usage >&2; exit 2; fi

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
source_dir="$repo_root/skills/devops/mem-service-ops"
hermes_home="${HERMES_HOME:-$HOME/.hermes}"
target_parent="$hermes_home/skills/devops"
target_dir="$target_parent/mem-service-ops"

if [[ ! -s "$source_dir/SKILL.md" ]]; then
  printf 'Skill source not found: %s/SKILL.md\n' "$source_dir" >&2
  exit 1
fi

mkdir -p "$target_parent"
if [[ -d "$target_dir" ]]; then
  if diff -qr -- "$source_dir" "$target_dir" >/dev/null; then
    printf 'Skill already current: %s\n' "$target_dir"
    exit 0
  fi
  if [[ "$replace_existing" != true ]]; then
    printf 'An existing skill differs at %s. Review it, then rerun with --replace to install this repository version.\n' "$target_dir" >&2
    exit 2
  fi
fi

stage_dir="$(mktemp -d "$target_parent/.mem-service-ops.install.XXXXXX")"
backup_dir=""
cleanup() {
  if [[ -n "$stage_dir" && -d "$stage_dir" ]]; then rm -rf -- "$stage_dir"; fi
}
trap cleanup EXIT
cp -a "$source_dir/." "$stage_dir/"

if [[ -d "$target_dir" ]]; then
  backup_root="$hermes_home/cache/mem-skill-backups"
  mkdir -p "$backup_root"
  backup_dir="$backup_root/mem-service-ops.$(date +%Y%m%d-%H%M%S).$$"
  mv -- "$target_dir" "$backup_dir"
fi
if ! mv -- "$stage_dir" "$target_dir"; then
  if [[ -n "$backup_dir" && -d "$backup_dir" ]]; then mv -- "$backup_dir" "$target_dir"; fi
  printf 'Could not install skill; previous version restored when available.\n' >&2
  exit 1
fi
stage_dir=""
printf 'Installed skill: %s\n' "$target_dir"
if [[ -n "$backup_dir" ]]; then printf 'Previous skill backed up to: %s\n' "$backup_dir"; fi
printf 'Start a new Hermes session if the current session does not discover the skill.\n'
