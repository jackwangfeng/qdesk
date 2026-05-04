# qdesk team quickstart (for Claude Code users)

**5 分钟把 qdesk 接进你的 Claude Code,以后写完前端 UI,Claude 自动验证。**

---

## TL;DR

```bash
# 1. 装 binary(下载 + 解压到 ~/.local/bin)
curl -fsSL https://raw.githubusercontent.com/jackwangfeng/qdesk/main/scripts/install.sh | bash

# 2. 设环境变量(你团队应该有共享 GEMINI_API_KEY,问 jeff;
#    QDESK_DEV_KEY 随便你设,只要本机 control 和 mcp 一致就行)
echo 'export GEMINI_API_KEY=AIza...' >> ~/.bashrc
echo 'export QDESK_DEV_KEY=qdesk-team-2026' >> ~/.bashrc
source ~/.bashrc

# 3. 后台起控制面(本机,长期挂着)
nohup qdesk-control \
    --listen 127.0.0.1:8090 \
    --dev-key "$QDESK_DEV_KEY" \
    --image qdesk/ubuntu-chrome:dev \
    > ~/.qdesk-control.log 2>&1 &

# 4. 注册到 Claude Code
claude mcp add --transport stdio qdesk -- qdesk-mcp \
    --control http://127.0.0.1:8090 \
    --api-key "$QDESK_DEV_KEY" \
    --gemini-key "$GEMINI_API_KEY"

# 5. 重启 Claude Code session,验证已挂上
claude /mcp        # 应该看到 qdesk 在 list 里
```

完事。Claude 现在写完代码会自动可以调用 4 个 qdesk 工具来验证 UI。

---

## 验证装好了

打开 Claude Code,在任何项目里跟它说:

> "测一下 https://example.com 能不能正常打开"

Claude 应该自动调 `qdesk_screenshot` 工具,30 秒后给你看一张 example.com 的截屏。

---

## 用法:让 Claude 帮你测自己写的 UI

最常见的场景:你刚改完前端,问 Claude 帮你验证。

**示例 1:基础 smoke test**
> "刚把登录页加了个'忘记密码'链接,跑下应用本地服务确认它能跳到重置密码页面。本地服务在 http://host.docker.internal:8888"

Claude 会自动:
1. 调 `qdesk_quick_test`,传一个 inline goal + expect
2. 30-60 秒后告诉你 PASS / FAIL
3. 如果 FAIL,贴截屏 + AI 写的诊断

**示例 2:多步流程**
> "测一下注册流程:输入 test@example.com → 点 continue → 验证收到验证码输入框"

**示例 3:看一眼**
> "我新加的设置页长啥样?跑 http://host.docker.internal:8888/settings 给我看截屏"

---

## 你的项目要做什么(每个项目一次)

把 `.mcp.json` 加到项目根目录(让 Claude Code 知道这项目用 qdesk):

```bash
cd /path/to/your-project

cat > .mcp.json <<'EOF'
{
  "mcpServers": {
    "qdesk": {
      "command": "qdesk-mcp",
      "args": [
        "--control", "http://127.0.0.1:8090",
        "--llm",     "gemini-2.5-flash"
      ],
      "env": {
        "QDESK_DEV_KEY":  "${QDESK_DEV_KEY}",
        "GEMINI_API_KEY": "${GEMINI_API_KEY}"
      }
    }
  }
}
EOF

git add .mcp.json && git commit -m "chore: enable qdesk MCP for Claude Code"
```

接下来你 team 任何成员 clone 这个项目并打开 Claude Code,**qdesk 工具自动可用**(只要他们本机装了 qdesk + control plane 跑着)。

---

## 附:写持久化测试文件

随手测的可以让 Claude 用 `qdesk_quick_test` 一次性跑(不留文件)。
重要流程要存成 `.qdesk.yaml` 跟代码一起进 git:

```yaml
# tests/qdesk/login-flow.qdesk.yaml
name: "Login flow"
url: http://host.docker.internal:8888

goal: |
  Click 'Sign in', enter test@example.com in the email field,
  click Continue, enter '123456' as the verification code, click Verify.

expect:
  - "After verification, the user is taken to the dashboard /home page."
  - "The user's email 'test@example.com' is shown somewhere on screen."
  - "There is no error toast visible."

ttl_seconds: 300
max_steps: 15
```

跑:
```bash
qdesk run tests/qdesk/login-flow.qdesk.yaml
```

CI 集成:`qdesk run` 退出码 0 = pass, 1 = fail。直接接到 GitHub Actions 跑就行。

---

## 出问题怎么办

**"qdesk-control: connection refused"**
- 控制面没在跑。`ps -ef | grep qdesk-control`,如果没,重新跑 step 3。

**"GEMINI_API_KEY is empty"**
- env 没生效。`echo $GEMINI_API_KEY` 应该看到 AIza... 开头的 39 字符。
- 重启 shell 或 `source ~/.bashrc`。

**"image not found"**
- 沙箱镜像缺失。先装 docker,然后:
  ```bash
  cd /tmp && git clone https://github.com/jackwangfeng/qdesk.git
  cd qdesk && make image
  ```

**"agent stuck clicking same coords"**
- Gemini Flash 在某些 canvas UI 上 Y 坐标偏小。在 .qdesk.yaml 里加 `llm: gemini-2.5-pro` 切到更准的模型(贵 10x 但可靠)。

**报告在哪?**
- 每次跑完命令行最后一行有 `📄 report: file:///...`。直接点开看就行。也可以 `~/qdesk-runs/` 翻历史。

---

## 它实际做什么

| 工具 | 干啥 | 触发 |
|---|---|---|
| `qdesk_health` | check 控制面在不在 | Claude 第一次接到 qdesk 任务时自检 |
| `qdesk_screenshot` | 打开 URL 截屏 | "看一眼这个页面长啥样" |
| `qdesk_quick_test` | inline 写测试跑 | "改完了帮我验证下" |
| `qdesk_run_test` | 跑已有 .qdesk.yaml 文件 | "跑一下 tests/qdesk/login.qdesk.yaml" |

---

## 接下来

- 跑通后翻 `SKILL.md` 看更详细的 prompt 写法和坑
- 编辑 qdesk 自己代码看 `AGENTS.md`
- 有 bug 来 https://github.com/jackwangfeng/qdesk/issues 提
