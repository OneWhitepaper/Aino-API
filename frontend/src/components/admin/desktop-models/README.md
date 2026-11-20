# 管理员内置模型工作台

正式页面为 `/admin/desktop-models`，复用 `AppLayout` 和现有管理员路由校验。
系统设置的旧位置改为跳转入口，其保存请求不再发送 `desktop`，避免覆盖独立页面的更新。

## 数据与保存

- `GET /api/v1/admin/settings/desktop` 读取真实目录。
- `PUT /api/v1/admin/settings/desktop` 整体替换目录，只写入原有的五个 `desktop.*` 设置键。
- 请求必须提供 `enabled`、`models`、`credential_ttl_seconds`；默认模型 ID 可以为 `null`。
- 服务器沿用原有目录、分组、模型白名单、图片权限、默认模型和凭据有效期校验，并记录设置审计。
- 页面先维护内存草稿，点击“保存配置”才写入服务器。失败保留草稿；离开页面或关闭浏览器时提示未保存修改。
- 正式代码没有模型样例、离线回退或本地持久化。目录关闭、空目录、读取失败是不同状态。

## 兼容规则

既有目录 ID 在编辑时保持固定，避免破坏桌面端会话引用。文本与生图分别设置默认值；文本默认值要求工具调用能力及人工确认的 Agent 验证。更换实际模型、分组、平台或协议会重置验证状态；移除模型或取消其默认资格时清除相应默认值。

生图固定使用 Images API，保留原有质量档位和分组图片生成权限。目录成员身份控制模型发布，全局开关控制整个目录；数据协议不增加单模型启用开关。凭据有效期仍为 300–3600 秒，只影响临时凭据。

`DesktopModelImporter` 保留真实分组聚合、账号来源选择、协议选择、白名单、去重、容量限制、部分失败及取消过期请求的逻辑。导入来源不绑定实际推理账号。价格、用户权限、订阅和扣费策略继续由既有服务决定。

`CatalogDialog` 使用原生 modal dialog；共用 `Select` 在 dialog 内打开时将下拉菜单放在最近的 dialog 内，保证搜索、键盘和点击不被原生 modal 的 inert 层拦截。

## 验证

新增页面和 API 测试位于 `src/views/admin/__tests__/DesktopModelsView.spec.ts`、`src/api/__tests__/admin.desktopSettings.spec.ts`。
后端用例位于 `internal/service/setting_desktop_admin_test.go`、`internal/handler/admin/setting_handler_desktop_test.go`。
本地视觉验收夹具隔离于忽略目录 `.taste-skill`，不进入正式构建，也不访问线上设置。
