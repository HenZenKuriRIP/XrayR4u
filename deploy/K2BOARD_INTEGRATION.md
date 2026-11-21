# K2Board × XrayR4u 对接参考

面向 **K2Board 面板实现** 与节点运维。VLESS、AnyTLS 的 UniProxy 已经接通。Hysteria2 的节点程序已经能拉用户并对外提供协议，面板的配置保护、订阅和节点表单还没有接上。本文是面板要补的契约。

不要新增 UniProxy 路径，也不要给节点增加 JSON 字段。Hysteria2 复用现有 `/config` 与 `/user`。混淆、Brutal、伪装留在节点 `config.yml`，不进面板数据库。

节点程序 **v0.9.19** 内嵌 **xray-core v26.11.9**，可以跑本文的 Hysteria2，也带下面的 REALITY 指纹要求。v0.9.16 二进制仍是 v26.7.11，不要用它。

| 项 | 当前值 |
|---|---|
| 节点程序 | XrayR4u **v0.9.19** |
| 面板协议 | K2Board UniProxy（`/api/v1/server/UniProxy/*`） |
| 内嵌核心 | **xray-core v26.11.9**。v0.9.19 发布二进制与源码一致；v0.9.16 仍是 v26.7.11 |
| 用户体系 | UUID（VLESS / AnyTLS 复用；Hysteria2 把同一个 UUID 当密码） |

> 节点侧关键参数均支持 **config.yml 本地覆盖**（`ControllerConfig` 非空优先于面板）。

---

## 1. 支持的节点形态

| 形态 | 典型用途 | 面板核心字段 | 节点本地可完整覆盖 |
|---|---|---|:---:|
| **A. VLESS + REALITY + Vision** | 直连抗探测（主推） | `tls=2` + `flow` + REALITY 密钥 | 部分（REALITY 密钥仍建议面板下发） |
| **B. VLESS + TLS + XHTTP + CDN + 后量子** | 过 CDN | `tls=1` + `network=xhttp` + path/host | **是**（见 §5） |
| **C. AnyTLS + TLS** | 多路复用 / 填充 | `node_type=anytls` + `tls=1` + 证书域名 | 是 |
| **D. Hysteria2 + TLS** | UDP / QUIC | `node_type=hysteria2` + `tls=1` + 证书域名 | 证书、ALPN、混淆、Brutal 在节点本地 |

后量子（PQ）分层（勿混为一谈）：

| 层 | 作用 | 适用形态 |
|---|---|---|
| REALITY **minClientVer** | 客户端版本门槛 | A |
| REALITY **ML-DSA-65** | 证书额外抗量子签名 | A（可选） |
| REALITY **X25519MLKEM768** | 握手 KEX（dest 支持时自动） | A（无字段） |
| TLS **CurvePreferences** | 源站 TLS 优先 PQ 曲线 | B（节点默认开启） |
| **VLESS Encryption** | 载荷抗量子加密 | A / B（可选，默认关） |

### 1.1 xray-core v26.11.9 对面板的额外要求

形态 A 的 `min_client_ver` 仍建议下发 `1.8.0`。节点在面板和 `config.yml` 都为空时也会写入 `1.8.0`。v26.11.9 **不再**自己把空值填成 `26.3.27`。这个版本号只比较客户端自报版本，**不管**下面的密钥交换。

从 v26.9.8 起，REALITY 服务端拒绝没有 `X25519MLKEM768` 的 ClientHello，并把这种连接当成探测，转到 dest 的真实证书。`min_client_ver` 改不了这件事。同一套密钥下：

| 客户端指纹 | v26.7.11 服务端 | v26.11.9 服务端 |
|---|---|---|
| `chrome`、`firefox`、`safari`、`randomized` | 能连 | 能连 |
| `edge`、`ios`、`qq`、`random` | 能连 | 被拒绝 |
| 未打开混合密钥的 Mihomo | 靠 `1.8.0` 能连 | 被拒绝 |

面板要做的对应改动：

- Clash / Mihomo 的 `reality-opts` 增加 `support-x25519mlkem768: true`。当前 `subscribe_svc.go` 的 `clashProxy` 在 `public-key`、`short-id` 旁边没有这个键，REALITY 分支要写成 `ro["support-x25519mlkem768"] = true`。
- 分享链接和节点表单的 `fp` 只用 `chrome`、`firefox`、`safari`、`randomized`。`vlessLink` 现在把库里的 `fingerprint` 原样写入 `fp`。不在这四项里时改写成 `chrome`。`NodesPage.tsx` 的 `FINGERPRINT_OPTIONS` 仍含 `edge`、`ios`、`android`、`random`，以及 `chrome120`、`chrome140`、`firefox148`、`safari26_3`。后一组没有在 v26.11.9 上验证过，默认列表只留前四项。
- 形态 A 的 dest 仍要能协商 `X25519` 或 `X25519MLKEM768`。这和以前一样。

形态 D 不使用 REALITY，不受这张表影响。

---

## 2. UniProxy API 一览

节点 `ApiConfig.NodeType` 会作为查询参数 `node_type` 发送（如 `vless` / `anytls` / `hysteria2`）。

| 方法 | 路径 | 方向 | 用途 |
|---|---|---|---|
| GET | `/api/v1/server/UniProxy/config` | 面板 → 节点 | 节点端口 / 传输 / TLS / REALITY / 后量子等 |
| GET | `/api/v1/server/UniProxy/user` | 面板 → 节点 | 用户 UUID 列表 |
| POST | `/api/v1/server/UniProxy/push` | 节点 → 面板 | 用户流量 |
| POST | `/api/v1/server/UniProxy/status` | 节点 → 面板 | CPU/内存/磁盘/在线 IP 等 |
| POST | `/api/v1/server/UniProxy/alive` | 节点 → 面板 | 在线用户 IP |
| GET | `/api/v1/server/UniProxy/rule` | 面板 → 节点 | 审计规则（Detect） |
| POST | `/api/v1/server/UniProxy/illegal` | 节点 → 面板 | 违规命中上报（Illegal） |

### 2.1 鉴权（节点 → 面板）

| 参数 | 位置 | 说明 |
|---|---|---|
| `node_id` | Query（或 `X-Node-Id`） | 节点 ID |
| API Key | **`Authorization: Bearer <key>`** 与/或 **`X-Api-Key`** | `ApiConfig.ApiKey`，每节点 bound token |
| `token` | Query | **仅兼容旧节点**；本版本**不再**把密钥放进 URL |
| `node_type` | Query | 如 `vless`、`anytls`、`hysteria2` |

生产环境请使用 Header，避免反向代理 access log / APM 泄露 token。

`config` 另带：`local_port=1`（兼容字段）。

### 2.2 审计规则 — `GET .../rule`

推荐响应：

```json
{
  "version": 1730000000,
  "rules": [
    {
      "id": 1,
      "pattern": "(?i)tracker\\.|announce",
      "name": "bt",
      "action": "reject",
      "severity": 0,
      "privacy": false
    }
  ]
}
```

**面板总开关 `detect_enable`（默认关）：**

- 关闭时：`rules` 恒为空；`POST /illegal` 返回 200 但 **accepted=0、不入库**。
- 开启：管理端「流量审计」页开关，或 settings `detect_enable=true`。
- 节点本地 `RuleListPath` **不受** 总开关影响。

**节点行为：**

1. 用安全编译（长度限制 + 超时）编译 `pattern`，防 ReDoS。
2. 与本地 `ApiConfig.RuleListPath` **合并**（本地规则 `id=-1`）。
3. `version` 未变可跳过重编译；请求失败时沿用上次成功缓存（避免闪断清空规则）。
4. `"rules":[]` 合法：仅本地规则生效；两边都空则不拦截。
5. `ControllerConfig.DisableGetRule: true` 时不拉面板规则。

完整契约：K2Board 仓库 [`docs/UNIPROXY_DETECT_ILLEGAL.md`](../../k2board_anytls/docs/UNIPROXY_DETECT_ILLEGAL.md)（或面板文档同名文件）。

### 2.3 违规上报 — `POST .../illegal`

Body v1（节点默认发送，count 已在本地聚合）：

```json
[
  {"uid": 12, "rule_id": 3, "count": 2},
  {"uid": 88, "rule_id": 1, "count": 1}
]
```

成功响应示例：

```json
{
  "status": "ok",
  "accepted": 2,
  "dropped": 0,
  "schema": 1,
  "kick_uids": [],
  "disable_uids": []
}
```

| 字段 | 含义 |
|---|---|
| `accepted` / `dropped` | 面板写入 / 丢弃条数 |
| `kick_uids` | 可选：断开这些 uid 的**已有连接**（默认 `[]`） |
| `disable_uids` | 可选：面板已禁用的 uid，节点同步剔除（默认 `[]`） |

**节点实现要点：**

- **热路径只拦截 + 本地计数**（`mydispatcher` → `RuleManager.Detect`）；HTTP 仅在周期任务（与 traffic push 同频）。
- 上报失败打错误日志；**仅 HTTP 成功**才打 `Report N illegal behaviors … to panel`。
- 收到 `kick_uids` / `disable_uids`：从本地用户表剔除并 `UpdateUsers` / `RemoveUser`（AnyTLS 踢 mux）。
- 失败可丢弃；**禁止阻塞代理转发**。单次 body 条数上限 1000。
- **不影响**用户订阅 token / 门户 session。

### 2.4 部署顺序（审计功能）

1. **先升级面板**（表 `detect_*` + `/rule` `/illegal` + 管理端「流量审计」）。
2. 管理端添加规则（可选）→ **打开总开关**（默认关，不开则不审查）。
3. **再升级节点二进制**并重启（Header 鉴权 + ReportIllegal）。
4. 确认日志：`Report N illegal behaviors … to panel`；**不要**默认开 `detect_auto_ban`。

旧节点忽略 `/rule`、不上报 `/illegal` 仍可正常代理；新节点 + 总开关开启后审计才完整。

---

## 3. 节点配置 API：`GET .../config`

### 3.1 形态 A — VLESS + REALITY + Vision（推荐直连）

```json
{
  "server_port": 443,
  "network": "tcp",
  "tls": 2,
  "flow": "xtls-rprx-vision",
  "decryption": "none",
  "tls_settings": {
    "server_name": "www.microsoft.com",
    "dest": "www.microsoft.com",
    "server_port": "443",
    "private_key": "<xray x25519 PrivateKey>",
    "public_key": "<PublicKey>",
    "short_id": "abcd",
    "fingerprint": "chrome",
    "min_client_ver": "1.8.0",
    "mldsa65_seed": "",
    "show": false
  },
  "base_config": {
    "push_interval": 60,
    "pull_interval": 60
  }
}
```

### 3.2 形态 B — VLESS + TLS + XHTTP（CDN）

> 节点侧已支持用 config.yml **完全覆盖** transport/security/path（不依赖面板）。  
> 面板若要可视化与订阅正确，建议按下表返回。

```json
{
  "server_port": 443,
  "network": "xhttp",
  "tls": 1,
  "flow": "",
  "decryption": "none",
  "host": "cdn.example.com",
  "path": "/vless-cdn",
  "tls_settings": {
    "server_name": "cdn.example.com"
  },
  "network_settings": {
    "host": "cdn.example.com",
    "path": "/vless-cdn",
    "mode": "auto"
  },
  "base_config": {
    "push_interval": 60,
    "pull_interval": 60
  }
}
```

说明：

- 当前节点解析 **优先读** `network`、`tls`、`flow`、`decryption`。  
- `host` / `path` / `network_settings`：**面板建议提供**；节点在未接面板字段前可用 `ControllerConfig.XHTTP` 覆盖（见 §5）。  
- CDN 场景 **`flow` 建议空**（Vision 默认 off）。  
- 源站证书在 **节点 config.yml 的 CertConfig**，不由 UniProxy 下发私钥文件。

### 3.3 形态 C — AnyTLS

```json
{
  "server_port": 443,
  "network": "tcp",
  "tls": 1,
  "tls_settings": {
    "server_name": "anytls.example.com"
  },
  "base_config": {
    "push_interval": 60,
    "pull_interval": 60
  }
}
```

- `node_type=anytls`（节点 ApiConfig.NodeType）  
- **不要** 返回 REALITY 字段（`private_key` / `public_key` / `short_id` / `flow`）  
- 用户 UUID 作 AnyTLS 密码  

### 3.4 形态 D — Hysteria2

节点查询参数 `node_type` 使用 `hysteria2`。节点也接受 `hysteria`、`hy2`，面板写入和判断时统一用 `hysteria2`。

`GET /config` 的成功响应就是下面这一份。`network` 可以仍是 `tcp`，节点会忽略它，实际监听 **UDP**。

```json
{
  "server_port": 443,
  "network": "tcp",
  "tls": 1,
  "tls_settings": {
    "server_name": "hy2.example.com"
  },
  "base_config": {
    "push_interval": 60,
    "pull_interval": 60
  }
}
```

必须同时满足：

| 项 | 要求 |
|---|---|
| `tls` | 必须是 `1`。`0` 或 `2` 时节点拒绝启动 |
| `flow` | 省略或空字符串。不要下发 `xtls-rprx-vision` |
| `decryption` | 不要下发。这不是 VLESS |
| `tls_settings` | 只留 `server_name`，值为证书域名，必须和节点 `CertDomain` 相同。两边都有值且不一致时，节点拒绝启动。不要带 `private_key`、`public_key`、`short_id`、`dest`、`min_client_ver`、`mldsa65_seed` |
| `host` / `path` | 不要下发。Hysteria2 不走 XHTTP、WebSocket、gRPC |
| 用户 | 现有 `/user`。`uuid` 就是 Hysteria2 密码，不用新用户字段 |

当前面板的 `buildUniProxyConfig`（`internal/handlers/server/uniproxy.go`）只对 `anytls` 跳过 REALITY。`node_type=hysteria2` 且库里 `tls=2` 时，仍会把 REALITY 密钥写进响应，节点会报 `Hysteria2 does not support REALITY` 并起不来。面板要把它和 `anytls` 同等处理：识别 `hysteria2` / `hysteria` / `hy2` 后强制响应为 `tls=1`、空 `flow`、无 `decryption`、无 REALITY 字段，`server_name` 用节点 SNI。

节点上的证书、ALPN `h3`、Salamander、Brutal、伪装由 `ControllerConfig` 和 `Hysteria2` 块负责，见 [HYSTERIA2.md](HYSTERIA2.md)。面板不保存、不下发这些值。默认节点配置是：无混淆、无 Brutal、伪装 404。订阅按这个默认生成。运维若在节点打开了 `ObfsPassword`，必须把**同一个**密码另外配进订阅；本文的基础对接不包含这个可选项。不要生成端口跳跃（`mport` / `ports`），节点没有听端口段。

节点配置里的 `ApiConfig.NodeType` 必须是 `hysteria2`，`EnableReality` 必须是 `false`。这是节点本机 `config.yml`，不是 UniProxy 字段。

---

## 4. `/config` 字段总表

### 4.1 顶层

| 字段 | 类型 | 必填 | 说明 |
|---|---|:---:|---|
| `server_port` | int | 是 | 监听端口 |
| `network` | string | 否 | `tcp`（默认）/ `xhttp` / `splithttp` / `ws` / `grpc` 等 |
| `tls` | int | 是 | `0` 无 / `1` TLS / `2` REALITY |
| `flow` | string | 否 | `xtls-rprx-vision` 或空 |
| `decryption` | string | 否 | VLESS Encryption 服务端串；默认等价 `none`。亦接受 `vless_decryption` |
| `host` | string | CDN 推荐 | XHTTP/WS Host（节点本地 `XHTTP.Host` 可覆盖） |
| `path` | string | CDN 推荐 | XHTTP/WS Path（节点本地 `XHTTP.Path` 可覆盖） |
| `base_config.push_interval` | int | 否 | 推送周期提示 |
| `base_config.pull_interval` | int | 否 | 拉取周期提示 |

### 4.2 `tls_settings`（REALITY / TLS 共用容器）

| 字段 | 形态 | 说明 |
|---|---|---|
| `server_name` | A/B/C/D | REALITY 的 SNI 列表元素；TLS / Hysteria2 的证书域名 |
| `dest` | A | REALITY 回落目标；空则用 server_name |
| `server_port` | A | dest 端口，默认 `443` |
| `private_key` | A | `xray x25519` 私钥 |
| `public_key` | A | 公钥（订阅 pbk） |
| `short_id` | A | shortId |
| `fingerprint` | A | 默认 `chrome` |
| `min_client_ver` | A | **强烈建议 `1.8.0`**；亦接受 `minClientVer` |
| `mldsa65_seed` | A 可选 | ML-DSA-65 seed；亦接受 `mldsa65Seed` |
| `show` | A 可选 | REALITY 调试日志 |

### 4.3 后量子相关

| 能力 | 面板动作 | 默认 |
|---|---|---|
| minClientVer | 下发 `min_client_ver: "1.8.0"` | 节点兜底 `1.8.0`。v26.11.9 不再自己填 `26.3.27` |
| ML-DSA-65 | 可选 `mldsa65_seed`；订阅下发 **Verify 公钥** | 关 |
| X25519MLKEM768（REALITY） | 订阅 Clash 写 `reality-opts.support-x25519mlkem768: true`；`fp` 不用 edge/ios/qq/random | 服务端从 v26.9.8 起强制客户端 ClientHello 携带 |
| VLESS Encryption | 顶层 `decryption`；订阅下发客户端 **encryption** | `none` |
| TLS 曲线 PQ（CDN 源站） | 无需面板；节点 CertConfig 默认含 X25519MLKEM768 | 节点默认 |

生成命令（**节点自带，与内嵌 core 同版本**；也可在面板一键生成）：

```bash
XrayR tools x25519      # REALITY 密钥对
XrayR tools mldsa65     # ML-DSA Seed / Verify
XrayR tools vlessenc    # VLESS Encryption 的 decryption + encryption 成对输出
# 短写: XrayR x25519 | XrayR mldsa65 | XrayR vlessenc
```

安装脚本在部署末尾可选生成并保存到 `/etc/XrayR/keys/`。

---

## 5. 节点本地覆盖（config.yml）与面板优先级

**规则：节点 `ControllerConfig` 非空字段优先于面板。**

| 节点配置项 | 覆盖对象 | 典型用途 |
|---|---|---|
| `Transport` | `network` | 强制 `xhttp` 做 CDN |
| `Security` | `tls` 类型 | 强制 `tls` / `reality` / `none` |
| `Vision` | Vision 开关 | `on` / `off` / `auto` |
| `XHTTP.Host/Path/Mode` | XHTTP | CDN 完整落地 |
| `CertConfig` | 源站证书 + ALPN/曲线 | TLS / CDN Full Strict |
| `RealityMinClientVer` | minClientVer | 兼容/收紧客户端 |
| `RealityMldsa65Seed` | mldsa65Seed | 本地开 ML-DSA |
| `RealityShow` | show | 调试 PQ KEX |
| `VlessDecryption` | decryption | 本地开 VLESS Encryption |
| `EnableFallback` | fallback | 与 Encryption 互斥 |

### CDN 节点推荐本地片段（面板未改完时也能跑）

```yaml
ControllerConfig:
  Transport: "xhttp"
  Security: "tls"
  Vision: "off"
  XHTTP:
    Host: "cdn.example.com"
    Path: "/vless-cdn"
    Mode: "auto"
  CertConfig:
    CertMode: file
    CertFile: /etc/XrayR/cert/cdn.example.com.crt
    KeyFile: /etc/XrayR/cert/cdn.example.com.key
  VlessDecryption: ""          # 可选: xray vlessenc 的 decryption
  EnableFallback: false
```

更细的 CDN 运维说明：[`TLS_XHTTP_CDN_PQ.md`](TLS_XHTTP_CDN_PQ.md)。

---

## 6. 用户 API：`GET .../user`

```json
{
  "users": [
    {
      "id": 1,
      "uuid": "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
      "email": "user@example.com",
      "speed_limit": 0
    }
  ]
}
```

| 字段 | 说明 |
|---|---|
| `id` | 用户 UID（流量/在线上报用） |
| `uuid` | VLESS id / AnyTLS 密码 / Hysteria2 密码 |
| `email` | 展示名；可空（节点回退为 uuid） |
| `speed_limit` | 可选，Mbps；`0` 表示不限（仍受节点全局 SpeedLimit 影响） |

**后量子密钥在节点级，不在用户级。** 用户列表格式无需为 PQ 改动。

---

## 7. 上报 API（格式保持不变）

### 7.1 流量 `POST .../push`

Body：`{ "<user_id>": [upload_bytes, download_bytes], ... }`

### 7.2 状态 `POST .../status`

```json
{
  "cpu": 12.5,
  "mem": 40.0,
  "disk": 55.0,
  "uptime": 86400,
  "active_conns": 42
}
```

### 7.3 在线 `POST .../alive`

Body：`{ "<uid>": ["1.2.3.4", "5.6.7.8"], ... }`

---

## 8. 订阅 / 分享链接（面板下发给客户端）

| 场景 | 客户端必要字段 |
|---|---|
| REALITY + Vision | address, port, uuid, `flow`, `pbk`, `sid`, `sni`, `fp`, security=reality, network=tcp |
| + ML-DSA | 额外 `mldsa65Verify`（公钥，非 seed） |
| TLS + XHTTP + CDN | address=CDN 域名, port, uuid, security=tls, network=xhttp, host, path, sni, alpn, fp；**一般不带 flow** |
| + VLESS Encryption | 额外 `encryption` = `xray vlessenc` 客户端串（与服务端 decryption 配对） |
| AnyTLS | 按 AnyTLS 客户端规范；密码=uuid；TLS 域名证书 |
| Hysteria2 | 见 §8.1。参数是 `sni` 和 `alpn=h3`，不要写 `obfs`、`insecure`、`fp`。旁边这份 K2Board 源码的订阅分支还没有 `hysteria2` 时，链接为空 |
| REALITY + v26.11.9 | `fp` 限 `chrome` / `firefox` / `safari` / `randomized`。Clash 的 `reality-opts` 增加 `support-x25519mlkem768: true` |

服务端 `decryption` 与客户端 `encryption` **成对**，不可混用不同生成结果。

### 8.1 Hysteria2 订阅

地址用节点的公网域名：有 `host` 用 `host`，否则用 `sni`。端口用 `server_port`。密码是用户 `uuid`，放进 URL 前做百分号编码。SNI 必须等于证书域名。不要写 `insecure=1`、`skip-cert-verify: true`、`fp`、`flow`、`pbk`。

分享链接，Shadowrocket 走这一条：

```text
hysteria2://<urlencode(uuid)>@hy2.example.com:443?sni=hy2.example.com&alpn=h3#<urlencode(节点名)>
```

Clash / Mihomo（`clashProxy` 新增 `hysteria2` 分支）。原版 Clash Premium 不认识 `type: hysteria2`。

```yaml
- name: hy2-example
  type: hysteria2
  server: hy2.example.com
  port: 443
  password: 11111111-1111-1111-1111-111111111111
  sni: hy2.example.com
  alpn:
    - h3
  skip-cert-verify: false
```

不要写 `obfs`、`up`、`down`、`ports`。节点默认没有 Salamander 和 Brutal，也不做端口跳跃。写了 `obfs` 却没有和节点相同的 `obfs-password`，客户端会认证失败。

Surge 5 在 `surgeLine` 增加：

```text
hy2-example = hysteria2, hy2.example.com, 443, password=<uuid>, sni=hy2.example.com, alpn=h3
```

sing-box 若以后要出站，对应块是 `type: hysteria2`，`password` 为 UUID，`tls.enabled=true`，`tls.server_name` 为证书域名，`tls.alpn` 为 `["h3"]`。当前面板没有 sing-box 订阅，不作为这次的必改项。

### 8.2 面板要改的三处

现有 `switch` 都是精确字符串，`anytls` 能命中是因为库存的就是小写 `anytls`。Hysteria2 入库、比较、分支一律用 `hysteria2`。保存时把 `hysteria`、`hy2` 规范成 `hysteria2`，否则订阅分支对不上。

**`internal/handlers/server/uniproxy.go` 的 `buildUniProxyConfig`**

在现有 `isAnyTLS` 旁边增加同样的判断，三个名字都认。命中后：

- 响应 `tls` 固定为 `1`，即使库里是 `0` 或 `2`
- `flow` 为空，不写 `decryption`
- 不写 `host`、`path`、`network_settings`
- 不进入 `node.TLS == 2 && !isAnyTLS` 那段，因此没有 `private_key`、`public_key`、`short_id`、`dest`、`min_client_ver`、`mldsa65_seed`
- `tls_settings.server_name` 只用节点 `sni`

**`internal/services/subscribe_svc.go`**

三处 `switch node.NodeType` 各加 `case "hysteria2"`，输出按 §8.1。地址沿用现有写法：`host` 非空用 `host`，否则用 `sni`。`buildNodeLink` 加了之后，Shadowrocket 不用再改，它直接复用这个函数。

REALITY 的 `vlessLink` 与 `clashProxy` 同时改指纹和混合密钥，见 §1.1。Clash 写在已有的 `reality-opts` 里：

```yaml
reality-opts:
  public-key: <public_key>
  short-id: <short_id>
  support-x25519mlkem768: true
```

有 ML-DSA 时，`mldsa65-public-key` 仍照旧附在这个 map 上。

**`web/src/views/NodesPage.tsx`**

照着现有 AnyTLS 模板加一条形态 D，不要走自定义里手填 `tls=2`。

| 位置 | 改法 |
|---|---|
| `ProfileId` / `PROFILES` | 增加 `hysteria2`。标题可用 `D · Hysteria2 · TLS`。说明写清：UDP，UUID 作密码，证书在节点机 |
| `applyProfile('hysteria2')` | `node_type=hysteria2`，`network=tcp`，`tls=1`，`flow=""` |
| `onNodeTypeChange` | 输入 `hysteria2` 时同样强制 `tls=1`、清空 `flow` |
| `detectProfile` / `lineTypeLabel` / `typeLabel` | `hysteria2` 显示为 `D · Hysteria2`，不要落进 REALITY 或自定义 |
| 表单 | 必填名称、端口、`sni`（证书域名）。`host` 可空，空则订阅用 `sni`。不展示 REALITY 密钥、`flow`、VLESS decryption、path |
| 指纹下拉 | 只留 `chrome`、`firefox`、`safari`、`randomized`，默认 `chrome` |

保存结果就是：`node_type=hysteria2`，`tls=1`，`sni` 为证书域名，不写 `reality_settings`。节点本机另配证书、`EnableReality: false` 和 UDP 放行，见 [HYSTERIA2.md](HYSTERIA2.md)。

---

## 9. 面板产品策略建议

| 产品线 | 配置要点 |
|---|---|
| **兼容默认** | REALITY + Vision + `min_client_ver=1.8.0`；指纹用 chrome；Clash 打开 `support-x25519mlkem768` |
| **直连增强** | 上 + 可选 ML-DSA；dest 选支持 `X25519` 或 `X25519MLKEM768` 的站点 |
| **CDN 线路** | TLS + XHTTP + path；Vision 关；可选 VLESS Encryption |
| **高安全** | 独立节点开 VLESS Encryption；仅推给支持的客户端 |
| **Hysteria2** | 独立节点，`node_type=hysteria2`，`tls=1`，`sni` 为证书域名；订阅按 §8.1 |

**禁止组合：**

- `decryption != none` **且** 节点 Fallback 开启 → 节点拒绝启动  
- REALITY 与传统 CDN 同路径混用（不推荐）
- Hysteria2 配 `tls=2`、空证书，或把 VLESS 的 `flow` / XHTTP path 塞进 `/config`
- Hysteria2 订阅带 `fp`、`insecure=1`，或在节点未开混淆时写 `obfs`

---

## 10. 对接完成状态（参考）

VLESS / AnyTLS 已经覆盖。Hysteria2 和 v26.11.9 的 REALITY 指纹还要补下面这些，补完才算对接完成：

- REALITY：`min_client_ver=1.8.0`、密钥、`flow`、可选 `mldsa65_seed` + 订阅 Verify
- REALITY 订阅：`fp` 不用 `edge` / `ios` / `qq` / `random`；Clash `reality-opts.support-x25519mlkem768: true`
- CDN / XHTTP：`network=xhttp`、host/path、TLS
- 可选 VLESS Encryption：`decryption` + 订阅 `encryption`
- AnyTLS：`node_type=anytls`、`tls=1`
- Hysteria2：`/config` 按 §3.4；分享链接、Clash、Surge 按 §8.1；管理页模板按 §8.2
- 用户 / push / status / alive 格式不变
- 审计：`GET /rule`、`POST /illegal`；鉴权 Header（Bearer / X-Api-Key）

节点侧由运维补齐：v26.11.9 二进制、Hysteria2 证书与 UDP 端口、`EnableReality: false`、CDN 的 `Transport/XHTTP`。不要为了加 Hysteria2 去改已经在跑的 VLESS / AnyTLS 节点条目。

---

## 11. 节点 config.yml 与面板分工

```text
┌─────────────────────────────────────────────────────────┐
│  K2Board 面板                                            │
│  · 用户 UUID / 套餐 / 订阅序列化                           │
│  · 节点：端口、协议形态、REALITY 密钥、path、decryption 等  │
└───────────────────────────┬─────────────────────────────┘
                            │ UniProxy HTTP API
┌───────────────────────────▼─────────────────────────────┐
│  XrayR4u 节点                                            │
│  · 拉 config/user/rule，推 traffic/status/alive/illegal    │
│  · ControllerConfig 可覆盖传输/TLS/XHTTP/PQ                 │
│  · 证书、限速、本地 rulelist、Fallback、监听 IP 等           │
│  · 驱动 xray-core 26.11.9                                  │
└─────────────────────────────────────────────────────────┘
```

| 归面板 | 归节点 |
|---|---|
| 用户与订单 | 证书文件路径 / ACME |
| 订阅链接 | ListenIP / 限速 / 设备限制 |
| REALITY 密钥材料（建议） | XHTTP/TLS 本地强制覆盖（CDN 救急） |
| 线路类型选择 | 日志、Fallback、PROXY protocol |
| 审计规则库 / 事件记账 / 策略 kick·ban | 本地拦截 + 周期上报；可选 `RuleListPath` |

---

## 12. 故障速查

| 现象 | 处理 |
|---|---|
| 升级到 v26.11.9 后 REALITY 拿到真实证书 | 客户端没有 `X25519MLKEM768`。Clash 打开 `support-x25519mlkem768`；`fp` 改为 chrome / firefox / safari / randomized。`min_client_ver` 解决不了这个现象 |
| Mihomo 报版本过低 | 下发 `min_client_ver: "1.8.0"` 或节点 `RealityMinClientVer` |
| Hysteria2 节点不起 | `/config` 的 `tls` 必须是 1，且不能带 REALITY 字段；节点要有证书。日志里的 `does not support REALITY` 就是 `tls=2` |
| 订阅里没有 Hysteria2 | 面板的 `buildNodeLink` / `clashProxy` / `surgeLine` 还没有 `hysteria2` 分支时，未知类型会返回空。节点不生成这条链接 |
| Hysteria2 密码错误 | 客户端密码必须是该用户的 UUID，不是节点 ApiKey，也不是混淆密码 |
| CDN 无流量 | 查 path/host、源站证书、CDN SSL=Full Strict、节点 `Transport/XHTTP` |
| 开 Encryption 启动失败 | 关闭 Fallback；检查 decryption 串（`xray vlessenc`） |
| ML-DSA 后仅新客户端能连 | 订阅必须带 `mldsa65Verify`，或关闭 seed |
| 想确认 REALITY 是否 PQ KEX | 节点 `RealityShow: true` 看日志 |
| 审计不上报 / 无规则 | 确认面板已部署 `/rule` `/illegal`；节点日志 `ReportIllegal failed`；`DisableGetRule` 是否误开 |
| 日志无 `Report N illegal…` | 无命中时正常；有命中应见 to panel，失败见 failed |
| 节点 401/403 | 检查 Header 鉴权与节点 ApiKey；勿再依赖 query `token` |

---

## 13. 相关文档

| 文档 | 内容 |
|---|---|
| 本文 | **K2 对接参考（API / 字段）** |
| [TLS_XHTTP_CDN_PQ.md](TLS_XHTTP_CDN_PQ.md) | 节点侧 CDN 栈运维与 yml |
| [README.md](README.md) | VPS 部署与安装脚本 |
| [../INSTALL.md](../INSTALL.md) | 安装详解 |
| [../ARCHITECTURE.md](../ARCHITECTURE.md) | 代码架构 |
| [HYSTERIA2.md](HYSTERIA2.md) | 节点侧 Hysteria2：配置、证书、上线与回滚 |

---

*XrayR4u v0.9.19 · xray-core v26.11.9 · REALITY/Vision/PQ + TLS/XHTTP/CDN + AnyTLS + Hysteria2 · UniProxy 无新字段*
