# xixi-main 分支：开发、CI 与版本规则

`xixi-main` 是本 fork（`xixiknow/codex2api`）的长期功能开发分支。`main` 只用来跟踪上游
`james-6-23/codex2api`，自有功能都提交到 `xixi-main`（或从它拉出的功能分支再合回）。

## 分支流程

```
上游 james-6-23/codex2api ──► main（只同步上游，不直接开发）
                                 │ 定期合并
                                 ▼
                            xixi-main ◄── feat/xxx、fix/xxx（PR 合入）
```

- 新功能：从 `xixi-main` 拉 `feat/<name>` / `fix/<name>`，开 PR 回 `xixi-main`。
- 同步上游：先在 GitHub 上 Sync fork 更新 `main`，再把 `main` 合并进 `xixi-main`（`git merge main`），
  不要 rebase，以便上游标签所在提交保留为 `xixi-main` 的祖先（版本基线靠它识别）。
- 不要建 `xixi-main/...` 形式的分支名（与 `xixi-main` 引用冲突）。

## 版本号规则

| 类型 | 格式 | 示例 |
| --- | --- | --- |
| 正式版 | `v<上游基线>-xixi.<N>` | `v3.1.1-xixi.1`、`v3.1.1-xixi.2` |
| edge 版 | `v<上游基线>-xixi.<N>-edge.<sha7>` | `v3.1.1-xixi.3-edge.ab12cd3` |

- **上游基线**：已合入当前提交的上游最高 `vX.Y.Z` 标签（CI 会临时拉取上游标签计算）。
- **N**：该基线下已发布的 xixi 正式版序号 + 1；合入新的上游版本（如 `v3.1.2`）后从 1 重新计数
  → `v3.1.2-xixi.1`。edge 版中的 N 表示"下一个正式版"的序号。
- **不会冲突**：上游从不使用 `-xixi` 后缀，所以与上游任何现有或未来版本都不重名；
  也绝不会再出现与上游同名的 `v3.1.1`。
- **版本比较**：后台的更新检查（`admin/system_update.go`、`frontend/src/lib/versionCheck.ts`）
  忽略 `-` 之后的部分，`v3.1.1-xixi.N` 视同 `v3.1.1`；上游发布 `v3.1.2` 后后台会提示有新版本，
  即提醒该同步上游了。

本地查看下一个版本号：

```bash
bash .github/scripts/xixi-version.sh
```

## CI 与镜像

| 工作流 | 触发 | 作用 |
| --- | --- | --- |
| `xixi-ci.yml` | push 到 `xixi-main`、手动、被 release 调用 | 与 `pr-check.yml` 相同的前端/Go/PG/race 检查；push 时全部通过后发布 edge 镜像 |
| `xixi-docker.yml` | 仅被调用 | 构建 `linux/amd64` + `linux/arm64` 镜像并推送到 GHCR |
| `xixi-release.yml` | 手动（Actions → xixi Release，分支选 `xixi-main`） | 完整 CI → 算版本 → 镜像 + 多平台二进制 → 最后打标签并创建 GitHub Release |

指向 `xixi-main` 的 PR 仍由已有的 `pr-check.yml` 检查。

镜像 `ghcr.io/xixiknow/codex2api` 的标签：

| 标签 | 含义 |
| --- | --- |
| `3.1.1-xixi.2` | 某个正式版（不可变） |
| `xixi` | 最新正式版（浮动） |
| `3.1.1-xixi.3-edge.ab12cd3` | `xixi-main` 某次提交的 edge 构建 |
| `xixi-edge` | `xixi-main` 最新提交（浮动） |

xixi 流程从不推送 `latest` 或纯 `X.Y.Z` 标签。

## 注意

- **不要手动推送 `v*-xixi.*` 标签**：上游的 `docker-image.yml` / `release.yml` 监听 `v*` 标签推送，
  手动推送会让它们再构建一次并覆盖 `latest`。由 `xixi-release.yml` 用 `GITHUB_TOKEN` 创建的标签不会触发其他工作流。
- 发布失败不会占用版本号（标签在最后一步才创建），修复后重新运行即可。
- `xixi-ci.yml` 复制自 `pr-check.yml`；上游调整 `pr-check.yml` 的检查项时，同步一下这里。
