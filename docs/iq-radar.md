# 智力雷达

用户端入口为 `/iq-radar`，位于“渠道状态”旁。后端接口为需要用户登录的 `GET /api/v1/iq-radar`，沿用面板用户限流。

## 原始数据源

参考站点云渡的页面注明来源为 [Codex Radar](https://codexradar.com/)。本功能直接读取原始公共接口，不依赖云渡账户、登录态或私有代理接口：

- 模型目录：`https://api.codexradar.com/api/v1/library-catalog`
- 模型与档位评分：`https://api.codexradar.com/api/v1/radar-bench-score?model=...&effort=...`
- 滚动 24 小时社区评分：`https://codexradar.com/api/model-ratings?view=public&window=24h`

模型与档位来自所有 `collections[].model_effort_selections` 的去重并集，并排除 `retired_model_ids`。不使用范围较窄的 execution selections，不在产品代码中固定模型列表。

2026-10-27 核对时，上述目录包含 6 个评测模型、34 个档位，当前 RadarBench 满分 150、任务数 64。页面以接口实际返回的目录与满分为准。社区数据包含的其他模型家族也会保留。

旧 `/api/v1/table` 已返回 410；旧 intelligence-efficiency 快照采用不同 IQ 评分体系，不能作为当前 RadarBench 的降级数据源。

## 口径

- 每个评分响应须与目录的 model、effort、benchmark、score_version、task_set_sha256、score_max 和 required_tasks 一致，防止不同评测批次混用。
- `score: 0` 是有效零分，`null` 表示未知，两者独立展示。
- 平均费用优先使用有效 `mean`；部分费用缺失时，以 `known_total / known_samples` 计算已知样本均值。未知费用显示“—”，不显示免费。费用是 API 等效美元参考，并非 Aino 扣费。
- 耗时由毫秒换算为分钟，并注明已知样本数。
- 趋势保留每个 UTC 日期最后一条有效评分。不生成缺失日期、不平滑或补齐未知分数；图表同时提供可展开的每日数值表。
- 社区评分独立采用 0–10 分制，仅请求并接受 24 小时窗口；零评价保留空值。
- 最近评测时间与同步时间分别显示。

## 缓存与运维

- 每个服务进程共享一份内存缓存，正常 TTL 为 30 分钟。到期后的首次访问触发后台更新，读取不会等待所有上游请求。
- 一次只运行一个刷新任务，最多同时请求 4 个评分，整轮更新超时为 3 分钟。
- 局部失败保留同一评测批次上次成功的单条数据与原同步时间，并显示过期提示；不同批次不能沿用旧分数。局部失败后 5 分钟重试，首次目录完全不可用时 1 分钟重试。
- 缓存不落数据库，重启后首次访问重新同步。页面同步期间短轮询，平时每分钟检查缓存；隐藏或离开页面时停止轮询。
- 页面“检查更新”读取共享缓存，不允许每个用户强制触发 34 次上游请求。
- HTTP 客户端沿用 `update.proxy_url` 出站代理（环境变量 `UPDATE_PROXY_URL`）。代理配置非法时只禁用雷达数据源，不静默改为直连，也不阻断应用启动。无需配置第三方账户或密钥。
- 只访问固定公共源站，禁止重定向，限制响应大小和请求超时。上游失效不会阻断其他系统页面。

## 验证

```sh
cd backend
go test ./internal/pkg/iqradar
go test -race ./internal/pkg/iqradar ./internal/service -run 'TestIQRadar|TestScore|TestCatalog|TestCommunity|TestSourceHTTP'
go test ./...

cd ../frontend
pnpm run test:run
pnpm run lint:check
pnpm run typecheck
pnpm run build
```

新增测试覆盖目录去重与退役模型排除、批次一致性、零分和空值、已知样本费用、UTC 历史、24 小时社区口径、HTTP 错误与重定向、共享缓存/局部失败、URL 档位选择、失败重试和轮询清理。UI 还需用真实上游数据检查桌面、手机断点、深色模式和图表明细。
