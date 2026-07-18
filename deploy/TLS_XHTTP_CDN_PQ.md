# 节点侧完整落地：TLS + XHTTP + CDN + 后量子

本文说明 **仅改节点 config.yml**（不依赖面板改接口）即可启用：

```text
VLESS + TLS + XHTTP + CDN + 后量子
```

后量子在本组合中的含义：

| 层 | 能力 | 节点配置 |
|---|---|---|
| TLS 握手 | **X25519MLKEM768** 曲线偏好（默认开启） | `CertConfig.CurvePreferences` |
| VLESS 载荷 | **VLESS Encryption**（`mlkem768x25519plus`） | `VlessDecryption` |
| REALITY PQ | ML-DSA / REALITY-MLKEM | **不适用**（本组合用 TLS，不是 REALITY） |

---

## 1. 架构（CDN Full / Strict）

```text
客户端
  │  TLS (SNI=你的域名) + XHTTP path
  ▼
CDN (Cloudflare / 其它)
  │  回源 TLS (Full Strict) 或 HTTP
  ▼
节点 XrayR4u 源站
  VLESS inbound
  network = xhttp
  security = tls          ← Full Strict
  decryption = PQ 串或 none
```

| CDN SSL 模式 | 节点 `Security` | 说明 |
|---|---|---|
| **Full (strict)** | `tls` + 有效证书 | **推荐**，本文默认 |
| Full | `tls` + 任意证书 | 可用但不严格校验 |
| Flexible | `none`（源站明文 HTTP+XHTTP） | 源站无 TLS；CDN→源不加密，**不推荐** |

---

## 2. 最小可运行 config.yml（节点本地完整驱动）

```yaml
Nodes:
  - PanelType: "V2board"
    ApiConfig:
      ApiHost: "https://your-panel.com"
      ApiKey: "token"
      NodeID: 1
      NodeType: Vless
      # 面板 network/tls 可忽略；由下方 ControllerConfig 覆盖
    ControllerConfig:
      ListenIP: 0.0.0.0
      UpdatePeriodic: 60

      # ===== 强制 TLS + XHTTP（覆盖面板）=====
      Transport: "xhttp"
      Security: "tls"
      Vision: "off"                 # CDN 推荐 off；需要时 "on"

      XHTTP:
        Host: "cdn.example.com"     # 通常与 CDN 域名一致
        Path: "/vless-cdn"          # 必须；CDN 按 path 回源
        Mode: "auto"                # auto | packet-up | stream-up | stream-one
        # Extra: '{"scMaxBufferedPosts":100}'  # 可选高级 JSON

      # ===== 源站证书（Full Strict 必填）=====
      CertConfig:
        CertMode: file
        CertDomain: "cdn.example.com"
        CertFile: /etc/XrayR/cert/cdn.example.com.crt
        KeyFile: /etc/XrayR/cert/cdn.example.com.key
        # 以下可省略 → 使用 PQ 友好默认
        # ALPN: ["h2", "http/1.1"]
        # CurvePreferences: ["X25519MLKEM768", "X25519", "P256"]
        # MinVersion: "1.3"
        RejectUnknownSni: false

      # ===== 后量子：VLESS Encryption（可选，默认 none）=====
      # 生成: 在装有 xray-core 的机器执行  xray vlessenc
      # 把输出的 "decryption" 整串贴到这里；客户端用配对的 "encryption"
      VlessDecryption: ""
      # 示例（请用真实生成值替换）:
      # VlessDecryption: "mlkem768x25519plus.native.600s.<serverKey>"

      EnableFallback: false         # 开了 VlessDecryption 时必须 false
      # 若 CDN/LB 传 PROXY protocol 再开:
      # EnableProxyProtocol: true
```

面板只负责用户 UUID / 流量；**传输与安全由节点本地字段完整接管**。

---

## 3. 字段说明

### 3.1 覆盖类

| 字段 | 作用 | CDN 建议值 |
|---|---|---|
| `Transport` | 覆盖面板 `network` | `xhttp` |
| `Security` | 覆盖面板 tls 类型 | `tls`（或 Flexible 时用 `none`） |
| `Vision` | `on` / `off` / `auto` | **`off`**（auto 在 XHTTP 且面板未开 Vision 时也为 off） |

### 3.2 XHTTP

| 字段 | 必填 | 说明 |
|---|:---:|---|
| `XHTTP.Path` | **是** | 如 `/vless`；无 path 会启动失败并报错 |
| `XHTTP.Host` | 推荐 | CDN 域名 / 源站期望 Host |
| `XHTTP.Mode` | 否 | 默认 `auto`；H2 端到端可试 `stream-one` / `stream-up` |
| `XHTTP.Headers` | 否 | 禁止包含 Host 键 |
| `XHTTP.Extra` | 否 | 原始 JSON，合并进 core `SplitHTTPConfig`（padding/xmux 等） |

### 3.3 TLS / 后量子曲线

| 字段 | 默认（空时） |
|---|---|
| `ALPN` | `h2`, `http/1.1` |
| `CurvePreferences` | **`X25519MLKEM768`, `X25519`, `P256`**（TLS 层 PQ 握手） |
| `MinVersion` / `MaxVersion` | core 默认 |

客户端与 CDN 到源站的 TLS 若支持 MLKEM，可协商 **X25519MLKEM768**。  
注意：很多 CDN 回源 TLS **不一定**支持 PQ 曲线；此时会回落到 X25519。载荷后量子仍靠 **VlessDecryption**。

### 3.4 VLESS Encryption（载荷后量子）

```bash
# 使用与节点相同大版本的 xray-core
xray vlessenc
```

输出两对，选 **Authentication: ML-KEM-768, Post-Quantum**：

- 服务端 → `VlessDecryption`（`...native.600s.<seed>`）
- 客户端 → `encryption`（`...native.0rtt.<client>`）

约束：

- **不能**与 `EnableFallback: true` 同开  
- 默认留空 = `none`（无载荷 PQ，仅 TLS 曲线 PQ）

---

## 4. CDN 侧配置要点（以 Cloudflare 为例）

1. 域名解析到 CDN；SSL/TLS 模式 **Full (strict)**  
2. 源站 IP 填 VPS；源站端口与节点 `server_port` 一致（常 443）  
3. 若用 path 分流，CDN 路由 / Worker / 回源路径与 `XHTTP.Path` 一致  
4. 证书：源站 `CertFile` 域名与客户端 SNI / CDN 主机名匹配  
5. WebSocket 类特性：XHTTP 走 HTTP 语义，**不依赖**「WebSocket 开关」；H2 回源更佳  
6. 可选：若需真实客户端 IP，在 CDN/反代开启 PROXY protocol，节点 `EnableProxyProtocol: true`

---

## 5. 客户端示意（非面板）

```json
{
  "protocol": "vless",
  "settings": {
    "vnext": [{
      "address": "cdn.example.com",
      "port": 443,
      "users": [{
        "id": "<uuid>",
        "encryption": "mlkem768x25519plus.native.0rtt.<clientKey>",
        "flow": ""
      }]
    }]
  },
  "streamSettings": {
    "network": "xhttp",
    "security": "tls",
    "tlsSettings": {
      "serverName": "cdn.example.com",
      "alpn": ["h2", "http/1.1"],
      "fingerprint": "chrome"
    },
    "xhttpSettings": {
      "host": "cdn.example.com",
      "path": "/vless-cdn",
      "mode": "auto"
    }
  }
}
```

- 未开 VLESS Encryption 时客户端 `encryption: "none"`  
- CDN 场景 `flow` 建议空（与节点 `Vision: off` 一致）

---

## 6. 与 REALITY + Vision 栈的关系

| | REALITY + Vision（直连） | TLS + XHTTP + CDN |
|---|---|---|
| 抗主动探测 | 强 | 依赖 CDN 品牌站 |
| 过 CDN | 不适合 | **适合** |
| PQ 签名 ML-DSA | 有 | 无（TLS 证书体系） |
| PQ 握手 | REALITY-MLKEM 自动 | TLS `CurvePreferences` |
| PQ 载荷 | VlessDecryption | **同左** |
| 节点本地覆盖 | 已有 REALITY 字段 | **本文件 Transport/Security/XHTTP** |

两套可并存于不同 NodeID / 不同端口。

---

## 7. 验收清单

- [ ] 仅配节点 yml（面板 network 即使是 tcp）仍监听 XHTTP+TLS  
- [ ] 缺 `XHTTP.Path` 时启动报错明确  
- [ ] `curl -vk https://cdn.example.com/vless-cdn` 能打到源站 TLS（不必是有效 VLESS）  
- [ ] 客户端 encryption 配对后可代理；改为错误串则失败  
- [ ] 开 `VlessDecryption` 时 `EnableFallback: true` 启动失败  
- [ ] 日志无 panic；限速/流量统计正常  

---

## 8. 生成密钥速查（节点自带 · 同版本 core）

```bash
XrayR tools vlessenc     # VLESS Encryption 服务端 decryption + 客户端 encryption
XrayR tools x25519       # 通用 X25519（REALITY 用；本 CDN 组合非必须）
XrayR tools mlkem768     # 底层 ML-KEM 材料（vlessenc 已封装）
XrayR tools help
```

无需再单独安装 `xray` 二进制。

---

*节点实现版本：ControllerConfig.Transport / Security / Vision / XHTTP / CertConfig 曲线与 ALPN 默认 / VlessDecryption。*
