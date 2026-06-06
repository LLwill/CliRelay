# Backend Structure Baseline

更新时间：2026-06-06

本基线对应后端结构治理 Phase 0。目标是先冻结当前结构债务，避免后续重构期间继续新增大文件、反向依赖和 handler 直连持久化路径。

## 扫描命令

```bash
python3 scripts/check-backend-structure.py
```

CI 中也会运行同一脚本。扫描器只依赖 Python 标准库，allowlist 位于：

```text
docs/internal-review/backend-structure-allowlist.json
```

## 当前结构指标

基于 `origin/dev` 的 2026-06-06 基线：

| 指标 | 数量 |
| --- | ---: |
| Go 文件总数 | 604 |
| 生产 Go 文件 | 415 |
| 测试 Go 文件 | 189 |
| `internal/` Go 文件 | 481 |
| `internal/` 生产 Go 文件 | 344 |
| `internal/` 测试 Go 文件 | 137 |
| 生产 Go 文件中 `>800` 行 | 26 |
| 生产 Go 文件中 `>1200` 行 | 14 |
| `internal/` 生产 Go 文件中 `>800` 行 | 23 |
| `internal/` 生产 Go 文件中 `>1200` 行 | 12 |
| 生产 `sdk/**` 中直接导入 `internal/**` 的文件 | 40 |
| 管理端 `Handler` receiver 方法 | 252 |
| `server.go` 内管理路由注册 | 194 |
| `internal/` 生产目录 | 82 |
| `internal/` 有同级测试目录 | 37 |
| `internal/` 无同级测试目录 | 45 |

## 当前 `>1200` 行生产文件

这些文件是历史债务，只允许通过 allowlist 保持现状；继续增长会触发结构扫描失败。

| 文件 | 行数 | 治理阶段 |
| --- | ---: | --- |
| `internal/api/handlers/management/auth_files.go` | 2286 | Phase 1 |
| `sdk/cliproxy/auth/conductor.go` | 3223 | Phase 5 |
| `internal/usage/usage_db.go` | 2530 | Phase 3 |
| `internal/api/handlers/management/config_lists.go` | 2307 | Phase 1/2 |
| `internal/runtime/executor/codex_image_executor.go` | 2233 | Phase 4 |
| `internal/config/config.go` | 2216 | Phase 2 |
| `internal/api/server.go` | 2090 | Phase 6 |
| `sdk/cliproxy/service.go` | 1788 | Phase 6/7 |
| `internal/runtime/executor/antigravity_executor.go` | 1766 | Phase 4 |
| `internal/runtime/executor/claude_executor.go` | 1456 | Phase 4 |
| `internal/runtime/executor/codex_websockets_executor.go` | 1453 | Phase 4 |
| `internal/runtime/executor/opencode_go_executor.go` | 1314 | Phase 4 |
| `internal/logging/request_logger.go` | 1268 | Phase 3 |
| `internal/registry/model_definitions_static_data.go` | 1233 | 静态数据例外 |

## 门禁规则

- 生产 Go 文件 `>800` 行：扫描输出 warning，作为治理提示。
- 生产 Go 文件 `>1200` 行：默认失败；只有 `backend-structure-allowlist.json` 中登记的历史债务可通过。
- allowlist 中的大文件带有 `max_lines`，文件继续增长会失败；收敛后应同步收紧 allowlist。
- 生产 `sdk/**` 文件禁止新增对 `github.com/router-for-me/CLIProxyAPI/v6/internal/**` 的导入。
- 现存 `sdk -> internal` 导入按文件和 import path 精确登记；同一文件新增 internal import 也会失败。
- 管理端 handler 禁止新增对 YAML/SQLite 持久化函数的直接调用。
- 现存管理端直连持久化调用按文件、符号和次数登记；新增调用次数会失败。

## 架构例外登记

- `internal/registry/model_definitions_static_data.go` 是静态模型定义数据，暂按静态数据例外处理；如果后续引入生成器或数据文件，应将其移出业务大文件债务。
- 其余 `>1200` 行文件均为业务逻辑或装配逻辑债务，不属于长期例外，只是迁移前兼容基线。

## 重构前契约测试清单

后续阶段开始迁移前，应按影响面补齐或复核以下测试：

- 管理 API route smoke。
- auth files list/upload/delete/patch 响应字段和状态码。
- config YAML 与 DB-backed runtime settings overlay 顺序。
- request logs query/filter/content/cleanup。
- quota snapshot 写入、查询与保留策略。
- provider executor non-stream/stream/error body 基础路径。
- SSE event translation 和 usage reporting。
- auth manager selection/retry/cooldown/refresh 并发行为。
- service/server route registration、middleware 顺序和 shutdown path。
- SDK public package compatibility compile tests。

## 后续维护要求

- 新业务数据默认进入 SQLite/数据库和管理 API，不得为了前端实现方便新增到 `config.yaml`。
- 新管理 API 必须通过 transport + use case/service 边界进入，不得把业务规则继续堆到超级 `Handler`。
- 新 provider 执行逻辑必须优先复用 runtime pipeline 或先补 pipeline 抽象，不得复制完整横切 executor 模板。
- 每个阶段结束时都要更新本基线或 allowlist，确保债务数量只减少、不扩大。
