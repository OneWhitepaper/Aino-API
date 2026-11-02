# Video 平台接入与验收

`video` 是独立的视频平台，名称与上游厂商品牌无关。它复用现有的账号、
分组、用户密钥、并发调度和用量结算机制，通过 Videos API 调用视频服务。

## 管理配置

1. 在账号管理中添加平台为 `video` 的 API Key 账号，填写实际上游的 Base URL
   与 API Key。协议为 `Videos API`；不会自动安装上游地址或销售价格。
2. 使用“从上游同步模型”，按需设置模型白名单或公开模型到上游模型的映射。
   内置识别 `veo-3.1`、`veo-3.1-fast`、`veo-3.1-lite`、`omni-flash`；
   上游声明视频输出能力的自定义模型也可同步。
3. 将账号加入 `video` 或 `composite` 分组，在分组中配置对应模型和分辨率的
   每秒价格。创建任务前必须存在可用的视频价格，显式填写 0 表示免费。
   组合分组还需启用媒体生成权限；公开别名可通过组合路由指向 `video`。
4. 给用户的 API Key 分配该分组，并按现有规则授予用户分组权限或订阅。
5. 在账号列表“更多 → 测试账号”中，默认连接检测只访问模型列表；
   选择真实视频生成测试才会提交付费任务，并等待生成及下载验证。

## 用户接口

| 方法 | 路径 | 功能 |
| --- | --- | --- |
| POST | `/v1/videos` | 创建异步视频任务 |
| GET | `/v1/videos/{id}` | 查询任务状态 |
| GET | `/v1/videos/{id}/content` | 下载视频，支持 Range |

创建也接受 `/v1/videos/generations` 和现有无 `/v1` 前缀的路由。
网关向通用视频上游统一发送 `POST /v1/videos`。

```bash
export AINO_API_BASE='https://YOUR_AINO_HOST'
export AINO_API_KEY='YOUR_AINO_API_KEY'

curl "$AINO_API_BASE/v1/videos" \
  -H "Authorization: Bearer $AINO_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"model":"veo-3.1","prompt":"A paper boat drifting on a calm lake","seconds":4,"resolution":"720p"}'
```

保留创建响应中的 `id`（本站任务以 `video_` 开头），使用同一把用户密钥查询：

```bash
export AINO_VIDEO_ID='REPLACE_WITH_RETURNED_ID'
curl "$AINO_API_BASE/v1/videos/$AINO_VIDEO_ID" \
  -H "Authorization: Bearer $AINO_API_KEY"

# status 为 completed 后下载；queued / in_progress 每 5–10 秒查询一次。
curl "$AINO_API_BASE/v1/videos/$AINO_VIDEO_ID/content" \
  -H "Authorization: Bearer $AINO_API_KEY" \
  -o video.mp4
```

失败终态为 `failed`，应停止轮询。返回的下载地址指向本站；下载继续使用本站密钥，
不向用户提供上游密钥。JSON 与 multipart 的参考图内容保持透传，模型字段按配置映射。
Veo 时长为 4/6/8 秒，默认 8 秒，可选 720p/1080p；Omni Flash 为 3–10 秒，
默认 3 秒且只支持 720p。自定义模型应显式提供时长。

## 调度与结算约束

- 创建时绑定任务所属用户、API Key 和实际提交账号；查询和下载返回该账号，
  不重新将任务分配给其他账号。其他用户或密钥访问返回 404。
- 任务绑定与创建时的计费元数据保存在 Redis，通常保留 24 小时。不要在等待任务期间
  清空 Redis、撤销原密钥或将原账号移出可用分组。
- 提交被接受不立即结算用户余额；首次查询或下载观察到成功输出时，
  以完成时长和创建时冻结的每秒价格、倍率结算。修改分组价格不影响已提交任务。
- 失败或未完成任务不计完成费用；重复查询/下载共用结算去重机制。
  `RUN_MODE=simple` 仍按原规则记录用量而跳过余额扣款。
- 创建失败不自动重试，避免上游已接单却重复生成。若超时或返回了任务 ID，
  应先查询既有任务，不能直接重复提交。
- 当前按首次成功查询/下载触发结算，未增加无人轮询任务的后台结算器。

## 本地验证

```bash
cd backend
go test -tags=unit ./internal/domain ./internal/service ./internal/handler/... ./internal/server/routes
go test -tags=unit ./internal/handler -run '^TestVideoHTTPWorkflowBillsSnapshotOnce$' -count=1 -v
go build -o /tmp/aino-video-server ./cmd/server

cd ../frontend
pnpm test:run
pnpm typecheck
pnpm lint:check
pnpm build
```

HTTP 工作流测试使用真实的调度、HTTP 传输和用量记录代码，以及本地受控上游，
验证公开模型映射、任务归属、完成状态、完整内容下载、Range、价格快照和单次结算。
这是本地集成验证，不代表真实上游已经成功出片。普通回归不会调用付费上游。

显式授权的真实验证使用私有 JSON 文件（`base_url`、`api_key`），从 `backend` 执行：

```bash
AINO_VIDEO_LIVE_CONFIG=/absolute/private/video-credentials.json \
AINO_VIDEO_LIVE_MODEL=omni-flash \
go test -tags=unit ./internal/handler -run '^TestVideoProviderLiveScheduling$' -count=1 -timeout 12m -v
```

此命令会消耗上游额度，账号与日志存储使用隔离测试数据，不修改生产用户余额。
凭据目录会写入 `creation-started` 防重入标记；请先检查是否已有任务，
不要通过删除标记来盲目重试。凭据不应放入仓库或提交。

## 2026-11-02 验收结果与外部阻塞

本地后端受影响模块的完整单元回归、前端 399 个文件的 3048 项测试、
类型检查、前端 lint，以及前后端生产构建（含嵌入前端的 Go 构建）均已通过。
额外 HTTP 工作流验证及相关服务/处理器的 Go race 检查也已通过。

使用用户授权的真实上游密钥验证时：

- 模型列表和额度查询成功，四个内置视频模型均可见。
- 经本站实际调度提交 `omni-flash`（3 秒）和 `veo-3.1-fast`（4 秒），
  上游均返回 HTTP 500 / `do_request_failed`，未返回任务 ID。
- 绕过本站、直接向上游请求相同的 `POST /v1/videos`，返回同样的 HTTP 500；
  对照请求 ID 为 `202611020449558492414168268d9d6yEO7I78e`。
- 对文档别名 `/v1/videos/generations` 使用 SDK 风格字段
  `seconds: "5"`、`size: "1280x720"`，上游返回 HTTP 503 /
  `model_not_found`，明确提示“分组 auto 下模型 omni-flash 无可用渠道（distributor）”；
  请求 ID 为 `202611020456491911480668268d9d6xWenQAWZ`。
- 上游用量与错误日志检查未发现本轮已计费的任务。

因此代码交付与本地验证已完成，但真实上游生成、真实视频下载的验收仍被外部通道阻塞。
需上游管理员修复该密钥可用的视频渠道或提供可用的视频上游后，再运行一次真实测试；
不能将模型列表可见或本地测试通过当作真实出片成功。生产部署状态以部署回执为准。

协议参考：

- https://apimax.ai/zh/docs/omni-flash
- https://apimax.ai/zh/docs/veo-3-1
- https://apimax.ai/zh/docs/get-task-detail

## 2026-11-02 13:14（北京时间）四模型补充实测

使用同一把已授权密钥直接请求文档规定的 `POST /v1/videos`，720p、最短文生视频，
四个公开 Videos API 模型均返回 HTTP 500 / `do_request_failed`，未返回任务 ID：

| 模型 | 时长 | 上游请求 ID |
| --- | --- | --- |
| `omni-flash` | 3 秒 | `202611020514154827693538268d9d6ky1ebwg8` |
| `veo-3.1` | 4 秒 | `202611020514154720867818268d9d6aMip4mCR` |
| `veo-3.1-fast` | 4 秒 | `202611020514168427264298268d9d6iFRmTRfC` |
| `veo-3.1-lite` | 4 秒 | `202611020514168608781998268d9d68kwkTJDX` |

13:16 的上游用量复核显示 `used_credits: 0`，这四次错误日志的 `quota` 均为 0。
以上结论仅适用于当前密钥、当前通道与这四次请求，不代表整站所有用户均无法使用视频。

`/v1/models` 还列出 `kling-3.0-turbo`、`kling-v1`、`kling-v1-text-to-video`
及 `mj_video`。当前公开视频文档仅列 Omni/Veo 两个系列；Midjourney 文档明确
`mj_video` 使用 `/mj/submit/video` 图生视频协议，不能直接当作 `/v1/videos` 模型调用。
这些额外条目的真实生成尚未验证，不能归为已确认不可用。
