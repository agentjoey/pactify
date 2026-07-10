# Task gate-diff-scope (C-10) — 门命令的变更范围注入({files} / PACT_CHANGED_FILES)

## 目标(backlog C-10 的可落地形态)
门命令(task `verify:` 行 / config gate)现在只能全仓跑——全仓 linter 会撞范围外历史债
(planner 规范只能"禁止",不能"给出路")。给门命令注入**merge-base 变更文件集**,
让 `eslint {files}` 这类范围化门成为可能:
1. 跑门前计算 `git diff --name-only <merge-base(base,HEAD)>...HEAD`(base=feature 的 base 分支,
   拿不到 base 时 fallback `git diff --name-only HEAD~1...HEAD`?**不**——拿不到就空集,见 3)。
2. 注入两种形态:
   - **占位符**:命令含 `{files}` → 替换为空格分隔的文件列表(shell-quote 每个文件名);
     变更集为空时替换为空串**且跳过该门**(视为 pass,detail 注明 "no changed files — gate skipped")
     ——空 {files} 传给 eslint 等会变成"全仓"或报错,跳过才是范围化语义。
   - **环境变量**:PACT_CHANGED_FILES(换行分隔)恒注入门命令的 env(不含 {files} 的门零行为变化,
     但脚本型门可自取)。
3. base 解析:feature 的 base 分支(状态里有;orchestrate 侧知道 feature)。解析/померge-base 失败
   → {files} 门 fail-closed(报错 detail 说明拿不到变更集),非 {files} 门照旧跑(env 缺省为空)。
4. 挂点:`runGate`(internal/orchestrate/gate.go:93)加参或新包装 `runGateScoped(ctx, exec, dir,
   command, base string)`;调用点 loop.go:661/848 传 feature base。cmdExec 接口若不支持 env,
   看其实现(grep type cmdExec / 实现体)——若是 shell 执行,PACT_CHANGED_FILES 可经
   `env PACT_CHANGED_FILES=... sh -c` 或实现体加 env 参数(选实现最小侵入的)。
5. 文档:planner prompt 的 verify 规范段(internal/planner/prompt.go 的 Verify rules)加一句:
   可用 `{files}` 占位符范围化(例 `verify: npx eslint {files}`)。

## 测试
- 纯函数:changedFiles(dir, base) 用真 git 临时仓(建 base 分支+改文件)断言列表;quote 含空格文件名。
- runGateScoped:含 {files} 且有变更 → 命令收到替换后的文件;空变更 → skip-pass;
  base 解析失败 + {files} → fail-closed;不含 {files} → 命令原样 + env 注入(fake exec 断言)。
- planner prompt 测试同步(若断言全文)。

## 验收(视角: correctness — 不含 {files} 的门字节不变、空集跳过语义、fail-closed)
verify: go build ./... && go test ./internal/orchestrate/ ./internal/planner/
