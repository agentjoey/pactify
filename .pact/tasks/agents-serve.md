# Task serve · serve

**Feature:** agents · **Owner:** opencode-worker · **Reviewer:** claude · **Deps:** cli

## 目标
机器级 agent 端点，镜像 internal/serve/wiring.go 的注册模式。

## 改文件
新建 `internal/serve/agents.go` + `agents_test.go`；在 api.go 的 Handler() 注册处加 `s.registerAgentRoutes(mux)`（紧挨 registerWiringRoutes）。

## 端点（机器级，**不挂** /projects/{id}）
- `GET /api/agents`：合并 agent.Scan() 与 agentreg.Load()，每条 `{kind, installed, detail, registered, label}`。
- `POST /api/agents/{kind}/register`（body `{"label":"..."}` 可空）：校验 kind 已知 → agentreg.Register（serve 层取 ts）→ Save → 200。author 写门同 wiring（参照 handleWire）。
- `DELETE /api/agents/{kind}/register`：agentreg.Unregister → Save → 200。
- 未知 kind → 400；写门未过同 wiring。

## 验收
agents_test.go（httptest，PACTIFY_HOME tempdir）：GET 含已知 kind 且 registered 反映状态；POST register 后 GET registered=true + label；DELETE 后 false；POST 未知 kind→400；非 author 写被拒（同 wiring 测试）。

## verify
```
go test ./internal/serve/ -run Agents
```

## 完成方式
TDD。座席 opencode-worker。checkpoint serve 附 verify 输出。不自标 accepted。
