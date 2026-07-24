#!/bin/bash
# daily-commit.sh — commit + push real WIP once a day, safely.
#
# Run by a launchd job (see scripts/install-daily-commit.sh). Behaviour:
#   - Operates only on branch `main`.
#   - If the tree is clean AND nothing is ahead of origin → does nothing.
#   - Otherwise: builds every module and runs unit tests; commits + pushes
#     ONLY if they pass. If tests/build fail, it commits nothing and logs why.
#   - NEVER stages .github/workflows/* (limited Actions minutes).
#   - Single-line commit message, no Co-Authored-By, no trailers.
#   - Never force-pushes.
#
# DRY_RUN=1 logs what it would do without committing/pushing.
set -uo pipefail

# launchd starts with a minimal PATH — set an explicit one.
export PATH="/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:/Users/amayabdaniel/go/bin:${PATH:-}"

REPO="/Users/amayabdaniel/projectz/aerial-ran-platform"
DRY_RUN="${DRY_RUN:-0}"

cd "$REPO" || { echo "repo not found at $REPO"; exit 1; }

echo "===== $(date '+%Y-%m-%d %H:%M:%S %z') daily-commit (dry_run=$DRY_RUN) ====="

branch=$(git branch --show-current)
if [ "$branch" != "main" ]; then
  echo "on branch '$branch', not 'main' — skipping"
  exit 0
fi

git fetch --quiet origin main 2>/dev/null || echo "warn: fetch failed (offline?)"

# Is there anything to do?
dirty=0
[ -n "$(git status --porcelain)" ] && dirty=1
ahead=$(git rev-list --count origin/main..HEAD 2>/dev/null || echo 0)

if [ "$dirty" = "0" ] && [ "${ahead:-0}" = "0" ]; then
  echo "working tree clean and not ahead of origin — nothing to do"
  exit 0
fi

# ── Stage WIP (excluding CI workflows) ───────────────────────────────
if [ "$dirty" = "1" ]; then
  git add -A
  # Unstage any CI workflow changes — never auto-commit these.
  git reset -q -- .github/workflows 2>/dev/null || true

  if git diff --cached --quiet; then
    echo "only .github/workflows changed (or nothing stageable) — not committing"
  else
    # ── Gate on build + tests ───────────────────────────────────────
    echo "--- make build ---"
    if ! make build >/tmp/aerial-daily-build.log 2>&1; then
      echo "BUILD FAILED — not committing. tail:"; tail -20 /tmp/aerial-daily-build.log
      git reset -q   # unstage; leave working tree untouched
      exit 1
    fi

    echo "--- make test-unit ---"
    test_out=$(make test-unit 2>&1)
    echo "$test_out"
    if echo "$test_out" | grep -q "^FAIL "; then
      echo "TESTS FAILED — not committing."
      git reset -q
      exit 1
    fi

    # ── Compose a single-line message from the staged diff ──────────
    nfiles=$(git diff --cached --name-only | wc -l | tr -d ' ')
    tops=$(git diff --cached --name-only | sed 's#/.*##' | sort -u | head -4 | paste -sd, - )
    msg="chore: daily WIP commit — ${nfiles} files (${tops})"

    if [ "$DRY_RUN" = "1" ]; then
      echo "[dry-run] would commit: $msg"
      git reset -q
    else
      git commit -m "$msg"
      echo "committed: $msg"
    fi
  fi
fi

# ── Push (covers this commit + any earlier unpushed commits) ─────────
if [ "$DRY_RUN" = "1" ]; then
  echo "[dry-run] would: git push origin main"
  exit 0
fi

# Load keychain-stored SSH keys into the agent (launchd has none by default).
ssh-add --apple-load-keychain 2>/dev/null || true

if [ -n "$(git rev-list --count origin/main..HEAD 2>/dev/null)" ] && [ "$(git rev-list --count origin/main..HEAD 2>/dev/null)" != "0" ]; then
  echo "--- git push origin main ---"
  git push origin main && echo "pushed" || echo "PUSH FAILED (check SSH key in keychain)"
else
  echo "nothing to push"
fi
