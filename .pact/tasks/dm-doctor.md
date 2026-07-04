# dm-doctor — doctor per-vendor CLI 预检

> 母 spec:`docs/specs/driver-modernization.md` §4。无依赖。

## 交付
1. `internal/doctor` 新增 per-kind 检查(遍历 `agent.Kinds()` 中有 headless RunnerSpec 的 kind):
   - binary:`exec.LookPath(RunnerSpec.Command)`(用注入的 PATH 参数,保持 doctor.Run 可测风格);
   - 认证态(静态,不发网络):按 kind 查凭据文件非空——claude-code:`~/.claude/.credentials.json`(或存在 macOS Keychain 时标记 `keychain?` 不判死);codex-cli:`~/.codex/auth.json`;gemini-cli:`~/.gemini/oauth_creds.json`;kimi-cli:`~/.kimi`(目录存在即视为可能已登录,宽松);opencode:跳过认证检查(本地模型可无凭据)。HOME 经参数注入可测。
   - ACP 可用性:kind 在 ACP 映射表(kimi/claude/codex/gemini)→ 报 `transport: acp available`,否则 `cmd only`。
2. 输出并入 `doctor.Run` 的 Check 列表;红项一行修复提示(`brew install …` / `<cli> login`)。
3. serve 启动时调用同一检查,结果写日志(stderr),**不阻断**。

## 测试
表驱动:fake HOME(t.TempDir 造凭据文件)+ fake PATH(t.TempDir 放假 binary);全部场景无真 CLI。`go test ./internal/doctor/` 绿。

## 边界
不改机器 register 的 agentKinds 语义;不发任何网络请求。
