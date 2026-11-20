GATE: OPEN 14 (rounds used up)

# 正式后台接入验收 · 2026-11-20

上面的门禁结果属于用户已确认的交互原型，不代表门禁通过。本次将该界面接入真实管理员应用；保留其管理工作台布局。原型门禁的开放项及豁免逐项列于文末。

## 实现结果

- 独立管理员路由 `/admin/desktop-models`，侧边栏入口位于渠道管理之后；正常模式和简单模式均可见，普通用户不显示该管理员入口。
- 新增受既有管理员认证保护的 `GET/PUT /api/v1/admin/settings/desktop`，沿用原有五个配置键和校验，不改表结构。
- 正式页面读取和保存真实配置；旧系统设置页面改为跳转入口，通用保存不再回传桌面配置。
- 表格、文本/生图页签、默认模型、搜索/筛选、编辑抽屉、真实分组导入、批量移除、临时凭据配置、桌面端选择预览均已实现。
- 保留 200 个模型上限、稳定目录 ID、人工 Agent 验证、路由变更后清除验证、图片质量档位、模型白名单和图片生成权限。
- 真实导入保留整个分组聚合、指定账号/协议、重复项处理、部分失败提示、错误重试和过期请求隔离。
- 保存错误显示后端原因并保留修改；支持放弃更改、离开确认及浏览器关闭提示。
- 共用 Select 在原生 dialog 内将下拉菜单放入最近的 dialog，保证导入选择不被原生 modal 的 inert 行为阻止。

## 已完成检查

- Vue/TypeScript 类型检查通过。
- 修改文件 ESLint 通过，`git diff --check` 通过。
- 116 项前端相关测试通过；最后的页面/API 13 项在最终调整后再次通过。
- 中英文 schema 完整性与静态引用检查通过。
- 后端目录专用保存、无效目录拒绝、存储故障、完整请求约束及无关配置保留测试通过。
- `go test -tags unit ./internal/server/routes -count=1` 通过。
- 最终 `pnpm run build` 通过。新增页面 JS 55.16 kB / gzip 16.87 kB，CSS 25.25 kB / gzip 5.57 kB；构建仍提示现有大 chunk、动态/静态混合导入及过期 Browserslist 数据，本次未调整依赖。
- 已人工查看实际生产组件在 1440、1024、768、390、320 像素宽度的截图，以及桌面/手机编辑抽屉。

## 本地视觉夹具与边界

视觉夹具使用实际 `DesktopModelsView`、`AppLayout`、侧边栏、编辑器和导入组件；API adapter 仅在忽略目录 `frontend/.taste-skill/implementation` 提供内存样例，不读写线上数据，也不进入正式构建。正式页面不包含模型样例、本地存储或失败时的虚假目录回退。

截图位于工作树的 `.taste-skill/shots/implementation-*`。原生固定抽屉以单屏 tile 检查，避免全页拼接截图省略固定元素导致误判。

当前没有提交、推送或部署。新页面的专用接口需要前端与后端一起发布；未对生产数据库执行保存验证。

## 已批准原型的门禁开放项和豁免

以下保持原报告结果。后台表格与侧边栏造成的营销页面屏高、hero、container 等规则不在本次接入阶段重新设计；按钮字号等开放项也未宣称已经通过。原有五条豁免的原因完整保留。

```text
  FAIL hero-screen      main#model-catalog > section.catalog-workspace.hero "模型与接入内置模型8管理 Aino 桌面端的模型目录、调用方式与默认选择。  [1440]
  FAIL section-height   main#model-catalog > section.catalog-workspace.hero "模型与接入内置模型8管理 Aino 桌面端的模型目录、调用方式与默认选择。  [1440]
  FAIL one-section      main#model-catalog > section.catalog-workspace.hero > div.catalog-wrap.wrap > div.connecti  [1440]
  FAIL one-heading      div.connection-band > div.connection-identity > div:nth-of-type(2) > h2 "Aino 桌面端 目录已启用"  [1440]
  FAIL container-edges  div#app > div.admin-shell > div.workspace-main > header.workspace-topbar "管理控制台内置模型交互预览"  [1440]
  FAIL container-edges  main#model-catalog > section.catalog-workspace.hero "模型与接入内置模型8管理 Aino 桌面端的模型目录、调用方式与默认选择。  [1440]
  FAIL container-edges  div#app "跳转到模型目录OmniFlow API万流 · 管理控制台OOmniFlow管理"  [1440]
  FAIL container-edges  main#model-catalog > section.catalog-workspace.hero > div.catalog-wrap.wrap "模型与接入内置模型8管理   [1440]
  FAIL container-edges  div.connection-band > div.connection-identity > div:nth-of-type(2) > h2 "Aino 桌面端 目录已启用"  [1440]
  FAIL container-edges  div.catalog-panel.app-frame > div.catalog-toolbar > div.filters > label.search-field "/"  [1440]
  FAIL button-styles    div.catalog-wrap.wrap > div.catalog-panel.app-frame > div.catalog-toolbar > button.btn.btn  [390]
  FAIL off-grid         section.catalog-workspace.hero > div.catalog-wrap.wrap > div.catalog-guidance > p "模型价格、用户  [390]
  FAIL edge-left        section.catalog-workspace.hero p "模型价格、用户倍率与使用权限沿用已有分组配置。文本模型需验证工具"  [1440, 390]
  FAIL hero-fold        section.catalog-workspace.hero section.catalog-workspace.hero "模型与接入内置模型8管理 Aino 桌面端的模型目录、  [1440, 390]
  WAIVED gaming-suspect frontend/prototypes/desktop-models/dist/assets/motion-DVdL0FVH.js: The unchanged skill motion.js shot() test is bundled by Vite. It checks HeadlessChrome to settle its own entrance for screenshots, exactly as documented in references/gate.md. Original source hashes match the kit.
  WAIVED no-visual main#model-catalog > section.catalog-workspace.hero "内置模型": This is the requested administrator application itself: the model table, filters and interactive editor are the product UI, not a marketing illustration.
  WAIVED copy-actions div.catalog-wrap.wrap > div.catalog-panel.app-frame > div.catalog-toolbar > butt: Administrators need Add, Import and Preview as distinct operations in this workspace. The requested application screen is not a two-CTA marketing hero.
  WAIVED motion-layer html[data-motion]: The original skill motion.css and motion.js are both loaded and bundled by Vite. The detector expects their unbundled filenames; source SHA-256 checks confirm both copied files are unchanged.
  WAIVED moment-none (the page): This is a data management application. The user requested working sidebar and editing interactions; a decorative scroll section would interfere with that task.
```
