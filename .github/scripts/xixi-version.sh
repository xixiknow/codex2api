#!/usr/bin/env bash
# 计算 xixi-main 分支的发布版本号，规则详见 docs/xixi-branch.md：
#
#   正式版：v<上游基线>-xixi.<N>               例 v3.1.1-xixi.1、v3.1.1-xixi.2
#   边缘版：v<上游基线>-xixi.<N>-edge.<sha7>   例 v3.1.1-xixi.3-edge.ab12cd3（N 为下一个正式版序号）
#
# <上游基线> = 已合入 HEAD 的上游（james-6-23/codex2api）最高 vX.Y.Z 标签；
# <N>        = 该基线下已发布的 xixi 序号 + 1，同步到新的上游基线后从 1 重新计数。
# 上游从不使用 -xixi 后缀，因此不会与上游任何现有或未来版本冲突；
# 后台版本比较（admin/system_update.go、frontend/src/lib/versionCheck.ts）会忽略 "-" 之后的部分，
# 所以 v3.1.1-xixi.N 视同 v3.1.1，上游发布 v3.1.2 后会正常提示有新版本（提醒同步上游）。
#
# 需要完整历史（actions/checkout 的 fetch-depth: 0）。输出 key=value 行，
# 在 GitHub Actions 中同时写入 $GITHUB_OUTPUT。
set -euo pipefail

UPSTREAM_URL="${XIXI_UPSTREAM_URL:-https://github.com/james-6-23/codex2api.git}"
SUFFIX="${XIXI_VERSION_SUFFIX:-xixi}"

# fork 不会继承上游标签，这里只把上游的 v* 标签拉到本地用于定位基线（不会推回 origin）。
git fetch --quiet --no-tags "$UPSTREAM_URL" '+refs/tags/v*:refs/tags/v*'
if git remote get-url origin >/dev/null 2>&1; then
  git fetch --quiet --tags origin || true
fi

base="$(git tag --merged HEAD --list 'v*' | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -n 1 || true)"
if [[ -z "$base" ]]; then
  echo "xixi-version: 找不到已合入 HEAD 的上游 vX.Y.Z 标签" >&2
  exit 1
fi

last="$(git tag --list "${base}-${SUFFIX}.*" \
  | sed -E "s/^${base//./\\.}-${SUFFIX}\.//" \
  | grep -E '^[0-9]+$' | sort -n | tail -n 1 || true)"
next=$(( ${last:-0} + 1 ))
sha="$(git rev-parse --short=7 HEAD)"

release="${base}-${SUFFIX}.${next}"
edge="${release}-edge.${sha}"

{
  echo "base=${base}"
  echo "number=${next}"
  echo "release=${release}"
  echo "release_plain=${release#v}"
  echo "edge=${edge}"
  echo "edge_plain=${edge#v}"
} | tee -a "${GITHUB_OUTPUT:-/dev/null}"
