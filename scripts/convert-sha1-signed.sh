#!/usr/bin/env bash
# Converts the sxcli-fw sha256 repository into a fresh sha1 repository,
# replaying every commit in order and PGP-signing each one at creation.
# (fast-export | fast-import cannot sign, and signatures could not
# survive the hash change anyway — replaying is the single-pass way.)
#
# Run this on the machine holding the signing key. Linear history only;
# author/committer names and BOTH dates are preserved exactly, but the
# author and committer EMAIL is overridden with the given one — so every
# replayed commit matches the identity of the PGP key and the forge
# account (Codeberg shows Verified only when the committer email belongs
# to the account the key is registered under). The only differences in
# the replay are the hashes, the emails and the signatures.
#
# usage: convert-sha1-signed.sh <source-repo> <target-dir> <email> [signing-key-id]
#
# With no key-id argument the machine's `git config user.signingkey`
# must already point at the right key. The gpg agent prompts once and
# caches for the remaining commits.

set -euo pipefail

usage="usage: convert-sha1-signed.sh <source-repo> <target-dir> <email> [signing-key-id]"
src=${1:?$usage}
dst=${2:?$usage}
email=${3:?$usage}
key=${4:-}

src=$(cd "$src" && pwd)

if [ -e "$dst" ]; then
    echo "error: target $dst already exists" >&2
    exit 1
fi
if [ -n "$(git -C "$src" status --porcelain)" ]; then
    echo "error: source repo has uncommitted changes" >&2
    exit 1
fi
if [ "$(git -C "$src" rev-list --merges master | wc -l)" -ne 0 ]; then
    echo "error: history is not linear; this script only replays linear history" >&2
    exit 1
fi

echo "source object format: $(git -C "$src" rev-parse --show-object-format)"

mkdir -p "$dst"
cd "$dst"
git init -q -b master   # sha1 is the default object format
git config user.name  "$(git -C "$src" config user.name)"
git config user.email "$email"
if [ -n "$key" ]; then
    git config user.signingkey "$key"
fi

for c in $(git -C "$src" rev-list --reverse master); do
    # replace the worktree with this commit's tree, exactly
    git rm -rfq --ignore-unmatch -- . >/dev/null 2>&1 || true
    git -C "$src" archive "$c" | tar -x
    git add -A
    GIT_AUTHOR_NAME="$(git -C "$src" log -1 --format=%an "$c")" \
    GIT_AUTHOR_EMAIL="$email" \
    GIT_AUTHOR_DATE="$(git -C "$src" log -1 --format=%aI "$c")" \
    GIT_COMMITTER_NAME="$(git -C "$src" log -1 --format=%cn "$c")" \
    GIT_COMMITTER_EMAIL="$email" \
    GIT_COMMITTER_DATE="$(git -C "$src" log -1 --format=%cI "$c")" \
    git commit -qS --no-verify -m "$(git -C "$src" log -1 --format=%B "$c")"
    echo "signed $(git rev-parse --short HEAD)  $(git -C "$src" log -1 --format=%s "$c")"
done

echo
echo "== verification =="
old_n=$(git -C "$src" rev-list --count master)
new_n=$(git rev-list --count master)
echo "commits: source=$old_n target=$new_n"
test "$old_n" = "$new_n"
if diff -r --exclude=.git . "$src" >/dev/null; then
    echo "worktrees: identical"
else
    echo "worktrees: DIFFER" >&2
    exit 1
fi
git log --show-signature -1 HEAD | sed -n '1,6p'
echo
echo "ok: $dst is the signed sha1 replay of $src"
echo "next: adopt it as canonical, then: git config commit.gpgsign true"
