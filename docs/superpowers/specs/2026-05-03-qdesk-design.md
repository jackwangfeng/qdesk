# qdesk 设计文档

**日期**:2026-05-03
**状态**:设计阶段(brainstorming 完成,待用户复审)
**作者**:jeff (with Claude)

---

## 1. 项目定位

**一句话:** AI-native 应用测试平台 — 用自然语言描述测试,AI agent 在云沙箱里执行,UI 变化时自愈,长期演进为通用 agent sandbox 基础设施。

**完整定位:**
- **短期 (0-6 月):** "AI-native testing platform" — 替代 Playwright/Cypress,主打 canvas / 桌面应用测试
- **长期 (6-24 月):** "Agent sandbox infrastructure" — 给 AI agent 提供桌面执行环境的通用基础设施
- **GTM 路径:** 测试做 wedge → 客户用着发现也能跑生产任务 → 自然演进到通用 sandbox

## 2. 为什么选这个方向

### 用户痛点

传统测试工具(Playwright / Cypress / Selenium / Appium)的核心问题:
1. **Selector 地狱** — 测试本质是脚本,DOM 微调即崩
2. **Canvas 无能** — 现代设计 / 协作 / 可视化工具大量使用 canvas,完全黑盒
3. **维护成本接近重写** — UI 演进时测试脚本要重写
4. **写测试 = boilerplate 体力活** — 开发者讨厌
5. **失败诊断靠人** — log 一坨,定位耗时
6. **不懂语义** — 不能说"点保存按钮",必须说 selector
7. **Flaky** — 时序问题导致测试不稳

### 市场机会

- **AI-native 测试**没有清晰领头羊(Octomind / mabl 偏 DOM,Applitools 偏视觉 diff)
- **Canvas 应用**的测试是真实空白(Figma / Canva / Miro / Excalidraw 类公司痛)
- AI 视觉理解能力 2024 年才稳定可用,赛道刚开
- 长期延伸到 agent sandbox 基础设施(对标 Browserbase / E2B),长期价值更大

### 创始人匹配

用户(jeff)有 Playwright 实战经验,真实感受过痛点 — founder-product fit 强。

## 3. 核心架构创新:Record + Self-heal 双模式

**问题:** AI agent 慢、贵、不确定,直接拿来跑测试不实用。

**解决方案:** 测试以两种模式运行,自动切换:

```
首次运行 / UI 变化后:
  Agent Mode (LLM 推理 + 执行 + 录制)
    ↓
  Recorded Trace (具体动作序列 + 屏幕基线)
    ↓
后续运行(99% 时间):
  Replay Mode (按 trace 复跑,快/便宜/确定)
    ↓
  失败时 → 回 Agent Mode 自愈 → 更新 trace
```

**关键属性:**
- 平稳期成本和速度 ≈ Playwright(因为走 replay)
- UI 变化时自愈,不需要人工修测试
- 测试源文件是**自然语言描述**,不是脚本
- 真 bug 时(自愈也失败)正确报错

## 4. 系统架构

### 4.1 高层架构

```
┌─────────────────────────────────────────────┐
│  CLI / SDK / CI 集成                         │
│  qdesk run / qdesk record / qdesk report    │
└────────────────────┬────────────────────────┘
                     │ HTTPS
                     ▼
╔════════════════════════════════════════════╗
║  CONTROL PLANE                              ║
║  · /v1/sessions       开 session            ║
║  · /v1/sessions/:id/actions   执行动作       ║
║  · /v1/sessions/:id/screenshot 截屏          ║
║  · /v1/sessions/:id/dom        a11y 树       ║
║  · /v1/tests           测试运行/记录/重放     ║
║  · /v1/traces          trace 存取           ║
║  · 认证 / 限流 / 计费 / 调度 / 审计           ║
╚══════════════════════╤═════════════════════╝
                       │ 内网 RPC
            ┌──────────┼──────────┐
            ▼          ▼          ▼
      ┌─────────┐ ┌─────────┐ ┌─────────┐
      │ DATA    │ │ DATA    │ │ DATA    │
      │ NODE    │ │ NODE    │ │ NODE    │
      │ ┌──┬──┐ │ │ ┌──┬──┐ │ │ ┌──┬──┐ │
      │ │SBX│SBX│ │ │SBX│SBX│ │ │SBX│SBX│
      │ └──┴──┘ │ │ └──┴──┘ │ │ └──┴──┘ │
      └─────────┘ └─────────┘ └─────────┘
```

### 4.2 沙箱内部

每个 sandbox = 一个 Linux 容器(Phase 0)/ microVM(Phase 2+):

```
┌─────────────────────────────────────────────┐
│  Container / microVM                         │
│  ┌────────────────────────────────────────┐ │
│  │  qdesk-agentd (Rust HTTP, :7878)       │ │
│  │  - 截屏 / 注入鼠标键盘 / 读 a11y         │ │
│  │  - 实现 control plane 调用的 RPC        │ │
│  └─────────────┬──────────────────────────┘ │
│                │                              │
│  ┌─────┐ ┌──────────┐ ┌─────────┐ ┌────────┐│
│  │Xvfb │ │ xdotool  │ │ AT-SPI  │ │ Apps   ││
│  │虚拟 │ │ scrot    │ │ a11y    │ │ Chrome ││
│  │屏幕 │ │          │ │ bridge  │ │ Office ││
│  └─────┘ └──────────┘ └─────────┘ └────────┘│
│  ┌────────────────────────────────────────┐ │
│  │  Window Manager (xfce4 minimal)        │ │
│  └────────────────────────────────────────┘ │
└─────────────────────────────────────────────┘
```

### 4.3 测试执行引擎

```
┌─────────────────────────────────────────────┐
│  Test Runner                                 │
│                                              │
│  1. 解析测试描述 (DSL 或自然语言)              │
│  2. 决定模式:                                 │
│     - 已有 trace + UI 未变 → Replay Mode      │
│     - 否则 → Agent Mode                       │
│  3. 执行                                      │
│     Replay: 按 trace 调 actions API           │
│     Agent:  循环 [截屏 → LLM 推理 → 动作]     │
│  4. 断言验证                                   │
│  5. 失败 → Agent Mode retry(自愈)             │
│  6. 输出报告(视频 / 时间线 / AI 诊断)          │
└─────────────────────────────────────────────┘
```

## 5. 关键技术决定

| 维度 | Phase 0 选择 | Phase 1+ 演进 | 理由 |
|---|---|---|---|
| 隔离层 | Docker | Firecracker microVM | Docker 1-2s 启动够 MVP;Firecracker ~125ms + 真隔离,商业化必换 |
| 显示层 | Xvfb (虚拟 framebuffer) | + dummy GPU | 无 GPU 沙箱可高密度部署(32 核机跑 100+ session) |
| 输入注入 | xdotool | ydotool / 内核 evdev | 简单可靠 |
| 截屏 | scrot / xwd / mss (Python) | DRM 直读 framebuffer | MVP 简单优先;后期优化到 <30ms |
| 结构化界面 | AT-SPI a11y 树 | + 自研 OCR / DOM 抓取 | a11y 树是文本骨架,LLM 友好 |
| 桌面环境 | xfce4-minimal | 自定义最小 WM | xfce4 工具链够用 |
| in-box agent | Rust + axum | 同 | 单文件二进制,启动快 |
| 控制面 | Rust + axum | 同 | 与 in-box agent 共享 crate;长期上 Firecracker 时一致 |
| 数据库 | SQLite (单机) | Postgres + Redis | MVP 简单 |
| LLM | **多模型适配层**(默认 Claude) | + GPT-4o vision / Gemini / 本地模型 | 接口从 Day 1 抽象,客户可自选 model;避免 vendor lock-in |

## 5.1 LLM 多模型适配层

从 Phase 0 起,test runner 通过统一 trait 调用模型,不直接耦合任何厂商:

```rust
trait VisionAgent {
    async fn act(&self, prompt: &str, screenshot: &[u8])
        -> Result<Vec<Action>>;
    async fn diagnose(&self, trace: &Trace, failure: &Failure)
        -> Result<Diagnosis>;
}

// 实现:ClaudeAgent / GPT4oAgent / GeminiAgent / OllamaAgent / ...
```

配置文件:

```yaml
# qdesk.config.yaml
llm:
  default: claude
  providers:
    claude:
      model: claude-opus-4-7
      api_key: ${ANTHROPIC_API_KEY}
    gpt4o:
      model: gpt-4o
      api_key: ${OPENAI_API_KEY}
    local:
      model: qwen-vl-72b
      endpoint: http://localhost:8000
```

测试可指定模型:

```yaml
test: "保存矩形"
llm: gpt4o      # 或不写,用 default
```

**理由:**
- 避免 vendor lock-in(客户重要决策因素)
- 各模型在不同任务上有差异(Claude Computer Use 强、GPT-4o 视觉细、Gemini 便宜)
- 本地模型选项对企业 / 合规客户友好
- A/B 测试不同模型的 trace 命中率,数据反哺产品

## 6. 对外 API

### 6.1 测试运行 API

```http
POST /v1/tests/run
{
  "test_file": "...",        # 内容或 ref
  "mode": "auto",            # auto | force-agent | force-replay
  "template": "chrome-only"
}
→ { "run_id": "run_abc", "status_url": "..." }

GET /v1/tests/runs/{id}
→ { "status": "passed", "trace_id": "...", "report_url": "..." }
```

### 6.2 沙箱原始 API(给非测试用例:agent / RPA 等)

```http
POST /v1/sessions
{ "template": "ubuntu-chrome", "ttl_seconds": 600 }
→ { "session_id": "sbx_a1b2c3", "endpoint": "...", "expires_at": "..." }

GET /v1/sessions/{id}/screenshot
→ image/png

POST /v1/sessions/{id}/actions
{ "type": "click", "x": 100, "y": 200 }
{ "type": "type", "text": "hello" }
{ "type": "key", "keys": ["ctrl", "s"] }
{ "type": "scroll", "x": 100, "y": 200, "dy": -3 }
{ "type": "drag", "from": [100,200], "to": [300,400] }
→ { "ok": true, "screen_changed": true }

GET /v1/sessions/{id}/dom
→ { "elements": [{ "role": "button", "name": "Save", "bounds": [...] }, ...] }

POST /v1/sessions/{id}/snapshots → { "snapshot_id": "..." }
POST /v1/sessions/{id}/restore  { "snapshot_id": "..." }
DELETE /v1/sessions/{id}
```

### 6.3 测试 DSL(初版)

```yaml
test: "保存矩形"
template: chrome-only
setup:
  url: https://excalidraw.com
steps:
  - 画一个红色的矩形
  - Cmd+S 保存
  - 刷新页面
expect:
  - 矩形仍然存在,颜色为红色
```

完全自然语言版本(后续):

```
test "保存功能" {
  打开 Figma
  画一个矩形并保存
  刷新后矩形还在
}
```

## 7. 观测 / 审计 / 回放

每个 test run 全程记录:
- **Action Log** (Postgres) — 每个动作 1 行 JSON,带前后截屏 hash
- **Screen Recording** (S3) — 屏幕变化时存 PNG,带时间戳
- **Live Stream** (WebSocket) — 实时观察(开发者 / CI 可订阅)
- **AI Diagnosis** — 失败时 LLM 自动写诊断报告

报告呈现:HTML 单页面,左侧时间线,右侧屏幕回放,底部 AI 解说。

## 8. Phase 0 MVP 范围(2 周内跑通)

**目标:** Demo 级跑通"AI 测试 Excalidraw 保存功能"。

### 范围内
- 单台 Linux 主机(开发机)
- Docker 容器:Ubuntu + Xvfb + xfce4 + Chrome
- `qdesk-agentd` Rust 服务(沙箱内,~500 行)
- `qdesk-control` Rust 服务(主机上,~800 行)
- 测试运行器:仅支持 Agent Mode(还没有 trace 录制)
- API: `POST /v1/sessions`, `/actions`, `/screenshot`, `DELETE`
- 直连端口映射(每个 sandbox 占主机端口)
- SQLite 内存模式,session 元数据
- 模板:仅 `ubuntu-chrome`

### 范围外
- Replay Mode + trace 自愈(Phase 1)
- 多机集群 / Firecracker(Phase 2)
- DOM / a11y 抽取(Phase 1)
- 实时观察 WebSocket(Phase 1)
- Web UI / dashboard(Phase 1)
- 鉴权 / 计费(Phase 1+)
- 录像存 S3(Phase 1)
- 多模板(Phase 1)

### Phase 0 验收测试

```bash
# 启服务
$ qdesk-control serve --port 8080

# 用 demo 脚本跑测试
$ python examples/excalidraw_demo.py
[Agent mode - first run]
🤖 打开 excalidraw.com...
🤖 找到矩形工具,在画布拖动绘制...
🤖 选中矩形,改颜色为红色...
🤖 Cmd+S 保存,等待提示...
🤖 刷新,验证矩形存在...
✅ PASS (45 秒)
```

## 9. Phase 1 / 2 / 3 路线

### Phase 1 (4 周):产品形态完整
- 加 Replay Mode + trace 录制
- Web UI:列出 runs,实时观察,看回放
- API key 鉴权(简版)
- 屏幕录像存对象存储 + 报告页面
- DOM/a11y 抽取
- 多模板(office / dev-tools / firefox / headless)
- 单机稳定撑 50 并发 session
- CI 集成:GitHub Action

### Phase 2 (8 周):上云 + 商业化
- 切 Firecracker microVM(<500ms 启动 + 真隔离)
- 多 data node 调度
- 计费集成(Stripe)
- 快照 / 恢复
- 自部署版本(on-prem 客户)
- Landing page + docs

### Phase 3+:差异化
- GPU sandbox 产品线
- 行业垂直模板(财务 / HR / 法务 ERP 预装)
- 训练数据导出(成功 trace 卖给做 agent 模型的公司)
- agent SDK(Python/TS,简化客户接入)
- Chaos mode(自动遍历状态空间)

## 10. 模块布局

```
qdesk/
├── crates/
│   ├── qdesk-control/      # 控制面 (axum HTTP API)
│   ├── qdesk-agentd/       # 沙箱内 daemon
│   ├── qdesk-protocol/     # 共享数据类型 (serde)
│   ├── qdesk-runtime/      # Docker / Firecracker 抽象
│   ├── qdesk-runner/       # 测试执行引擎 (Agent Mode + Replay Mode)
│   └── qdesk-cli/          # qdesk 命令行
├── images/
│   └── ubuntu-chrome/
│       ├── Dockerfile
│       └── entrypoint.sh
├── examples/
│   └── excalidraw_demo.py  # Phase 0 验收 demo
├── docs/
│   └── superpowers/specs/
└── Cargo.toml
```

## 11. 商业模式(未来)

```
免费层:
  · 本地运行,自带 LLM API key
  · 开源 CLI + runner
  → 社区 / 口碑 / 飞轮起点

Pro ($49/人/月):
  · 托管运行
  · CI 集成
  · 团队 dashboard
  · AI 失败诊断
  → 个人开发者 / 小团队

Team ($299+/月/团队):
  · 共享 trace 库
  · SSO
  · 私有沙箱模板
  → 中等公司

Enterprise:
  · On-prem 部署
  · 自定义垂直模板
  · Audit / compliance
  → 大公司
```

## 12. 竞品定位

| 类别 | 玩家 | 我们的差异 |
|---|---|---|
| 传统 web 自动化 | Playwright / Cypress | 不写脚本,自愈,canvas 支持 |
| 视觉测试 | Applitools / Percy | 我们测交互+断言,他们只测像素 diff |
| AI 测试平台 | Octomind / mabl / Testim | 偏 DOM,且不开源;我们 canvas 强,开源 wedge |
| Browser agent | Browser Use / Skyvern | 不专做测试,无 self-heal trace 概念 |
| Agent sandbox 基建 | Browserbase / E2B | 我们做的更广(桌面),且有测试 wedge |

**最锋利的 wedge:canvas 应用测试,目前无人专攻。**

## 13. 战略选择(已决)

| 决策 | 选择 | 理由 |
|---|---|---|
| 押差异化 | 桌面应用一等公民 + canvas 测试 | 不和 Browserbase 撞 |
| 开源策略 | sandbox 开源、控制面闭源 | 借势社区,留商业护城河 |
| MVP 验收 | 自研 demo 测 Excalidraw | 直接打 canvas 痛点 |
| 主语言 | Rust | Firecracker 阶段一致;in-box / control plane 共享 crate |
| 付费模型 | 按 session-分钟 | 简单可懂 |
| GTM 路径 | C(测试 wedge → 通用 sandbox) | 痛点更尖,客户预算更明确 |

## 14. 主要风险

1. **AI 测试 ≠ 完全可靠** — 即使有 self-heal,某些复杂场景 agent 仍然会走错
   - 缓解:Replay Mode 是主路径,Agent 仅在变化点出现;客户能审 trace
2. **LLM 成本** — 大规模跑测试,Agent Mode 单次可能 $0.5+
   - 缓解:Replay 模式 99% 时间走;价格随基础模型下降
3. **冷启动** — Phase 0 demo 没 trace 缓存,慢 + 贵
   - 缓解:用第一批 design partner 共建 trace 库
4. **企业销售周期长** — Pro 层 PLG 容易,Team/Enterprise 慢
   - 缓解:Pro 层先跑通;Enterprise 后期再发力
5. **大厂下场** — Anthropic / OpenAI 自己出测试产品
   - 缓解:垂直深度 + 数据飞轮 + 开源社区做护城河

## 15. 已定决策

- [x] **项目名:qdesk**
- [x] **开源协议:Apache 2.0**(比 MIT 多专利条款保护,适合潜在商用)
- [x] **LLM:多模型适配层** — 从 Day 1 支持多 model 可选,默认 Claude Computer Use,可切 GPT-4o vision / Gemini / 本地视觉模型
- [x] **Phase 0 demo:Excalidraw**(免登录 / 纯 canvas / 公开)
- [x] **测试 DSL 后缀:`.qdesk`**

---

## 后续步骤

1. ✅ 设计稿落到此文件
2. ⏳ 用户复审本设计
3. ⏳ 用户拍板待澄清项
4. ⏳ 进入 writing-plans skill,产出实施计划
5. ⏳ Phase 0 实施(2 周目标)
