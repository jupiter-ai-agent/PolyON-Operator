#!/usr/bin/env zsh
#
# PolyON Release Script
# 
# 단일 명령으로: 빌드 → manifest 동기화 → Operator 리빌드 → push → commit
# 재설치해도 동일 결과를 보장하는 구조적 장치.
#
# Usage:
#   ./scripts/release.sh              # Core + Console + Operator (기본)
#   ./scripts/release.sh core         # Core만
#   ./scripts/release.sh console      # Console만  
#   ./scripts/release.sh --bump core  # Core 버전 bump + 빌드
#   ./scripts/release.sh --status     # 현재 상태 확인 (빌드 안 함)
#
set -euo pipefail

REPO_ROOT="${0:A:h:h}"
VERSION_FILE="$REPO_ROOT/VERSION"
REGISTRY="jupitertriangles"
PLATFORM="linux/arm64"

# 개별 리포 경로 (PolyON-Operator와 동일 레벨)
CORE_ROOT="${REPO_ROOT}/../PolyON-Core"
CONSOLE_ROOT="${REPO_ROOT}/../PolyON-Console"
PORTAL_ROOT="${REPO_ROOT}/../PolyON-Portal"

# Colors
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
log()  { echo "${GREEN}[release]${NC} $*"; }
warn() { echo "${YELLOW}[release]${NC} $*"; }
err()  { echo "${RED}[release]${NC} $*" >&2; }
step() { echo "\n${BLUE}━━━ $* ━━━${NC}"; }

# ── VERSION file ────────────────────────────────────────────────────────────

typeset -A VER

load_versions() {
  while IFS='=' read -r key val; do
    [[ -z "$key" || "$key" == \#* ]] && continue
    VER[$key]="$val"
  done < "$VERSION_FILE"
}

save_versions() {
  : > "$VERSION_FILE"
  for key in core console portal operator dc mail appengine; do
    [[ -n "${VER[$key]:-}" ]] && echo "$key=${VER[$key]}" >> "$VERSION_FILE"
  done
}

bump_patch() {
  local v="$1"
  local major=${v%%.*}
  local rest=${v#*.}
  local minor=${rest%%.*}
  local patch=${rest#*.}
  echo "$major.$minor.$((patch + 1))"
}

# ── Build ───────────────────────────────────────────────────────────────────

build_core() {
  local tag="$REGISTRY/polyon-core:v${VER[core]}"
  step "Core v${VER[core]}"
  cd "$CORE_ROOT"
  log "Go build..."
  go build ./...
  log "Docker build: $tag"
  docker build --platform "$PLATFORM" --build-arg "CORE_VERSION=v${VER[core]}" -t "$tag" .
  log "Docker push..."
  docker push "$tag"
  log "Core ✓"
}

build_console() {
  local tag="$REGISTRY/polyon-console:v${VER[console]}"
  step "Console v${VER[console]}"
  cd "$CONSOLE_ROOT"
  # Sync package.json version
  sed -i '' "s/\"version\": \"[^\"]*\"/\"version\": \"${VER[console]}\"/" package.json
  log "npm build..."
  rm -rf dist
  npm run build 2>&1 | tail -1
  log "Docker build: $tag"
  docker build --platform "$PLATFORM" --no-cache -t "$tag" -f Dockerfile.deploy .
  log "Docker push..."
  docker push "$tag"
  log "Console ✓"
}

build_portal() {
  local tag="$REGISTRY/polyon-portal:v${VER[portal]:-0.2.0}"
  step "Portal v${VER[portal]:-0.2.0}"
  cd "$PORTAL_ROOT"
  log "Docker build: $tag"
  docker build --platform "$PLATFORM" -t "$tag" .
  log "Docker push..."
  docker push "$tag"
  log "Portal ✓"
}

build_operator() {
  local tag="$REGISTRY/polyon-operator:v${VER[operator]}"
  step "Operator v${VER[operator]}"
  cd "$REPO_ROOT"
  log "Docker build: $tag"
  docker build --platform "$PLATFORM" -t "$tag" .
  log "Docker push..."
  docker push "$tag"
  log "Operator ✓"
}

build_dc() {
  local tag="$REGISTRY/polyon-dc:v${VER[dc]}"
  step "DC v${VER[dc]}"
  cd "$REPO_ROOT/dc"
  docker build --platform "$PLATFORM" -t "$tag" .
  docker push "$tag"
  log "DC ✓"
}

build_mail() {
  local tag="$REGISTRY/polyon-mail:v${VER[mail]}"
  step "Mail v${VER[mail]}"
  cd "$REPO_ROOT/mail"
  docker build --platform "$PLATFORM" -t "$tag" .
  docker push "$tag"
  log "Mail ✓"
}

# ── Manifest sync ──────────────────────────────────────────────────────────

sync_manifests() {
  step "Operator manifest 이미지 태그 동기화"
  local m="$REPO_ROOT/manifests"

  sed -i '' "s|$REGISTRY/polyon-core:v[0-9.]*|$REGISTRY/polyon-core:v${VER[core]}|g" "$m/core.yaml"
  log "core.yaml → v${VER[core]}"

  sed -i '' "s|$REGISTRY/polyon-console:v[0-9.]*|$REGISTRY/polyon-console:v${VER[console]}|g" "$m/console.yaml"
  log "console.yaml → v${VER[console]}"

  sed -i '' "s|$REGISTRY/polyon-dc:v[0-9.]*|$REGISTRY/polyon-dc:v${VER[dc]}|g" "$m/samba-dc.yaml"
  log "samba-dc.yaml → v${VER[dc]}"

  sed -i '' "s|$REGISTRY/polyon-mail:v[0-9.]*|$REGISTRY/polyon-mail:v${VER[mail]}|g" "$m/stalwart.yaml"
  log "stalwart.yaml → v${VER[mail]}"

  sed -i '' "s|$REGISTRY/polyon-appengine:v[0-9.]*|$REGISTRY/polyon-appengine:v${VER[appengine]}|g" "$m/appengine.yaml"
  log "appengine.yaml → v${VER[appengine]}"

  log "동기화 완료 ✓"
}

# ── Git ─────────────────────────────────────────────────────────────────────

git_finalize() {
  step "Git commit + push"
  cd "$REPO_ROOT"
  git add -A

  if git diff --cached --quiet; then
    log "No changes to commit"
    return
  fi

  local msg="release: $1 — core v${VER[core]}, console v${VER[console]}, op v${VER[operator]}"
  git commit -m "$msg"
  git push origin main
  log "Git push ✓"
}

# ── Status ──────────────────────────────────────────────────────────────────

show_status() {
  step "VERSION file"
  for k in core console operator dc mail erpengine; do
    echo "  $k = v${VER[$k]:-?}"
  done

  step "Operator manifest images"
  grep "image:.*$REGISTRY" "$REPO_ROOT/manifests/"*.yaml | sed 's|.*/manifests/|  |'

  step "K8s deployed (polyon)"
  kubectl get deploy,statefulset -n polyon \
    -o custom-columns='NAME:.metadata.name,IMAGE:.spec.template.spec.containers[0].image' \
    --no-headers 2>/dev/null | sed 's/^/  /' || echo "  (namespace not found)"

  step "Sync check"
  local ok=true
  for yaml_file in "$REPO_ROOT/manifests/"*.yaml; do
    local fname="${yaml_file:t}"
    for k in core console portal dc mail appengine; do
      if grep -q "$REGISTRY/polyon-$k:" "$yaml_file" 2>/dev/null; then
        local in_yaml=$(grep -o "$REGISTRY/polyon-$k:v[0-9.]*" "$yaml_file" | head -1)
        local expected="$REGISTRY/polyon-$k:v${VER[$k]:-?}"
        if [[ "$in_yaml" != "$expected" ]]; then
          warn "MISMATCH: $fname has $in_yaml, expected $expected"
          ok=false
        fi
      fi
    done
  done
  $ok && log "All manifests in sync ✓"
}

# ── Main ────────────────────────────────────────────────────────────────────

main() {
  load_versions

  local do_bump=false
  local do_status=false
  local targets=()

  for arg in "$@"; do
    case "$arg" in
      --bump)           do_bump=true ;;
      --status|--dry-run) do_status=true ;;
      core|console|portal|operator|dc|mail|appengine) targets+=("$arg") ;;
      all) targets=(core console portal operator) ;;
      *) err "Unknown: $arg"; exit 1 ;;
    esac
  done

  [[ ${#targets[@]} -eq 0 ]] && targets=(core console)

  if $do_status; then
    show_status
    return
  fi

  # Bump
  if $do_bump; then
    for t in "${targets[@]}"; do
      local old="${VER[$t]}"
      VER[$t]="$(bump_patch "$old")"
      log "$t: v$old → v${VER[$t]}"
    done
    save_versions
  fi

  step "Release: ${targets[*]}"
  echo "  core     = v${VER[core]}"
  echo "  console  = v${VER[console]}"
  echo "  operator = v${VER[operator]}"

  # Build targets
  for t in "${targets[@]}"; do
    case "$t" in
      core)      build_core ;;
      console)   build_console ;;
      portal)    build_portal ;;
      dc)        build_dc ;;
      mail)      build_mail ;;
      appengine) warn "AppEngine은 PolyON-AppEngine 리포에서 별도 빌드" ;;
    esac
  done

  # Always: sync manifests → rebuild operator
  sync_manifests
  build_operator

  # Git
  git_finalize "${targets[*]}"

  step "Release 완료 ✓"
  echo ""
  echo "  Core:     v${VER[core]}"
  echo "  Console:  v${VER[console]}"
  echo "  Operator: v${VER[operator]}"
  echo ""
  log "kubectl set image deploy/polyon-operator operator=$REGISTRY/polyon-operator:v${VER[operator]} -n polyon-operator"
  log "재설치 시 이 버전으로 배포됩니다."
}

main "$@"
