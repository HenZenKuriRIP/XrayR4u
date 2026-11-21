# 安装对照后的节点改动说明

v0.9.19 已按第 1–8 节落地。下面保留当时的判断，避免以后把已经排除的现象再改一遍。第 0 节仍然不要当成协议缺陷去修。

本文给 XrayR4u 后续改代码用。依据是同一套面板上的两组安装：

- 手工部署能同时提供 VLESS 和 Hysteria2。布局是 nginx 占用 TCP 443，VLESS 使用面板上的 TCP 端口（现网是 8443），Hysteria2 使用 UDP 443，`SendIP: 0.0.0.0`，`CertMode: file`，伪装反代到本机 80。
- `deploy/install.sh` 菜单 `[5]` 装出的机器上，VLESS 能连通，Hysteria2 不能。进程、证书和用户表当时都是好的。客户端失败是因为订阅里的 Hysteria2 主机名后来变成了 NXDOMAIN，而 VLESS 订阅主机是 IP。

改代码时按下面的优先级。第 0 节里的现象不要当成协议缺陷去修。

不改 UniProxy 的路径和 JSON。不在安装脚本里打开 Salamander 或 Brutal。面板订阅只带 `sni` 和 `alpn=h3`，节点若自行打开混淆或 Brutal，面板客户端会全部失败。

已经在跑 v0.9.16 的 REALITY 机器保持原二进制。v26.11.9 会拒绝没有 `X25519MLKEM768` 的 ClientHello，也会拒绝 `edge`、`ios`、`qq`、`random`。允许的指纹是 `chrome`、`firefox`、`safari`、`randomized`。等面板订阅带上混合密钥之后，再升级那些旧机器。

## 0. 不要修的现象

| 现象 | 代码里的实际行为 |
|------|------------------|
| 脚本把 `Hysteria2` 整段注释掉 | `MasqType` 为空时，核心用 HTTP 404 回应未认证探测。认证仍然走用户 UUID。正确密码的客户端不读这个页面。 |
| 怀疑 ALPN 没写上 | `install.sh` 写了 `ALPN: ["h3"]`。`buildTLSSettings` 会把这份列表抄进 TLS。`applyHysteriaALPN` 发现列表里已有 `h3` 就直接返回，结果仍是 `h3`。 |
| 怀疑脚本漏了 `SendIP` | 脚本三条节点都写了 `SendIP: 0.0.0.0`。脚本机这次的 Hysteria2 故障不是这项。漏写才会踩第 1 节的默认值。 |
| 怀疑 HTTP-01 证书只有叶子 | lego 默认 `bundle=true`，`.crt` 含签发链。这次证书已签出，域名和面板 `server_name` 一致，Hysteria2 入站也完成了启动。 |
| `dns.json`、`route.json`、`custom_outbound.json` | 能通的手工部署没有这三个文件。它们在连接被接受之后才生效，VLESS 能访问外网时，它们不是 Hysteria2 单独失败的原因。 |
| 访问日志为空就断定协议坏了 | `accepted` 写在出站拨号之前。Hysteria2 认证失败目前不进 `access.log`。日志为空只说明还没有一次成功接受。 |

脚本机当时的事实，供对照，不要再把它当成可复现的协议错误：

- XrayR 0.9.18 已监听 TCP 8443 和 UDP 443，两边都加载了用户。
- 证书由 `CertMode: http` 签给 Hysteria2 的域名，有效期到 2027-02。
- 之后该域名在 Cloudflare 上变为 NXDOMAIN。VLESS 的面板 `host` 是 IP，所以不受影响。
- 这两条面板节点后来已从数据库删除。域名和节点恢复之前，不要用那台机器验证协议。

## 1. 默认出站地址

文件：`panel/defaultConfig.go`、`panel/panel.go`、`service/controller/outboundbuilder.go`。

`getDefaultControllerConfig` 把 `ListenIP` 和 `SendIP` 都设成 `127.0.0.1`。`panel.Start` 用 `mergo.WithOverride` 合并 YAML。mergo 只用源里的非空值覆盖目标，YAML 里不写 `SendIP` 时，默认的 `127.0.0.1` 会留下来。

`OutboundBuilder` 在 `SendIP != ""` 时设置 freedom 的 `sendThrough`。源地址固定为 `127.0.0.1` 再去连接公网地址，拨号返回 `EINVAL`（errno 22）。VLESS 和 Hysteria2 走同一条出站，两条都会通不过。

`deploy/HYSTERIA2.md` 第 5 节的示例只写了 `ListenIP: 0.0.0.0`，没有 `SendIP`。按该示例手工安装，会正好落在这个默认值上。`install.sh` 已经显式写成 `0.0.0.0`，所以脚本路径避开了它。

改法：

1. 默认 `ListenIP` 和 `SendIP` 改为 `0.0.0.0`。这是现网能通的值，内核据此选择源地址。
2. 保留「YAML 非空优先」。不要用空字符串表示关闭 `sendThrough`，因为 mergo 吃不掉已有默认值。若以后要支持「完全不设置 sendThrough」，增加一个明确字段，例如 `DisableSendThrough: true`。
3. 更新 `HYSTERIA2.md`、`INSTALL.md` 和 `config.yml.template` 的示例，两条都写上 `SendIP: 0.0.0.0`。

验收：一份不写 `SendIP` 的配置启动后，对应出站的 `sendThrough` 是 `0.0.0.0`。显式写成其他地址时仍覆盖默认值。

## 2. 首次启动失败后没有重试

文件：`panel/panel.go`、`service/controller/controller.go`。

`Panel.Start` 对每个节点调用 `Controller.Start`。失败时只打一行日志，进程继续跑，`Running` 仍设为 true。`nodeInfoMonitor` 和 `userReportPeriodic` 只在 `Start` 成功之后才创建。因此第一次 `GetNodeInfo` 或 `GetUserList` 失败后，这条节点不会再试，直到整个进程重启。

同一台机器上的两条节点共用一个出口 IP。面板的节点鉴权限额按 IP 计算，而且发生在写心跳之前。限额窗口内返回 429 时，失败的那条没有心跳，也没有用户表。另一条如果已经 `Start` 成功，会继续服务。看起来就是「一个协议正常，另一个协议没有入站」。

改法：

1. `Start` 失败时为该节点单独做退避重试，间隔使用它自己的 `UpdatePeriodic`，上限可取 5 分钟。
2. 重试期间不要关掉同进程里已经成功的节点。
3. 成功之后再进入现有的周期同步。周期同步里 `GetUserList` 失败保持旧用户表，这个行为保留。
4. 日志写节点类型、节点 ID 和 HTTP 状态。不要写 API Key，也不要写带密钥的 URL。

验收：第一次拉配置返回错误、第二次成功时，不重启进程也能出现入站和用户加载日志。

## 3. 安装脚本把防火墙端口写成 443

文件：`deploy/install.sh`，约 1539–1565 行。

注释写的是面板端口不是 443 时还要放行实际端口。实现是：

- TCP 固定从 `443` 开始，VLESS 或 `CertMode http` 再加 `80`。
- Hysteria2 只加 UDP `443`。

脚本不读面板的 `server_port`。现网 VLESS 在 TCP 8443，是因为 TCP 443 留给了网站。只靠脚本的防火墙规则时，8443 不会被打开。Hysteria2 的面板端口一旦不是 443，UDP 也不会被打开。云厂商安全组跟 ufw 是两层，脚本只能改本机防火墙，结束时要把实际端口打印出来，提醒到云控制台放行同样的 TCP 和 UDP。

改法：

1. 写配置之前，用已输入的 API Key 请求 `GET /api/v1/server/UniProxy/config`，查询参数只放 `node_id`。密钥放 `Authorization: Bearer` 和 `X-Api-Key`。
2. 请求带 `User-Agent: XrayR/<版本>`。经 Cloudflare 时，Python 默认 UA 会得到 403，不能用那个结果判断密钥错误。
3. VLESS 放行返回的 `server_port/tcp`。Hysteria2 放行返回的 `server_port/udp`。两个端口可以相同，协议不同。
4. 接口失败时停下来问端口，不要静默退回 443。
5. 结束摘要打印「本机已放行」的协议和端口，并写明云安全组需要同一组规则。

验收：面板 VLESS 端口为 8443、Hysteria2 端口为 443 时，ufw 或 iptables 里同时有 `8443/tcp` 和 `443/udp`。

## 4. 安装时核对 Hysteria2 域名

Hysteria2 订阅由面板生成，主机名来自面板节点的 `host`，SNI 来自 `sni`。节点的 `/config` 只带回 `server_port` 和 `tls_settings.server_name`，没有订阅主机名。客户端要解析的是面板 `host`。这个名字被删掉或不再指向本机时，VLESS 仍可凭 IP 连通，Hysteria2 在发出 QUIC 之前就失败。

`matchHysteriaCertDomain` 只在 `server_name` 和 `CertDomain` 都非空且不一致时拒绝启动。它不检查这个名字能不能解析，也不检查解析结果是不是本机。

安装脚本改法：

1. `CertMode` 为 `http` 或 `file` 之前，解析操作员输入的域名。
2. `NXDOMAIN` 或没有任何 A/AAAA 时，停止安装，不要继续跑 lego。
3. 把解析到的地址和本机公网地址比较。不一致时打印两边的地址并停止。HTTP-01 在这种情况下签不出证书；签出之后再改 DNS，客户端也会连错机器。
4. 用第 3 节拿到的 `tls_settings.server_name` 和 `CertDomain` 比较。不一致时停止，并提示面板 SNI 要改成证书域名。空的面板 SNI 可以继续，运行时会用本机 `CertDomain`。
5. 脚本结束时打印：Hysteria2 客户端使用面板里的主机名，该名字在证书有效期内都要解析到这台机器。

运行时改法，放在 `nodeInfoMonitor` 里，只对 Hysteria2 做：

1. 每小时解析一次 `CertDomain`。
2. `NXDOMAIN`、无记录、或记录不再包含本机地址时，打一条警告。
3. 不要因此拆掉入站。证书还在，恢复 DNS 后客户端应能继续用。
4. 同一状态不要每 60 秒重复刷日志。

验收：对一个不存在的域名执行安装，脚本在申请证书之前退出。对一个解析到其他地址的域名同样退出。

## 5. 同机网站、证书和伪装

能通的机器使用下面的布局。脚本菜单 `[5]` 目前不装 nginx，Hysteria2 证书用 `CertMode: http`，伪装段保持注释。404 伪装不会拒绝正确密码。这一节是要让脚本能装出和现网相同的形态，不是补一个认证缺陷。

目标布局：

| 项目 | 值 |
|------|----|
| TCP 80、TCP 443 | nginx。公网 80 返回到 HTTPS 的 301。 |
| 本机 `127.0.0.1:80` | 返回伪装页面。用同一个 `listen 80`，在配置里判断 `remote_addr`。另写一条 `listen 127.0.0.1:80` 时，曾经没有形成独立套接字，本机访问仍走到 301。 |
| VLESS | 面板上的 TCP 端口。网站占用 443 时，这个端口不能也是 443。 |
| Hysteria2 | 面板上的 UDP 端口，现网是 443。 |
| 证书 | certbot 申请，完整链复制到 `/etc/XrayR/cert/<域名>.crt` 和 `.key`。节点用 `CertMode: file`。 |
| 续期 | `/etc/letsencrypt/renewal-hooks/deploy/` 里的脚本复制新证书后执行 `systemctl try-restart xrayr`。 |
| 伪装 | `Hysteria2.MasqType: proxy`，`MasqURL: http://127.0.0.1:80`。 |
| 混淆和带宽 | 留空。 |

脚本改法：

1. `[5]` 增加一个问题：是否在 TCP 443 放伪装网站。默认可以是否。
2. 选择是：安装 nginx 和 certbot，写成上面的布局，Hysteria2 用 `CertMode: file` 和 proxy 伪装。VLESS 的面板端口若是 443，停止并说明 443 已被网站占用。
3. 选择否：保持现在的 `CertMode: http` 和默认 404。仍执行第 3、4 节的端口和域名检查。
4. 两种选择都不写 `ObfsPassword`、`Congestion`、`Up`、`Down`。

`controller.go` 里，`CertMode` 为 `http` 或 `dns` 时，每个 `UpdatePeriodic`（默认 60 秒）都会调用 `RenewCert`。lego 使用 `--days 30`，未到期时应当空跑，但每分钟启动一次进程。80 端口后来被 nginx 占用时，这次空跑会变成持续的续签错误。

改法：根据证书文件的到期时间判断，距离到期超过 30 天就不要调用 lego。两次尝试至少间隔 12 小时。已有一次续签在跑时不要再启动。失败日志按小时合并。

## 6. 安装脚本的其余缺口

文件仍是 `deploy/install.sh`。

1. `config.yml` 现为 `644`。里面有 API Key，改为 `600`，目录保持 root。
2. systemd 单元增加 `WorkingDirectory=/etc/XrayR`。`main.go` 已经按配置文件目录设置 `XRAY_LOCATION_CONFIG`，所以证书路径现在也能落到 `/etc/XrayR/cert`。工作目录补上之后，相对路径不再依赖启动时的 cwd。
3. 覆盖保护目前只查找已有的 VLESS 和 AnyTLS。一台只有 Hysteria2 的机器会被整份覆盖。保护条件加上 `hysteria2`、`hysteria`、`hy2`。已有这些节点时仍然直接退出，不换二进制、不改配置。
4. 安装结束时检查并打印：`ss` 里的实际监听、证书主题和到期日、journal 里的用户加载行数。TCP 端口能连通，不能用来证明 UDP 端口可用，摘要里写明这一点。

## 7. Hysteria2 认证失败要留下日志

`access.log` 的 `accepted` 发生在选择出站之后、拨号之前。认证被拒绝的 Hysteria2 连接到不了这一步，所以日志行数可以一直是 0。这让「UDP 没到」和「密码不对」无法区分。

在 Hysteria2 认证失败的路径打一条 warning：节点标签、来源 IP、结果为拒绝。不记录 UUID，不记录 `Hysteria-Auth` 的内容。成功接受仍走现有 access log。

验收：用错误密码连一次，error log 有这一行，access log 不增加 `accepted`。正确密码仍能代理，并出现原来的接受记录。

## 8. `applyHysteriaALPN` 的收紧

文件：`service/controller/hysteria.go`。

现在的提前返回是对的，因为 `buildTLSSettings` 已经把含 `h3` 的列表写进 `tlsSettings.ALPN`。以后如果有人改了 `buildTLSSettings`，这个提前返回会让 Hysteria2 留下 `h2` 和 `http/1.1`。

改法：显式列表里已经有 `h3` 时，仍然把该列表赋给 `tlsSettings.ALPN`，不要只 return。没有显式列表时继续只写 `h3`。VLESS 和 AnyTLS 不经过这个函数，默认 ALPN 保持不动。

## 9. 建议改动的文件

| 文件 | 改动 |
|------|------|
| `panel/defaultConfig.go` | 默认 `ListenIP`、`SendIP` 改为 `0.0.0.0` |
| `panel/panel.go` | 单个节点 `Start` 失败后退避重试 |
| `service/controller/controller.go` | 续签按到期时间触发；每小时检查 Hysteria2 域名 |
| `service/controller/hysteria.go` | 含 `h3` 的 ALPN 也写回 TLS 配置 |
| Hysteria2 认证路径 | 拒绝时打不含密钥的 warning |
| `deploy/install.sh` | 读取 `server_port`、核对域名、可选伪装站、权限、工作目录、覆盖保护、结束检查 |
| `deploy/HYSTERIA2.md`、`INSTALL.md`、`deploy/README.md` | 示例补上 `SendIP`；写明订阅主机名不在 `/config` 里；防火墙以面板端口为准 |

## 10. 回归

在仓库根目录保留现有测试：

```bash
go test -count=1 -timeout 180s ./service/controller/ ./api/v2board/ \
  -run 'Hysteria|EnableRealityStays|InboundBuilder_Vless|BuildHysteria|AnyTLS'
```

另外补这几项：

- 不写 `SendIP` 的配置，合并默认值后出站是 `0.0.0.0`。
- 显式 `SendIP` 覆盖默认值。
- `Start` 第一次失败、第二次成功，同进程内出现入站。
- 面板 `server_name` 与 `CertDomain` 不一致时，Hysteria2 仍然拒绝启动。
- VLESS 入站仍是 `vless` + `tcp`，AnyTLS 仍是 `anytls` + `tcp`。
- 安装脚本在已有 `NodeType: hysteria2` 时退出，不覆盖 `config.yml`。
- 域名无法解析时，安装脚本不调用 lego。

现网机器上的 v0.9.16 不在这次升级范围内。新机器用改完的 `[5]` 安装；已经装好的机器只替换对应配置项和二进制，不要重跑会覆盖 `config.yml` 的安装脚本。
