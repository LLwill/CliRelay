# Backend Structure Baseline

更新时间：2026-06-08

本基线对应 2026-06-08 这轮后端架构重放完成后的状态。目标从“冻结旧债务”切换为“固化已收敛结果”：继续阻止新增大文件、`sdk -> internal` 反向依赖和 management handler 直连持久化路径，同时把 allowlist 收紧到当前真实剩余债务。

## 扫描命令

```bash
python3 scripts/check-backend-structure.py
```

CI 中也会运行同一脚本。扫描器只依赖 Python 标准库，allowlist 位于：

```text
docs/internal-review/backend-structure-allowlist.json
```

## 当前结构指标

基于 2026-06-08 本地重放收口后的基线：

| 指标 | 数量 |
| --- | ---: |
| Go 文件总数 | 850 |
| 生产 Go 文件 | 612 |
| 测试 Go 文件 | 238 |
| `internal/` Go 文件 | 666 |
| `internal/` 生产 Go 文件 | 488 |
| `internal/` 测试 Go 文件 | 178 |
| 生产 Go 文件中 `>800` 行 | 7 |
| 生产 Go 文件中 `>1200` 行 | 2 |
| `internal/` 生产 Go 文件中 `>800` 行 | 6 |
| `internal/` 生产 Go 文件中 `>1200` 行 | 2 |
| 生产 `sdk/**` 中直接导入 `internal/**` 的文件 | 26 |
| 管理端 `Handler` receiver 方法 | 249 |
| `server.go` 内管理路由注册 | 0 |
| `internal/` 生产目录 | 107 |
| `internal/` 有同级测试目录 | 53 |
| `internal/` 无同级测试目录 | 54 |

## 当前 `>1200` 行生产文件

这些文件是当前仅剩的 `>1200` 行生产文件，只允许通过 allowlist 保持现状；继续增长会触发结构扫描失败。

| 文件 | 行数 | 治理阶段 |
| --- | ---: | --- |
| `internal/logging/request_logger.go` | 1268 | Phase 3 |
| `internal/registry/model_definitions_static_data.go` | 1233 | 静态数据例外 |

## 当前 `>800` 行生产文件

| 文件 | 行数 | 说明 |
| --- | ---: | --- |
| `internal/logging/request_logger.go` | 1268 | 仍是请求日志聚合热点 |
| `internal/registry/model_definitions_static_data.go` | 1233 | 静态模型定义例外 |
| `internal/registry/model_registry.go` | 1189 | registry 聚合热点，已降到 `<1200` |
| `sdk/api/handlers/handlers.go` | 1160 | SDK façade 仍偏厚，但未新增 `sdk -> internal` 债务 |
| `internal/api/handlers/management/usage_logs_handler.go` | 1078 | usage logs handler 仍是管理端大文件 |
| `internal/api/handlers/management/api_tools.go` | 1048 | API tools 管理端大文件 |
| `internal/api/handlers/management/models.go` | 937 | model config 管理端大文件 |

## 门禁规则

- 生产 Go 文件 `>800` 行：扫描输出 warning，作为治理提示。
- 生产 Go 文件 `>1200` 行：默认失败；只有 `backend-structure-allowlist.json` 中登记的历史债务可通过。
- allowlist 中的大文件带有 `max_lines`，文件继续增长会失败；收敛后应同步收紧 allowlist。
- 生产 `sdk/**` 文件禁止新增对 `github.com/router-for-me/CLIProxyAPI/v6/internal/**` 的导入。
- 现存 `sdk -> internal` 导入按文件和 import path 精确登记；同一文件新增 internal import 也会失败。
- 管理端 handler 禁止新增对 YAML/SQLite 持久化函数的直接调用。
- 当前生产 management handler 直连持久化调用已清零；若再次出现将直接触发扫描失败。

## 架构例外登记

- `internal/registry/model_definitions_static_data.go` 是静态模型定义数据，暂按静态数据例外处理；如果后续引入生成器或数据文件，应将其移出业务大文件债务。
- `internal/logging/request_logger.go` 仍是业务大文件债务，但目前已成为 Phase 3 之后少数残留的大文件之一，后续应结合 request log pipeline 再继续下钻。

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
- 新管理 API 必须通过 transport + use case/service 边界进入，不得把业务规则继续堆到超级 `Handler`；runtime settings 持久化也必须走 service/bridge，不得回退到 handler 直写。
- 新 provider 执行逻辑必须优先复用 runtime pipeline 或先补 pipeline 抽象，不得复制完整横切 executor 模板。
- 每个阶段结束时都要更新本基线或 allowlist，确保债务数量只减少、不扩大。
