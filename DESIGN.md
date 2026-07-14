## Project Understanding

### 业务理解

`w1ndys-bot-webui` 是 NapCat OneBot 11 QQ 机器人的单管理员控制台。产品核心价值不是承载聊天，而是把编译期插件能力与运行期配置安全地连接起来：管理员可以查看插件状态，调整启停与优先级，维护功能触发命令，配置全局或群级权限，并通过审计记录追踪变更。

产品属于机器人基础设施、即时通信自动化与轻量运维管理领域。目标用户主要是机器人部署者或维护者，具备基础技术能力，理解 QQ 群、插件、命令、权限与 Docker，但不应被要求理解数据库表或 OneBot 载荷。使用频率是“偶尔查看、集中配置、故障时高频排查”；核心诉求是配置正确、影响范围明确、操作可恢复、异常可定位。

主要场景包括：首次部署后检查插件；为插件功能增删群级命令；为群主、管理员或指定 QQ 配置权限；调整命令前缀；查看审计与故障状态。管理操作以桌面端为主，移动端主要用于紧急查看、启停和小范围修改，不采用移动优先设计。

### 技术理解

- 前端：Vue 3、TypeScript、Vue Router、Vite。
- UI 库：Naive UI，使用 `NConfigProvider` 主题覆盖与 CSS-in-JS。
- CSS：全局原生 CSS 与 CSS 变量；未使用 Tailwind CSS、CSS Modules 或 Sass。
- 网络：原生 Fetch，统一消费 `{ code, message, data }` REST 响应。
- 部署：Vite 产物由 Docker 多阶段构建，Go 与 WebSocket、REST API 共用端口并托管 SPA。
- 路由：登录页、插件总览、插件工作台嵌套路由、系统设置；审计页面后端能力已存在但前端待接入。

插件工作台是核心层级：插件 → 概览 / 所属功能 / 命令 / 权限。Manifest 功能元数据由代码定义，只读；命令、权限、启停、优先级和受控系统设置可修改。页面以表单、数据表格、状态标签、筛选器和确认对话框为主，不涉及拖拽、实时协作或富文本。

### 界面与情感理解

界面应传达“专业、稳定、克制、可追溯”，同时保留轻微暖棕品牌温度。它不是面向大众的内容产品，也不是高饱和游戏面板。参考气质应接近 Linear 的克制、GitHub Settings 的层级、Grafana 的运维信息密度与 Vercel Dashboard 的清晰反馈，但使用暖棕作为识别色。

## Design Decisions

| 决策点 | 选择 | 理由 |
| --- | --- | --- |
| 整体调性 | 现代极简 + 专业运维 | 配置错误会直接影响 QQ 群与插件运行，可信度优先于装饰性 |
| 色调方向 | 暖中性底色 + 克制曲奇棕 | 暖棕保留项目辨识度，中性色保证表格、日志和 JSON 的可读性 |
| 信息密度 | 中等偏紧凑 | 管理员需要快速对比多条命令和权限，但使用频率不适合极端密集 |
| 圆角风格 | 中小圆角，基础 8px / 0.5rem | 兼顾工具感与亲和力，避免大圆角造成“营销卡片”观感 |
| 阴影使用 | 克制，边框优先 | 数据密集页面中过多阴影会制造层级噪音 |
| 动效策略 | 功能性动效，120–220ms | 只用于反馈层级、抽屉和状态变化，不延迟高频操作 |
| 主要模式 | 仅亮色模式 | 单管理员项目优先降低设计、开发和测试成本；亮色更适合日常表格与配置阅读 |
| 导航模式 | 固定侧栏 + 插件二级菜单 + 工作台 Tabs | 与插件中心业务模型一致，减少跨插件误操作 |

## 1. Visual Theme & Atmosphere

### 设计哲学

每个页面都应先回答三个问题：当前在管理哪个插件或系统范围、修改会影响谁、操作是否已经生效。视觉设计服务于范围识别和风险控制。品牌色只用于导航激活、主操作和关键焦点；大面积背景、表格与内容容器使用低彩度中性色。禁止用渐变、大面积阴影或超大标题掩盖数据关系。

### 视觉关键词

- 专业可信
- 克制温暖
- 范围明确
- 数据优先
- 可追溯

### 参考方向

- GitHub Settings：设置分组、危险区域和说明文本。
- Linear：紧凑排版、轻边框和快速反馈。
- Grafana：筛选、状态、日志和数据表格。
- Vercel Dashboard：清晰层级、空状态和部署感知。

## 2. Color Palette & Roles

### 品牌与辅助色

| Token | HEX | 用途 |
| --- | --- | --- |
| `brand-500` | `#8B5E3C` | 主按钮、激活导航、焦点边框 |
| `brand-600` | `#70472D` | 按下态、深色品牌前景 |
| `brand-400` | `#A87550` | 悬停态、图表强调 |
| `brand-100` | `#F1E2D4` | 选中底色、轻提示背景 |
| `accent-500` | `#526D82` | 信息类辅助强调、链接图标 |

### 亮色中性色阶

| Token | HEX | 用途 |
| --- | --- | --- |
| `neutral-0` | `#FFFFFF` | 最高表面、输入框 |
| `neutral-25` | `#FCFAF7` | 卡片表面 |
| `neutral-50` | `#F7F4F0` | 页面背景 |
| `neutral-100` | `#EEE9E3` | 悬停背景 |
| `neutral-200` | `#DED7CF` | 常规边框 |
| `neutral-300` | `#C5BBB1` | 禁用边框 |
| `neutral-500` | `#7B7067` | 辅助文字 |
| `neutral-600` | `#625850` | 次级正文 |
| `neutral-800` | `#352F2B` | 主正文 |
| `neutral-950` | `#1D1A18` | 高强调标题 |

### 语义色

| 语义 | 主色 | 浅底色 | 深前景色 | 用途 |
| --- | --- | --- | --- | --- |
| Success | `#2F7D4A` | `#E8F5EC` | `#205C35` | 启用、连接正常、保存成功 |
| Warning | `#B06B16` | `#FFF3DD` | `#7D480D` | 覆盖值、待确认、部分可用 |
| Error | `#C43D3D` | `#FDEAEA` | `#8D2929` | 禁用失败、权限拒绝、危险操作 |
| Info | `#3F6F94` | `#EAF3FA` | `#294F6C` | 帮助、范围说明、只读信息 |

语义绝不只依赖颜色；必须同时使用文字、图标或状态标签。

### 表面层级

| 层级 | HEX | 用途 |
| --- | --- | --- |
| Canvas | `#F7F4F0` | 页面背景 |
| Surface | `#FCFAF7` | 侧栏、主内容 |
| Card | `#FFFFFF` | 卡片、表格 |
| Raised | `#FFFFFF` | 下拉、Popover、粘性表头 |
| Overlay | `#FFFFFF` | Modal、Drawer |

### CSS 变量命名

颜色变量按角色命名，不在业务组件中直接使用色阶：`--color-bg-canvas`、`--color-bg-surface`、`--color-bg-raised`、`--color-text-primary`、`--color-text-secondary`、`--color-text-muted`、`--color-border`、`--color-border-strong`、`--color-primary`、`--color-primary-hover`、`--color-primary-pressed`、`--color-primary-soft`、`--color-success`、`--color-warning`、`--color-error`、`--color-info`。

## 3. Typography Rules

### 字体族

```css
--font-sans: Inter, "Noto Sans SC", "Microsoft YaHei", "PingFang SC", system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
--font-mono: "JetBrains Mono", "SFMono-Regular", Consolas, "Liberation Mono", Menlo, monospace;
```

不强制远程字体；生产环境默认使用系统字体，避免外部资源与加载抖动。QQ、群号、插件键、功能键、命令标准化值、请求 ID 和 JSON 使用等宽字体。

### 字号层级

| Token | Size | Weight | Line Height | Letter Spacing | Use Case |
| --- | --- | --- | --- | --- | --- |
| `display` | 36px / 2.25rem | 700 | 44px / 2.75rem | -0.02em | 登录页主标题，管理页禁用 |
| `h1` | 28px / 1.75rem | 700 | 36px / 2.25rem | -0.015em | 页面标题 |
| `h2` | 22px / 1.375rem | 650 | 30px / 1.875rem | -0.01em | 区块标题 |
| `h3` | 18px / 1.125rem | 650 | 26px / 1.625rem | 0 | 卡片标题 |
| `h4` | 16px / 1rem | 650 | 24px / 1.5rem | 0 | 小节标题 |
| `h5` | 14px / 0.875rem | 650 | 22px / 1.375rem | 0 | 表单组标题 |
| `h6` | 12px / 0.75rem | 700 | 18px / 1.125rem | 0.04em | 导航分组、表头 |
| `body-lg` | 16px / 1rem | 400 | 26px / 1.625rem | 0 | 重要说明 |
| `body` | 14px / 0.875rem | 400 | 22px / 1.375rem | 0 | 默认正文与控件 |
| `body-sm` | 13px / 0.8125rem | 400 | 20px / 1.25rem | 0 | 表格、次级说明 |
| `caption` | 12px / 0.75rem | 400 | 18px / 1.125rem | 0.01em | 时间、版本、辅助信息 |

段落最大宽度为 680px / 42.5rem；帮助文本最大宽度为 560px / 35rem。中文正文行高保持字号的 1.55–1.7 倍；标题禁止全大写中文；中英文之间保留自然空格；不要用增加字距模拟中文高级感。单行标签禁止断字，长插件键允许 `overflow-wrap: anywhere`。

## 4. Component Stylings

### Button

| 变体 | 默认 | Hover | Active | Focus-visible | Disabled | Loading |
| --- | --- | --- | --- | --- | --- | --- |
| Primary | 品牌底、白字 | `brand-400` | `brand-600` | 3px / 0.1875rem 品牌浅色焦点环 | 40% 不透明度 | 保持宽度，显示 Spinner |
| Secondary | 白底、品牌边框 | `brand-100` 底 | 边框加深 | 3px / 0.1875rem 品牌浅色焦点环 | 中性禁用态 | 禁止重复点击 |
| Ghost | 透明底、次级文字 | `neutral-100` | `neutral-200` | 2px / 0.125rem 品牌焦点轮廓 | 无 Hover | 可用于工具栏 |
| Danger | Error 底、白字 | 深 Error | 再深一级 | 3px / 0.1875rem Error 浅色焦点环 | 40% 不透明度 | 危险请求期间锁定 |
| Link | 无边框、品牌文字 | 下划线 | 深品牌色 | 2px / 0.125rem 品牌焦点轮廓 | 中性文字 | 不用于主提交 |

| Size | Height | Horizontal Padding | Font |
| --- | --- | --- | --- |
| sm | 28px / 1.75rem | 10px / 0.625rem | 12px / 0.75rem |
| md | 36px / 2.25rem | 14px / 0.875rem | 14px / 0.875rem |
| lg | 44px / 2.75rem | 18px / 1.125rem | 15px / 0.9375rem |

同一区域只允许一个 Primary。删除、禁用系统能力等危险操作必须使用 Danger，并经过确认对话框。

### Input / TextArea / Select

| 状态 | 规范 |
| --- | --- |
| 默认 | 36px / 2.25rem 高；1px / 0.0625rem 中性边框；白色表面 |
| 聚焦 | 品牌边框；0 0 0 3px / 0.1875rem `brand-100` 焦点环 |
| 错误 | Error 边框；下方 12px / 0.75rem 错误文案，不仅改变颜色 |
| 禁用 | `neutral-100` 背景；`neutral-500` 文字；禁止 Hover |
| 只读 | 无焦点环；保留可复制文本；与禁用态视觉不同 |

TextArea 默认最小高度 96px / 6rem，允许垂直缩放。Select 下拉项高度 36px / 2.25rem。QQ、群号与优先级输入必须显示范围或格式帮助；禁止依赖 placeholder 充当标签。

### Card

| 类型 | Border | Shadow | Padding | Hover |
| --- | --- | --- | --- | --- |
| 基础内容卡 | 1px / 0.0625rem | 无 | 20px / 1.25rem | 无位移 |
| 可点击插件卡 | 1px / 0.0625rem | Elevation 0 | 20px / 1.25rem | 边框变品牌色 |
| 关键摘要卡 | 1px / 0.0625rem | Elevation 1 | 24px / 1.5rem | 无位移 |
| 危险区域 | Error 浅边框 | 无 | 20px / 1.25rem | 无 |

卡片圆角 8px / 0.5rem；禁止 16px / 1rem 以上大圆角。卡片不嵌套卡片，优先使用分隔线和区块标题。

### Table

| 项目 | 规范 |
| --- | --- |
| Header | 36px / 2.25rem 高，12px / 0.75rem，`neutral-50` 背景，必要时粘性 |
| Row | 紧凑 40px / 2.5rem；常规 48px / 3rem |
| Zebra | 默认不使用；超过 12 行且列多时使用 `neutral-25` |
| Hover | `neutral-50`，不改变布局 |
| Selection | `brand-100`，同时显示选择控件 |
| Empty | 表格容器内 160px / 10rem 高空状态 |
| Actions | 固定右侧，最多 3 个直接操作，其余进入菜单 |

命令、权限和审计使用表格而非卡片列表。表头保持简短，完整解释放 Tooltip。横向滚动时固定目标列与操作列。

### Navigation

| 部位 | 规范 |
| --- | --- |
| 顶栏 | 56px / 3.5rem 高，仅包含品牌、连接状态、账户操作 |
| 侧栏 | 240px / 15rem；折叠后 64px / 4rem；一级系统模块 + 插件二级项 |
| 激活项 | 品牌文字 + `brand-100` 背景 + 左侧 3px / 0.1875rem 指示条 |
| Hover | `neutral-100` 背景，不使用阴影 |
| 面包屑 | 13px / 0.8125rem；用于“插件 / ping / 权限”范围确认 |
| Tabs | 插件详情内部使用，不与侧栏竞争全局导航 |

### Modal / Drawer

| 组件 | 尺寸与行为 |
| --- | --- |
| Confirm Modal | 420px / 26.25rem；危险操作说明影响范围和不可逆性 |
| Form Modal | 560px / 35rem；超过 8 个字段改用独立页面或 Drawer |
| Detail Drawer | 640px / 40rem；审计详情、JSON 前后对比 |
| Mobile Drawer | 85vw，最大 320px / 20rem；承载侧栏导航 |
| Overlay | `#1D1A18` 40% 不透明度 |
| Animation | 180ms；淡入 + 8px / 0.5rem 位移，遵循 reduced-motion |

### Toast / Alert / Badge

| 组件 | 规范 |
| --- | --- |
| Success Toast | 右上角；3 秒；说明“已保存并热更新” |
| Error Toast | 右上角；6 秒或手动关闭；保留可复制请求 ID |
| Warning Alert | 页面内容顶部；用于部分失败、默认覆盖、敏感影响 |
| Inline Alert | 紧邻表单；不得用 Toast 替代字段错误 |
| Badge | 仅用于数量和连接状态；不用于长文本 |

### Tag / Chip

| 变体 | 用途 |
| --- | --- |
| Success | 已启用、可用、成功 |
| Neutral | 默认值、只读、未覆盖 |
| Warning | 数据库覆盖、部分可用、待确认 |
| Error | 已停用、失败、拒绝 |
| Info | 全局、群级、角色、用户范围 |

Tag 高度 24px / 1.5rem，圆角 4px / 0.25rem。可关闭 Chip 仅用于筛选条件，不用于持久化状态。

### Tabs / Pagination / States

| Tabs 状态 | 规范 |
| --- | --- |
| Default | 40px / 2.5rem 高；次级文字；无背景块 |
| Hover | 主文字色；浅中性背景，不移动布局 |
| Active | 品牌文字 + 2px / 0.125rem 品牌指示线；与 URL 同步 |
| Focus-visible | 2px / 0.125rem 品牌轮廓，轮廓偏移 2px / 0.125rem |
| Disabled | 40% 不透明度；不可聚焦、不可点击 |
| Overflow | 单行横向滚动；保留当前 Tab 可见；禁止换行 |

| Pagination 状态 | 规范 |
| --- | --- |
| Default | 32px / 2rem 控件；右下对齐；显示总数和每页数量 |
| Hover | 品牌浅底；文字保持高对比度 |
| Current | 品牌底、白字，并设置 `aria-current="page"` |
| Focus-visible | 2px / 0.125rem 品牌轮廓 |
| Disabled | 首尾边界按钮禁用；不响应键盘激活 |
| Loading | 保留当前页数据和分页尺寸，禁用翻页并显示局部进度 |
| Overflow | 使用省略项；始终保留首页、末页、当前页邻近项 |

| Badge / 状态组件 | 规范 |
| --- | --- |
| Count Badge | 最小 18px / 1.125rem 高；最大显示 `99+`；必须有可读标签 |
| Status Badge | 颜色 + 状态文字；连接态可有圆点但不可只显示圆点 |
| Hover | 仅可交互 Badge 显示浅底；纯状态 Badge 无 Hover |
| Focus-visible | 可交互 Badge 使用 2px / 0.125rem 焦点轮廓 |
| Disabled | 降低对比度并移除关闭按钮 |

| Empty / Loading / Error | 规范 |
| --- | --- |
| Empty State | 简短标题 + 原因 + 一个推荐动作；不使用大插画 |
| Page Loading | 首次加载使用 Skeleton；局部保存只锁定相关行或按钮 |
| Error State | 保留已有数据；显示错误和重试，不清空成“无数据” |

## 5. Layout Principles

基础间距单位为 4px / 0.25rem。

| Token | Value | 用途 |
| --- | --- | --- |
| `space-0` | 0px / 0rem | 无间距 |
| `space-1` | 4px / 0.25rem | 图标微调 |
| `space-2` | 8px / 0.5rem | 紧凑控件间距 |
| `space-3` | 12px / 0.75rem | 标签与输入 |
| `space-4` | 16px / 1rem | 默认组件间距 |
| `space-5` | 20px / 1.25rem | 卡片内边距 |
| `space-6` | 24px / 1.5rem | 区块间距 |
| `space-8` | 32px / 2rem | 页面小节 |
| `space-10` | 40px / 2.5rem | 页面顶部区域 |
| `space-12` | 48px / 3rem | 大区块分隔 |
| `space-16` | 64px / 4rem | 登录页与空状态 |

页面布局：顶栏 56px / 3.5rem；桌面侧栏 240px / 15rem；主内容最大宽度 1280px / 80rem；内容区左右边距桌面 32px / 2rem、平板 24px / 1.5rem、手机 16px / 1rem。页面标题区与内容间隔 24px / 1.5rem。数据页面优先占满可用宽度，不为追求留白把表格压窄。

采用 12 栏网格，栏间距 24px / 1.5rem。表单详情最大 8 栏，辅助说明 4 栏；插件摘要卡桌面 3–4 栏、平板 6 栏、手机 12 栏。禁止在同一视口并排超过两张复杂表单卡。

## 6. Depth & Elevation

| Level | CSS box-shadow | 使用场景 |
| --- | --- | --- |
| 0 | `none` | 默认卡片、表格、侧栏 |
| 1 | `0 1px 2px rgba(29, 26, 24, 0.06)` | 粘性顶栏、摘要卡 |
| 2 | `0 4px 12px rgba(29, 26, 24, 0.10)` | Dropdown、Popover |
| 3 | `0 12px 32px rgba(29, 26, 24, 0.14)` | Drawer、Modal |
| 4 | `0 20px 48px rgba(29, 26, 24, 0.18)` | 仅全局阻断层 |

阴影位移对应：1px / 0.0625rem、2px / 0.125rem、4px / 0.25rem、12px / 0.75rem、20px / 1.25rem；模糊对应 2px / 0.125rem、12px / 0.75rem、32px / 2rem、48px / 3rem。

| z-index Token | Value | 场景 |
| --- | --- | --- |
| `z-base` | 0 | 页面内容 |
| `z-sticky` | 100 | 粘性表头、顶栏 |
| `z-dropdown` | 1000 | Select、Popover |
| `z-drawer` | 1200 | Drawer |
| `z-modal` | 1300 | Modal |
| `z-toast` | 1400 | Message、Toast |

## 7. Design Do's and Don'ts

### Do

1. 始终在页面标题或面包屑显示当前插件与功能范围。
2. 保存成功明确说明是否已经热更新，不只显示“成功”。
3. 删除命令或权限前展示作用域、插件、功能和主体。
4. 使用表格呈现可比较的命令、权限和审计记录。
5. 所有错误保留请求 ID，并提供重试或恢复路径。
6. 默认值、数据库覆盖值和 Manifest 只读值必须视觉区分。
7. 时间从 UTC 转换为本地时区，并在 Tooltip 标注原始 UTC。

### Don't

1. 不要使用大面积曲奇棕背景或棕色渐变。
2. 不要在管理页使用超过 32px / 2rem 的标题。
3. 不要用卡片代替所有表格，尤其是命令、权限和审计。
4. 不要把插件启停与优先级伪装成一次原子保存。
5. 不要允许跨插件页面显示或编辑其他插件的数据。
6. 不要仅靠红绿颜色表达状态。
7. 不要在未加载功能 Manifest 时默认提交“插件全部功能”。
8. 不要把系统密钥、管理员密码或 Token 放入 WebUI 设置。
9. 不要对只读 Manifest 功能提供虚假的新增、编辑或删除按钮。
10. 不要用 Toast 替代字段校验、部分提交或持久化不确定提示。
11. 不要使用超过两层 Card 嵌套或多重重阴影。
12. 不要在移动端强行展示完整宽表；改为关键列 + 详情 Drawer。

### 项目特有护栏

- `admin` 系统插件不可禁用，控件应禁用并解释原因。
- 权限优先级必须提供可查看的“解析顺序”帮助，不允许只展示孤立规则。
- 空 `feature_key` 表示插件全部功能，必须显示为明确文案，不能显示空白。
- 群级规则必须展示群号；全局规则使用“全局”，不显示内部 `scope_id=0`。
- Manifest 默认权限是最终回退，不应被伪装成数据库规则。
- 审计前后 JSON 使用并排或差异视图，敏感字段必须脱敏。

## 8. Responsive Behavior

| Breakpoint | Range | 行为 |
| --- | --- | --- |
| Mobile | 0–639px / 0–39.9375rem | 侧栏转 Drawer；单列表单；表格转关键列；底部操作可粘性 |
| Tablet | 640–1023px / 40–63.9375rem | 侧栏可折叠；2 栏摘要；表格允许横向滚动 |
| Desktop | 1024–1439px / 64–89.9375rem | 固定 240px / 15rem 侧栏；完整工作台 |
| Wide | ≥1440px / 90rem | 内容最大 1280px / 80rem；不无限拉伸表单 |

触摸目标最小 44px × 44px / 2.75rem × 2.75rem；紧凑桌面按钮可为 36px / 2.25rem，但移动端必须提升。侧栏在 Mobile 使用左侧 Drawer，在 Tablet 可折叠为 64px / 4rem 图标栏，在 Desktop 固定展开。插件工作台 Tabs 在 Mobile 横向滚动，不换成多行。Modal 在 Mobile 使用全宽 Drawer 或距边缘 16px / 1rem 的全屏容器。

支持 `prefers-reduced-motion: reduce`：关闭位移动画，将过渡压缩至 0–80ms。支持键盘完整操作、可见焦点环和 Escape 关闭浮层。

## 9. Agent Prompt Guide

### 核心色彩速查

```text
Primary: #8B5E3C
Primary Hover: #A87550
Primary Pressed: #70472D
Primary Soft: #F1E2D4
Canvas: #F7F4F0
Surface: #FCFAF7
Card: #FFFFFF
Text Primary: #352F2B
Text Secondary: #625850
Text Muted: #7B7067
Border: #DED7CF
Success: #2F7D4A
Warning: #B06B16
Error: #C43D3D
Info: #3F6F94
```

### CSS 变量完整列表

```css
:root {
  --color-bg-canvas: #F7F4F0;
  --color-bg-surface: #FCFAF7;
  --color-bg-card: #FFFFFF;
  --color-bg-raised: #FFFFFF;
  --color-bg-overlay: #1D1A18;
  --color-text-primary: #352F2B;
  --color-text-secondary: #625850;
  --color-text-muted: #7B7067;
  --color-border: #DED7CF;
  --color-border-strong: #C5BBB1;
  --color-primary: #8B5E3C;
  --color-primary-hover: #A87550;
  --color-primary-pressed: #70472D;
  --color-primary-soft: #F1E2D4;
  --color-accent: #526D82;
  --color-success: #2F7D4A;
  --color-success-soft: #E8F5EC;
  --color-success-foreground: #205C35;
  --color-warning: #B06B16;
  --color-warning-soft: #FFF3DD;
  --color-warning-foreground: #7D480D;
  --color-error: #C43D3D;
  --color-error-soft: #FDEAEA;
  --color-error-foreground: #8D2929;
  --color-info: #3F6F94;
  --color-info-soft: #EAF3FA;
  --color-info-foreground: #294F6C;
  --font-sans: Inter, "Noto Sans SC", "Microsoft YaHei", "PingFang SC", system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  --font-mono: "JetBrains Mono", "SFMono-Regular", Consolas, "Liberation Mono", Menlo, monospace;
  --font-size-caption: 0.75rem; /* 12px / 0.75rem */
  --font-size-body-sm: 0.8125rem; /* 13px / 0.8125rem */
  --font-size-body: 0.875rem; /* 14px / 0.875rem */
  --font-size-body-lg: 1rem; /* 16px / 1rem */
  --font-size-h6: 0.75rem; /* 12px / 0.75rem */
  --font-size-h5: 0.875rem; /* 14px / 0.875rem */
  --font-size-h4: 1rem; /* 16px / 1rem */
  --font-size-h3: 1.125rem; /* 18px / 1.125rem */
  --font-size-h2: 1.375rem; /* 22px / 1.375rem */
  --font-size-h1: 1.75rem; /* 28px / 1.75rem */
  --font-size-display: 2.25rem; /* 36px / 2.25rem */
  --font-weight-regular: 400;
  --font-weight-semibold: 650;
  --font-weight-bold: 700;
  --line-height-caption: 1.125rem; /* 18px / 1.125rem */
  --line-height-body-sm: 1.25rem; /* 20px / 1.25rem */
  --line-height-body: 1.375rem; /* 22px / 1.375rem */
  --line-height-body-lg: 1.625rem; /* 26px / 1.625rem */
  --line-height-h4: 1.5rem; /* 24px / 1.5rem */
  --line-height-h3: 1.625rem; /* 26px / 1.625rem */
  --line-height-h2: 1.875rem; /* 30px / 1.875rem */
  --line-height-h1: 2.25rem; /* 36px / 2.25rem */
  --line-height-display: 2.75rem; /* 44px / 2.75rem */
  --letter-spacing-tight: -0.015em;
  --letter-spacing-normal: 0em;
  --letter-spacing-label: 0.04em;
  --radius-sm: 0.25rem; /* 4px / 0.25rem */
  --radius-md: 0.5rem; /* 8px / 0.5rem */
  --radius-lg: 0.75rem; /* 12px / 0.75rem */
  --space-0: 0rem; /* 0px / 0rem */
  --space-1: 0.25rem; /* 4px / 0.25rem */
  --space-2: 0.5rem; /* 8px / 0.5rem */
  --space-3: 0.75rem; /* 12px / 0.75rem */
  --space-4: 1rem; /* 16px / 1rem */
  --space-5: 1.25rem; /* 20px / 1.25rem */
  --space-6: 1.5rem; /* 24px / 1.5rem */
  --space-8: 2rem; /* 32px / 2rem */
  --space-10: 2.5rem; /* 40px / 2.5rem */
  --space-12: 3rem; /* 48px / 3rem */
  --space-16: 4rem; /* 64px / 4rem */
  --shadow-0: none;
  --shadow-1: 0 0.0625rem 0.125rem rgba(29, 26, 24, 0.06); /* 0 1px 2px */
  --shadow-2: 0 0.25rem 0.75rem rgba(29, 26, 24, 0.10); /* 0 4px 12px */
  --shadow-3: 0 0.75rem 2rem rgba(29, 26, 24, 0.14); /* 0 12px 32px */
  --shadow-4: 0 1.25rem 3rem rgba(29, 26, 24, 0.18); /* 0 20px 48px */
  --z-base: 0;
  --z-sticky: 100;
  --z-dropdown: 1000;
  --z-drawer: 1200;
  --z-modal: 1300;
  --z-toast: 1400;
}
```

项目仅维护亮色模式。不得新增暗色切换入口、暗色专属变量或按系统主题自动切换逻辑；如未来确有需求，应作为独立设计与验收项目重新评估。

### Tailwind Token 映射

项目未使用 Tailwind CSS。不要为了单个页面引入 Tailwind；优先使用 Naive UI Theme Overrides 与上述 CSS 角色变量。若未来迁移 Tailwind，映射 `primary` → `#8B5E3C`、`surface` → `#FCFAF7`、`canvas` → `#F7F4F0`、`border` → `#DED7CF`、`danger` → `#C43D3D`。

### AI 快速提示词模板

```text
为 w1ndys-bot-webui 实现【页面/组件】。
技术栈：Vue 3 + TypeScript + Naive UI + 原生 CSS 变量，不引入新 UI/CSS 框架。
遵循 DESIGN.md：专业运维控制台、亮色暖中性、曲奇棕仅作强调、中等偏紧凑、边框优先于阴影。
明确显示当前插件/功能/全局或群级范围；写操作展示 loading、成功热更新反馈和可恢复错误；危险操作必须确认。
命令、权限、审计优先使用表格；Manifest 功能只读；禁止跨插件数据混用。
同时实现 Mobile 0–639px、Tablet 640–1023px、Desktop ≥1024px 响应式，并满足键盘、焦点、触摸目标和 reduced-motion 要求。
复用 Naive UI Theme Overrides 与 DESIGN.md CSS 变量，禁止硬编码新色值、超大标题、厚重阴影和装饰性渐变。
```
