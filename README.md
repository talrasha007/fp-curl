# fp-curl

一个最小的 Go 命令行工具，尽量兼容常见 `curl` 用法，并新增 `--fp` 参数。

执行流程：

1. 解析并移除 `--fp` / `--fp=value`
2. 先把 `--fp` 的值打印出来
3. 使用 Go 内置 `net/http` 发起请求

当前支持的常用参数：

- URL 位置参数
- `-X`, `--request`
- `-H`, `--header`
- `-d`, `--data`, `--data-raw`, `--data-binary`
- `-I`, `--head`
- `-i`, `--include`
- `-o`, `--output`
- `-L`, `--location`
- `-k`, `--insecure`

未实现的 `curl` 参数会直接报错，避免行为悄悄偏离预期。

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
