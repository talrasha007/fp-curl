# fp-curl

一个最小的 Go 命令行工具，尽量兼容常见 `curl` 用法，并新增 `--fp` 参数。

执行流程：

1. 解析并移除 `--fp` / `--fp=value`
2. 先把 `--fp` 的值打印出来
3. 使用 `github.com/talrasha007/CycleTLS` 发起请求

当前支持的常用参数：

- URL 位置参数
- `-X`, `--request`
- `-H`, `--header`
- `-d`, `--data`, `--data-raw`, `--data-binary`
- `-I`, `--head`
- `-i`, `--include`
- `-o`, `--output`
- `-x`, `--proxy`
- `-L`, `--location`
- `-k`, `--insecure`

未实现的 `curl` 参数会直接报错，避免行为悄悄偏离预期。

当前 `CycleTLS` 默认配置对齐代码里的实现：

- `ShuffleExtensions: true`
- `EnableClientSessionCache: true`
- `Meta: "ignore_ja3"`
- `EnableConnectionReuse: false`
- `MaxIdleClients: 128`
- `MaxTotalRequests: 2`
- `MaxResponseBodySize: -1`
- `SignatureAlgorithms: "RAND"`
- `Ja3: "RAND"`
- 默认 `UserAgent` 为示例中的 Chrome UA；如果传了 `-H "User-Agent: ..."` 会覆盖

## 构建

```powershell
go build -o fp-curl.exe .
```

## 示例

```powershell
.\fp-curl.exe --fp demo https://example.com
.\fp-curl.exe --fp demo -X POST -H "Content-Type: application/json" -d "{\"name\":\"codex\"}" https://example.com
.\fp-curl.exe -I https://example.com
```
