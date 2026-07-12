# Task egress-serve (egress 包 2/2) — serve 上行持久水位:重启只推增量

## 背景
internal/serve/relay.go 的 replayProject 每次 serve 启动把整个账本 [0, upTo) 重新 enqueue
上行(靠 relay 幂等去重)。本周十余次重启 × 全项目 = 数万次多余 ingest 往返。

## 交付
1. per-project 持久水位:`.pact/relay-uploaded.json`(runtime 文件;确认 .pact runtime ignore
   机制——照 status.json 等现有 runtime 文件的处理,走 .git/info/exclude 已有规则,不入 git):
   `{ "<project>": <uploadedByteOffset> }`,原子写(tmp+rename),0o600。
2. 上传成功推进水位:postOne 成功(2xx)后按该行字节数推进对应项目水位并节流落盘
   (如每 32 行或 2s 一次;进程退出 flush)。失败/重试不推进。
3. replayProject 改从 `max(0, watermark)` 起补到 upTo;水位 > 文件大小或读取异常 → 回退 0
   全量(幂等兜底,同时重置水位)。
4. 边界:项目重命名/删除(孤儿键无害,可惰性清);多项目并发写同一文件(互斥锁);
   PACT_RELAY_URL 未配则一切不变。
## 测试
水位持久化/重启增量(fake relay 收到的行数断言)/截断回退全量/失败不推进/原子写。
go test ./internal/serve/... -race 绿 + go vet。
verify: go test ./internal/serve/... && go vet ./internal/serve/...
