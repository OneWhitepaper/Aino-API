# 内置模型管理交互预览

Kind: software administration interface.

本次范围是管理员界面原型：在侧边栏单独展示「内置模型」，将设置页里的长表单拆为可筛选模型列表、侧边编辑、分组导入和桌面端选择预览。使用现有 Vue 3 与 `DesktopSettings` / `DesktopModelEntry` 类型，独立入口不加载正式应用、不调用任何后端 API，不读取用户登录状态。

## 本地运行

在 `frontend` 目录运行：

```sh
pnpm exec vite --config prototypes/desktop-models/vite.config.ts
```

打开 `http://127.0.0.1:4176/`。修改保留在内存草稿；「保存配置」仅写入当前浏览器的 `omniflow:desktop-models-design:v1`，可通过右上角说明中的「恢复初始预览」恢复。模型和能力均为交互用的本地数据，不代表生产目录或上游能力。

可直接分享面板状态：`?panel=editor&model=deepseek-chat`、`?panel=import`、`?panel=desktop`、`?panel=connection`。这些入口仅打开相应面板，不产生配置更改。

## 设计与能力边界

- 侧栏将内置模型置于渠道管理后、系统设置前。其他管理页面仅展示导航位置。
- 文本、生图目录及默认模型独立。默认文本模型沿用已验证且支持工具调用的条件；生图固定使用 Images API。
- 列表展示显示名、实际模型 ID、关联分组、调用协议、模型能力和验证状态。行内编辑在右侧面板中完成。
- 关联分组、模型、平台或协议变化后重置验证；无效的默认值清空。移出目录保留上游账号和分组。
- 导入从本地候选目录选择并按分组、模型与协议去重。文本候选的工具能力及 Agent 验证默认为关闭。
- 模型条目没有单独的上下架开关：与现有数据结构一致，使用加入／移出目录管理。全局开关控制目录开放。
- 保留接入凭据 300–3600 秒有效期；这不是设备永久密钥的有效期。
- 原型不修改正式管理导航、路由、设置接口或计费。确认设计后，可用正式页面适配器接入同一数据类型。

## 视觉计划

原有 OmniFlow 青绿色通过技能的 `start.mjs brand` 生成中性色令牌，背景 `#f7f9fa`、文字 `#172b36`、主操作 `#087f75`，与现站品牌及截图的开关一致。软件字体抽签首选 Instrument Sans / DM Mono，字体保存在本地，中文使用设备中文字体。

该任务是完整后台管理界面，采用持久侧栏、紧凑表格、原生 modal dialog 及移动端菜单。营销首页的全屏 hero、随机页脚、滚动展示章节和效果场景不适用于本次界面，因此不加入。所有内容真实响应点击；微交互使用原样复制的技能 motion 运行时，并支持 reduced motion。无装饰性循环或 WebGL。

Voice: 技术型、直接；目录、分组、协议、验证状态保持现有词义；保存提示明确本地作用域。

## 素材

- OmniFlow 与 Aino 使用项目已有图标。
- DeepSeek、Claude、Gemini 使用项目已有的 `public/omniflow-home` 原色 SVG 及对应许可。
- OpenAI 图标来自项目已安装的 `@lobehub/icons`（MIT），原 SVG 路径未修改；商标归其权利人。
- Instrument Sans 和 DM Mono 来自 Google Fonts，随附 SIL OFL 许可证。
- `assets/motion` 为 `design-taste-frontend` 2.5.1 原始文件，未修改。
- 不使用照片、价格、客户评价、生产统计或真实账户数据。

验证结果记录在同目录 `validation.md`。
