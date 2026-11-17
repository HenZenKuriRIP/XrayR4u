# 节点支持 Hysteria2

本文说明 XrayR4u 如何在**不改 UniProxy 接口、不改 VLESS / AnyTLS 行为**的前提下接入 Hysteria2，以及怎样加到已经在跑的节点上。

面板订阅链接（`hysteria2://...`、Clash `type: hysteria2`）不在这次改动里。节点能同步用户并对外提供协议；客户端链接需要以后在 K2Board 订阅里另加，或先手工下发。

## 1. 结论

内嵌 xray-core v26.11.9 已经实现 Hysteria2（配置里 `version` 必须为 2）：

- 传输：`transport/internet/hysteria`（QUIC，ALPN `h3`）
- 代理：`proxy/hysteria`（HTTP/3 `POST https://hysteria/auth`，头 `Hysteria-Auth`，成功码 233，TCP 帧 `0x401`）
- 多用户：`proxy.UserManager` 的 `AddUser` / `RemoveUser`，和 VLESS 同一条增量同步路径

节点以前只注册了传输，没有按 `NodeType` 建这个入站。这次补的是协议分支，不是新的一层。

`api.API` 的方法没有增加。UniProxy 仍然是：

| 调用 | 节点使用的字段 |
|------|----------------|
| `GET /config` | `server_port`、`tls=1`、`tls_settings.server_name` |
| `GET /user` | `id`、`uuid`、`email`、`speed_limit`、`device_limit` |
| 状态 / 在线 / 流量 / `/rule` / `/illegal` | 与 VLESS、AnyTLS 相同 |

密码就是用户 UUID。标准客户端 URI：

```text
hysteria2://<uuid>@<host>:<port>?sni=<证书域名>
```

## 2. 为什么不会破坏已有 VLESS 和 AnyTLS

协议选择仍然只看本节点 `ApiConfig.NodeType`。

| NodeType | 入站 | 用户下发 | 这次是否改行为 |
|----------|------|----------|----------------|
| `Vless`（及其他非 anytls / 非 hysteria 值） | 原来的 VLESS 分支 | `buildVlessUser` + `UserManager` | 否 |
| `anytls` | 原来的 AnyTLS 分支 | `UpdateUsers` 全量替换 | 否 |
| `hysteria2` / `hysteria` / `hy2` | 新的 Hysteria2 分支 | `UserManager`，账号是 UUID | 新增 |

具体隔离：

1. **提前返回。** `InboundBuilder` 在调用共用的 `buildStreamSetting` 之前就识别 Hysteria2。面板上的 `network=xhttp`、本地 `Transport: ws`、空的 XHTTP Path 都不会进入 VLESS/CDN 的传输构建，也不会把 VLESS 的默认 ALPN（`h2`、`http/1.1`）改掉。
2. **ALPN 只在 Hysteria2 上改成 `h3`。** xray-core 仅在 TLS 的 ALPN 列表为空时才自动填 `h3`。共用的 `buildTLSSettings` 在 ALPN 为空时仍是 `h2` + `http/1.1`，VLESS CDN 和 AnyTLS 继续用这个默认。
3. **`EnableReality: true` 不再套到 Hysteria2 和 AnyTLS。** 这个开关以前就会跳过 AnyTLS。现在同样跳过 Hysteria2。`NodeType: Vless` 且 `EnableReality: true` 时，仍然把 `tls=1` 提升成 REALITY，和改前一致。
4. **用户键不变。** 邮箱仍是 `tag|email|uid`。限速、在线 IP、流量计数、审计踢线沿用现有 `mydispatcher`。Hysteria2 没有走 AnyTLS 那条 `UpdateUsers` 全量替换。
5. **新配置块默认不生效。** `ControllerConfig.Hysteria2` 只在 Hysteria2 入站里读取。VLESS / AnyTLS 节点不写这个块，行为与现在相同。
6. **已有节点配置不用改。** 不把正在服务的节点 `NodeType` 改成 `hysteria2`。新协议用**新的** `Nodes[]` 条目、**新的端口**、**自己的证书域名**。

Hysteria2 删用户后，新连接会立刻失败。已经建立的 QUIC 会话要等到空闲超时才断开。这和「只从认证表删除」一致，不会去动 VLESS / AnyTLS 的连接。

## 3. 代码改动

| 文件 | 作用 |
|------|------|
| `main/distro/all/all.go` | 空白导入 `proxy/hysteria`，注册入站工厂。传输导入原来就有。 |
| `api/apimodel.go` | `IsHysteriaNode`：只认 `hysteria2`、`hysteria`、`hy2`。 |
| `api/v2board/v2board.go` | `EnableReality` 不覆盖上述 NodeType。 |
| `service/controller/inboundbuilder.go` | Hysteria2 在共用传输构建之前返回。 |
| `service/controller/hysteria.go` | 入站、TLS/`h3`、可选伪装 / Salamander / Brutal、用户账号。 |
| `service/controller/userbuilder.go` | `buildHysteriaUser`。 |
| `service/controller/controller.go` | `addNewUser` 在 AnyTLS 判断之后、VLESS 判断之前增加 Hysteria2。增量删除仍走原来的 `removeUsers`。 |
| `service/controller/config.go` | 可选 `Hysteria2` 本地配置。 |
| `service/controller/hysteria_test.go` | 构建隔离测试和本机 QUIC 握手。 |

没有改 `panel.Start`、`mydispatcher`、限速器或路由。

## 4. 运行时行为

1. `ApiConfig.NodeType` 为 `hysteria2`。
2. `GET /config` 得到端口，且 `tls` 必须是 `1`。`tls=0` 或 `tls=2`（REALITY）会在入站构建时直接失败，不会偷偷听一个明文或 REALITY 端口。
3. 传输固定为 `hysteria`，忽略面板 `network` 和本地 `Transport`。
4. 证书走现有 `CertConfig`（`file` / `http` / `dns`）。未手写 ALPN 时，这个入站使用 `h3`。
5. 入站先以空用户表启动，随后 `GET /user`，把每个 UUID 加进认证表。
6. 之后的增删和 VLESS 一样按 UID diff。UUID 变化会先删后加。
7. 未认证的 QUIC 连接走伪装。默认是 HTTP 404。可选 `file`、`proxy`、`string`。
8. 未配置带宽时用 BBR。需要 Brutal 时在本地写 `Up` / `Down`。

可选、且**不在面板 JSON 里**的项：

| 配置 | 空值含义 |
|------|----------|
| `Hysteria2.ObfsPassword` | 关闭 Salamander。客户端若开了混淆，必须填同一个密码。 |
| `Hysteria2.Congestion` / `Up` / `Down` | 不写则走核心默认（未设带宽时是 BBR）。`brutal` 需要上下行带宽，例如 `100 mbps`。 |
| `Hysteria2.MasqType` | `404`。 |
| `Hysteria2.UdpIdleTimeout` | 60 秒，允许范围 2–600。 |

端口跳跃（UDP hop）这次没有做。它会多占端口，不适合在已经跑着 VLESS/AnyTLS 的机器上默认打开。

## 5. 节点配置示例

在现有 `Nodes` 列表**追加**一项，不要改已经在服务的条目。端口不要和 VLESS、AnyTLS 或本机其他 UDP 服务重合。Hysteria2 听的是 UDP。

```yaml
  - PanelType: "V2board"
    ApiConfig:
      ApiHost: "https://your-panel.com"
      ApiKey: "your-api-token"
      NodeID: 5
      NodeType: hysteria2
      EnableReality: false
      Timeout: 30
    ControllerConfig:
      ListenIP: 0.0.0.0
      UpdatePeriodic: 60
      CertConfig:
        CertMode: file
        CertDomain: "hy2.example.com"
        CertFile: /etc/XrayR/cert/hy2.example.com.crt
        KeyFile: /etc/XrayR/cert/hy2.example.com.key
        ALPN: ["h3"]
      # 下面整段都可以不写
      # Hysteria2:
      #   MasqType: "404"
      #   ObfsPassword: ""
      #   Congestion: ""
      #   Up: ""
      #   Down: ""
```

面板侧这条节点保持现有记录即可：`node_type` 写成 `hysteria2`（后台这个字段是普通字符串），`tls=1`，端口与 SNI 填证书域名。不需要新的 UniProxy 字段。管理页下拉框目前没有 Hysteria2 模板，可以用现有创建接口写入 `node_type`，或等面板表单后续再加。

防火墙放行该 UDP 端口。证书域名必须能被客户端校验。

### 5.1 安装脚本

`deploy/install.sh` 的协议菜单：

| 选项 | 行为 |
|---|---|
| `[1]` VLESS + REALITY | 与原来相同 |
| `[2]` AnyTLS + TLS | 与原来相同 |
| `[3]` VLESS + AnyTLS | 与原来相同 |
| `[4]` Hysteria2 + TLS | 新机器上只写一条 `NodeType: hysteria2` |

`[4]` 会询问域名和证书（已有文件，或 HTTP-01），并写入 `EnableReality: false`、`ALPN: ["h3"]`。混淆和 Brutal 保持关闭。

两条限制：

- 已发布的 **v0.9.16** 没有 Hysteria2。选 `[4]` 时，脚本**不会**下载这个 Release。把包含本分支的二进制放在脚本同目录，文件名是 `XrayR`。
- 脚本生成配置时会**整份覆盖** `/etc/XrayR/config.yml`。若文件里已经有 `NodeType: Vless` 或 `anytls`，`[4]` 会直接退出，不替换二进制、不改配置。已经在跑的机器按下一节追加节点，不要重跑安装脚本。

## 6. 上线到正在运行的服务器

1. 在测试机编译，确认下面的测试通过。
2. 备份服务器上的 `/etc/XrayR/config.yml` 和当前二进制。不要对这台机器重跑 `install.sh`。
3. 替换二进制。**先不要改配置**，重启一次。原有 VLESS / AnyTLS 节点应照常拉用户、报流量。此时期望日志里没有 Hysteria2 入站。
4. 确认旧节点正常后，再追加第 5 节的新节点并重启（或等配置热重载）。
5. 看日志出现 `[hysteria2: <id>] Start monitor node status`，以及添加用户的条数。
6. 用一条测试 UUID 从客户端连 `hysteria2://<uuid>@域名:端口?sni=域名`。错误密码应失败。
7. 再检查旧节点的在线人数和流量仍在上报。

热重载会按现有逻辑重建**有变化**的节点。只追加新 `Nodes[]` 时，旧节点的入站标签不变，不会因为这次代码被拆掉重来；重启进程则会全部重新监听，这是现有行为，与协议无关。重启窗口要选在可接受的时间。

## 7. 回滚

- 配置回滚：删掉新的 `Nodes[]` 条目，重启。VLESS / AnyTLS 配置没有被这次改动依赖。
- 二进制回滚：换回上一版二进制。新二进制才认识 `NodeType: hysteria2`。若配置里还留着该条目，旧二进制会把它当成 VLESS 去建（历史默认分支）。回滚二进制之前先删掉这条节点。

## 8. 本地测试

在仓库根目录：

```bash
go test -count=1 -timeout 180s ./service/controller/ ./api/v2board/ \
  -run 'Hysteria|EnableRealityStays|InboundBuilder_Vless|BuildHysteria|AnyTLS'
```

覆盖：

- UUID 映射到 `Hysteria-Auth`，邮箱键为 `tag|email|uid`；空 UUID 拒绝。
- `NodeType: hysteria2` 且面板 `network=xhttp`、本地 `Transport: ws`、Path 为空时，入站仍是 `hysteria`，并带 Salamander 掩码。
- REALITY、`Security: none`、未开 TLS 都会拒绝启动。
- `Vless` 入站仍是 `vless` + `tcp`；`anytls` 入站仍是 `anytls` + `tcp`。
- `EnableReality: true` 对 `vless` 仍提升为 REALITY；对 `anytls` 和 `hysteria2` 保持 `tls`。
- 本机自签证书上完成一次 Hysteria2 QUIC 握手：正确密码成功，错误密码失败。

`api/v2board/v2board_test.go` 里连 `localhost:9897` 的用例，以及 `TestBuildV2ray` / `TestBuildTrojan` / `TestBuildSS` / `common/legocmd` 的 ACME 用例，依赖外部面板或真实证书申请，不作为这次的通过条件。

## 9. 这次故意没做

- 不改 UniProxy 路径和 JSON。
- 不在 K2Board 订阅里生成 Hysteria2 链接。
- 不把混淆、Brutal、伪装写进面板数据库。
- 不做 UDP 端口跳跃。
- 不改 VLESS 默认 ALPN、Vision、REALITY 或 AnyTLS Fallback。
