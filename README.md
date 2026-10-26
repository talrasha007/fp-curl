# fp-curl

一个最小的 Go 命令行工具，尽量兼容常见 `curl` 用法，并新增 `--fp` 参数，取值为curl, chrome, rand。

当前支持的常用参数：

- URL 位置参数
- `-X`, `--request`
- `-H`, `--header`
- `-A`, `--user-agent`
- `-d`, `--data`, `--data-raw`, `--data-binary`
- `-I`, `--head`
- `-i`, `--include`
- `-o`, `--output`
- `-x`, `--proxy`
- `--resolve IP`、`--resolve host:port:IP`（可重复）
- `--interface IP`（通过 CycleTLS `LocalIP` 绑定本地源 IP）
- `-L`, `--location`
- `-k`, `--insecure`
- `--http3`

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
- 默认 `UserAgent` 为示例中的 Chrome UA；如果传了 `-A "..."`、`--user-agent "..."` 或 `-H "User-Agent: ..."` 会覆盖

## 构建

```powershell
go build -o fp-curl.exe .
```

## 示例

```powershell
.\fp-curl.exe --fp demo https://example.com
.\fp-curl.exe --fp demo -X POST -H "Content-Type: application/json" -d "{\"name\":\"codex\"}" https://example.com
.\fp-curl.exe -I https://example.com
.\fp-curl.exe --resolve 192.0.2.10 https://example.com
.\fp-curl.exe --resolve example.com:443:192.0.2.10 https://example.com
.\fp-curl.exe --resolve "example.com:443:[2001:db8::10]" https://example.com
.\fp-curl.exe --interface 192.168.1.10 --resolve 192.0.2.10 https://example.com
```

`--resolve` 和 `--interface` 均支持 `--参数=值` 写法。

`--resolve IP` 将所有请求（包括 `-L` 跟随的重定向）连接到该 IP，保留每个 URL 的端口、Host 和 TLS SNI。
`--resolve host:port:IP` 仅覆盖匹配域名及端口的请求；省略 URL 端口时 HTTP 使用 80、HTTPS 使用 443。
可以重复指定规则，也可使用 `*:port:IP` 作为后备规则；具体域名优先，同一域名及端口最后一条生效。裸 IP 为全局覆盖，优先于域名规则；多次传入裸 IP 时最后一个生效。
IPv6 可直接作为裸 IP 传入，规则中的 IPv6 可用方括号包裹。
当前支持每条规则一个 IP，不支持 curl 的多地址列表、`+` 临时规则和 `-` 删除规则。

`--interface` 接受本机已配置的 IPv4 或 IPv6 地址，不接受网卡名称。该参数直接传入 CycleTLS 的 `LocalIP`；CycleTLS v0.8.0 在 HTTP/3 下会忽略此配置。
