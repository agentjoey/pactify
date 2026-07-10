# Task join-handshake (C-13) — join 携带 protocol_version 并 fail-fast 版本不符

## 目标(backlog C-13 的可落地子集)
init 事件已带 `protocol_version`(paths.ProtocolVersion),validate 也已 fail-closed;缺的是
**join 时的双向校验**:老 pactify 客户端加入新 major 项目应在 join 即报错,而非之后行为怪异。
(C-13 还提了 capabilities[] —— **本任务不做**:当前无任何消费方,空词汇表只会漂移;
 记 backlog 一句 defer 即可。)

## 改动
### internal/pact/engine.go(joinWithClientLocked)
1. join 前读项目 protocol_version(照 rules.go validateLog 的做法:扫 init 事件 payload 的
   protocol_version;抽一个包内 helper `projectProtocolVersion(evs []event.Event) int` 供两处复用,
   validateLog 同步改用它——纯重构)。
2. 若 `projVer > paths.ProtocolVersion` → 返回错误
   `pactify join: project protocol_version %d exceeds this client's supported %d; upgrade pactify`。
3. join payload 加 `"protocol_version": paths.ProtocolVersion`(**恒发**——它是新 join 的事实声明;
   旧日志无此键仍合法,additive)。
### schemas/event.schema.json
- join payload 的 schema 若显式枚举可选键(client/kind 那样),加 `protocol_version`(integer,可选);
  若 additionalProperties 宽松则确认不拦。**schemas.bats 必须仍绿**。
### 测试
- engine_test:①正常 join 后 log 里 join 事件含 protocol_version==1;②篡改 init 的
  protocol_version 为 99(照 TestValidateFailsClosedOnHigherProtocolMajor 的读改写手法 + LogReplay
  重建 STATE)后,新 seat join 返回错误且信息含 "upgrade pactify";③老项目(手工去掉 init 的
  protocol_version 键 + replay)join 不报错(缺省=0 兼容)。
- validateLog 重构后现有 validate 测试全绿。

## 验收(视角: correctness — fail-fast 明确、additive 兼容、schema 门绿)
verify: go build ./... && go test ./internal/pact/ && bats tests/schemas.bats
(注意 bats 用 jsonschema-capable python:PATH 里 /opt/homebrew/opt/python@3.11/libexec/bin 前置)
