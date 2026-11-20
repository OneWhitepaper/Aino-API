GATE: OPEN 14 (rounds used up)

# 内置模型管理交互预览验证记录

日期：2026-11-20。分支：`codex/admin-built-in-models-preview`。基线：`e571717db62afad9c124b2e57870c8240386edbc`。

## 已完成验证

- 原型使用本项目 Vue 3 和现有 DesktopSettings 类型，独立 Vite 入口。无生产后台 API、无真实模型调用、无真实账户登录。
- TypeScript：`vue-tsc --noEmit -p prototypes/desktop-models/tsconfig.json` 通过。
- Lint：原型 Vue / TS 文件检查通过；`git diff --check` 通过。
- Vitest：8 项组件交互测试通过。覆盖文本／生图与默认值独立、搜索空结果、编辑草稿后显式本地保存、变更路由重置验证和默认值、导入去重且新模型待验证、移除默认模型、表单校验和未保存提示、全局目录开关及桌面预览选择不改变管理员默认值。测试禁止 fetch，确认操作不发出后端请求。
- 原型生产构建通过。主入口 JS 约 138 kB，motion 约 11 kB（压缩前文件大小）；本地字体约 65 kB。
- 最终生产产物性能检查：1440 与 390 两种宽度均完成，`0 FAIL, 0 WARN -> PASS`。未发现失败请求、长任务或布局偏移。该结果为本机 CPU 浏览器实验，不代表公网网络性能。
- 使用技能自带 shoot 工具观察了 1440、1024、820、768、390 宽度的实际渲染，并查看桌面和手机编辑抽屉的单屏 tiles。拼接整页截图会省略第二屏的 fixed 对话框，因此抽屉以单屏 tile 为准。
- 第一轮布局脚本在没有 `.wrap` 时无法输出完整报告；随后加入管理页内容容器，最终构建完成三轮 gate，状态为 OPEN，未宣称 PASS。

## 最终检查器状态

使用用户指定技能的三轮检查，保留 14 个未通过项。多数来自营销页面规则与后台工作台的结构差异；按钮字号检查仍有一项，需要正式接入时继续核对。后续正式迁移不能把本记录视为所有设计检查通过。完整输出见同目录 `validation-gate.txt`。

### 每项保留的 FAIL

1. **hero-screen** — `main#model-catalog > section.catalog-workspace.hero "模型与接入内置模型8管理 Aino 桌面端的模型目录、调用方式与默认选择。预览桌"`
   the hero ends at 1250px on a 900px screen, 350px below the fold: it ends at the fold, 1px either way (SR-1)

2. **section-height** — `main#model-catalog > section.catalog-workspace.hero "模型与接入内置模型8管理 Aino 桌面端的模型目录、调用方式与默认选择。预览桌"`
   the section starts at 104px, 104px below the screen grid (0 x 900px): sections start on whole screens (SR-2)

3. **one-section** — `main#model-catalog > section.catalog-workspace.hero > div.catalog-wrap.wrap > div.connection-band "Aino 桌面端 目录已启用文本与生图分别管理，使用已有分组和计费规则。接入设置"`
   a band: a box 100% of the section wide on a ground of its own that holds the heading or the intro: the section's own ground is the band (SR-4)

4. **one-heading** — `div.connection-band > div.connection-identity > div:nth-of-type(2) > h2 "Aino 桌面端 目录已启用"`
   the hero holds an h2 (1): its heading is the h1, and an h2 opens the next section (SR-5)

5. **container-edges** — `div#app > div.admin-shell > div.workspace-main > header.workspace-topbar "管理控制台内置模型交互预览"`
   the nav's content runs 264px from the left and 40px from the right (read from its outermost blocks: no .wrap): 264 and 40px are not one margin; the margin is 64-120px at 1440px (one container, equal margins, 2px either way) (SR-6)

6. **container-edges** — `main#model-catalog > section.catalog-workspace.hero "模型与接入内置模型8管理 Aino 桌面端的模型目录、调用方式与默认选择。预览桌"`
   the content of section 1 runs 264px from the left and 40px from the right: 264 and 40px are not one margin; the margin is 64-120px at 1440px (one container, equal margins, 2px either way) (SR-6)

7. **container-edges** — `div#app "跳转到模型目录OmniFlow API万流 · 管理控制台OOmniFlow管理"`
   a <div> at the top of the page, outside header.nav, main, footer.closing and nav.actionbar (SR-6)

8. **container-edges** — `main#model-catalog > section.catalog-workspace.hero > div.catalog-wrap.wrap "模型与接入内置模型8管理 Aino 桌面端的模型目录、调用方式与默认选择。预览桌"`
   the hero's .wrap holds 5 children, 0 of them .hero-text: one .hero-text and at most one strip (SR-6)

9. **container-edges** — `div.connection-band > div.connection-identity > div:nth-of-type(2) > h2 "Aino 桌面端 目录已启用"`
   2 text blocks start on a left line at x 349, 85px from the container's left: text starts on the container's left, or on one split line (x 747, 844, 941) (also "文本与生图分别管理，使用已有分组和计费规则。") (SR-6)

10. **container-edges** — `div.catalog-panel.app-frame > div.catalog-toolbar > div.filters > label.search-field "/"`
   1 text block starts on a left line at x 300, 36px from the container's left: text starts on the container's left, or on one split line (x 747, 844, 941) (and 2 more like it: x 294, x 289) (SR-6)

11. **button-styles** — `div.catalog-wrap.wrap > div.catalog-panel.app-frame > div.catalog-toolbar > button.btn.btn-secondary "从分组导入"`
   one button style (soft, outline #d4dde1) with labels in 2 sizes: 13, 16px ("从分组导入", "预览桌面端")

12. **off-grid** — `section.catalog-workspace.hero > div.catalog-wrap.wrap > div.catalog-guidance > p "模型价格、用户倍率与使用权限沿用已有分组配置。 文本模型需验证工具调用与流式回复"`
   left edge at 38px, 22px off a common edge of its section at 16px (7 elements, like span:nth-of-type(1)); 1 more in this section off the grid too (span at 38px)

13. **edge-left** — `section.catalog-workspace.hero p "模型价格、用户倍率与使用权限沿用已有分组配置。文本模型需验证工具"`
   starts at x 294; the lines are 264 and the column starts

14. **hero-fold** — `section.catalog-workspace.hero section.catalog-workspace.hero "模型与接入内置模型8管理 Aino 桌面端的模型目录、调用方式与"`
   starts at y 104, not under the nav

### 五项显式 waiver 及理由

- **gaming-suspect** — `frontend/prototypes/desktop-models/dist/assets/motion-DVdL0FVH.js`
  The unchanged skill motion.js shot() test is bundled by Vite. It checks HeadlessChrome to settle its own entrance for screenshots, exactly as documented in references/gate.md. Original source hashes match the kit.

- **motion-layer** — `html[data-motion]`
  The original skill motion.css and motion.js are both loaded and bundled by Vite. The detector expects their unbundled filenames; source SHA-256 checks confirm both copied files are unchanged.

- **no-visual** — `main#model-catalog > section.catalog-workspace.hero "内置模型"`
  This is the requested administrator application itself: the model table, filters and interactive editor are the product UI, not a marketing illustration.

- **copy-actions** — `div.catalog-wrap.wrap > div.catalog-panel.app-frame > div.catalog-toolbar > button.btn.btn-secondary "从分组导入"`
  Administrators need Add, Import and Preview as distinct operations in this workspace. The requested application screen is not a two-CTA marketing hero.

- **moment-none** — `(the page)`
  This is a data management application. The user requested working sidebar and editing interactions; a decorative scroll section would interfere with that task.

## 保留的 WARN 与检查边界

最终 gate 保留 6 个 WARN：表格跨首屏折线、持久侧栏 sticky、已知管理员页面标题不包含营销买方描述、添加按钮的营销式动作位置、Vue 根节点内的首个跳转链接等。详见完整输出。保留的理由是工作台使用滚动表格、持久导航和后台任务标题；实际跳转链接仍位于 Tab 顺序首位。最终输出不包含 copy-lint FAIL；无自创品牌、价格、统计、评价、照片或联系地址。

## 设计记录

Kind: software administration interface。白色内容面板、浅灰地面与已有 OmniFlow 青绿色。字体抽签首选 Instrument Sans / DM Mono；使用 brand 命令生成的颜色与字体令牌。后台应用采用管理员侧栏、模型表格、原生 dialog 编辑面板，未加入营销 hero 的大字、页脚、照片、WebGL、自动循环或滚动展示章节。

Voice: 技术型、直接；使用现有目录、分组、协议、验证等术语。每个保存提示明确为当前浏览器的本地配置。

图片：仅项目已有 OmniFlow / Aino 图标及原有供应商标志；OpenAI 原路径来自已安装的 @lobehub/icons。无照片替身，无待补客户素材，无表单外发地址，也无正式页面 canonical（独立本地原型）。本地示例记录只用于操作演示，不代表最新模型、价格或可用能力。

原始技能 motion 文件 SHA-256：

- `motion.css`: `948bfb7192c908934206ef5776ac28bdd8c3499483d7347cbb24e7b6fb5c6810`，与技能原文件一致。
- `motion.js`: `e4453f1ac91d81002c5a75dccc1263285267999a8bb2453278ab63232e7062cc`，与技能原文件一致。

## 复查命令

在 frontend 目录运行：

```sh
pnpm exec vue-tsc --noEmit -p prototypes/desktop-models/tsconfig.json
pnpm exec vitest run --config prototypes/desktop-models/vitest.config.ts
pnpm exec eslint prototypes/desktop-models --ext .vue,.ts --quiet
pnpm exec vite build --config prototypes/desktop-models/vite.config.ts
```

后台正式导航、正式路由、接口保存与线上部署均未在本次预览中修改；它们是后续落地工作。
