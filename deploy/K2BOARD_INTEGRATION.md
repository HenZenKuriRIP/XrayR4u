# K2Board × XrayR4u 对接参考

面向 **K2Board / UniProxy 面板与节点运维** 的正式对接参考。  
面板端对接已完成；本文作为 **API 契约与字段速查**，以及节点本地覆盖说明。

| 项 | 当前值 |
|---|---|
| 节点程序 | XrayR4u **v0.9.14** |
| 面板协议 | K2Board UniProxy（`/api/v1/server/UniProxy/*`） |
| 内嵌核心 | **xray-core v26.7.11** |
| 用户体系 | UUID（VLESS / AnyTLS 复用） |

> 节点侧关键参数均支持 **config.yml 本地覆盖**（`ControllerConfig` 非空优先于面板）。

---

## 1. 支持的节点形态

| 形态 | 典型用途 | 面板核心字段 | 节点本地可完整覆盖 |
|---|---|---|:---:|
| **A. VLESS + REALITY + Vision** | 直连抗探测（主推） | `tls=2` + `flow` + REALITY 密钥 | 部分（REALITY 密钥仍建议面板下发） |
| **B. VLESS + TLS + XHTTP + CDN + 后量子** | 过 CDN | `tls=1` + `network=xhttp` + path/host | **是**（见 §5） |
| **C. AnyTLS + TLS** | 多路复用 / 填充 | `node_type=anytls` + `tls=1` + 证书域名 | 是 |

后量子（PQ）分层（勿混为一谈）：

| 层 | 作用 | 适用形态 |
|---|---|---|
| REALITY **minClientVer** | 客户端版本门槛 | A |
| REALITY **ML-DSA-65** | 证书额外抗量子签名 | A（可选） |
| REALITY **X25519MLKEM768** | 握手 KEX（dest 支持时自动） | A（无字段） |
| TLS **CurvePreferences** | 源站 TLS 优先 PQ 曲线 | B（节点默认开启） |
| **VLESS Encryption** | 载荷抗量子加密 | A / B（可选，默认关） |

---

## 2. UniProxy API 一览

节点 `ApiConfig.NodeType` 会作为查询参数 `node_type` 发送（如 `vless` / `anytls`）。

| 方法 | 路径 | 方向 | 用途 |
|---|---|---|---|
| GET | `/api/v1/server/UniProxy/config` | 面板 → 节点 | 节点端口 / 传输 / TLS / REALITY / 后量子等 |
| GET | `/api/v1/server/UniProxy/user` | 面板 → 节点 | 用户 UUID 列表 |
| POST | `/api/v1/server/UniProxy/push` | 节点 → 面板 | 用户流量 |
| POST | `/api/v1/server/UniProxy/status` | 节点 → 面板 | CPU/内存/磁盘/在线 IP 等 |
| POST | `/api/v1/server/UniProxy/alive` | 节点 → 面板 | 在线用户 IP |

公共 Query（节点自动带）：

```text
node_id=<ApiConfig.NodeID>
token=<ApiConfig.ApiKey>
node_type=<ApiConfig.NodeType>   # 如 vless、anytls
```

`config` 另带：`local_port=1`（兼容字段）。

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
| `server_name` | A/B/C | REALITY 的 SNI 列表元素；TLS 的展示 SNI |
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
| minClientVer | 下发 `min_client_ver` | 节点兜底 `1.8.0`（勿留空让 core 变成 26.3.27） |
| ML-DSA-65 | 可选 `mldsa65_seed`；订阅下发 **Verify 公钥** | 关 |
| X25519MLKEM768（REALITY） | **无字段**；dest 支持则自动 | 自动 |
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
| `uuid` | VLESS id / AnyTLS 密码 |
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

服务端 `decryption` 与客户端 `encryption` **成对**，不可混用不同生成结果。

---

## 9. 面板产品策略建议

| 产品线 | 配置要点 |
|---|---|
| **兼容默认** | REALITY + Vision + `min_client_ver=1.8.0`；PQ 全关 |
| **直连增强** | 上 + 可选 ML-DSA；dest 选支持 PQ 的站点 |
| **CDN 线路** | TLS + XHTTP + path；Vision 关；可选 VLESS Encryption |
| **高安全** | 独立节点开 VLESS Encryption；仅推给支持的客户端 |

**禁止组合：**

- `decryption != none` **且** 节点 Fallback 开启 → 节点拒绝启动  
- REALITY 与传统 CDN 同路径混用（不推荐）

---

## 10. 对接完成状态（参考）

面板侧已完成 UniProxy 对接时，应覆盖：

- REALITY：`min_client_ver`、密钥、`flow`、可选 `mldsa65_seed` + 订阅 Verify  
- CDN / XHTTP：`network=xhttp`、host/path、TLS  
- 可选 VLESS Encryption：`decryption` + 订阅 `encryption`  
- AnyTLS：`node_type=anytls`、`tls=1`  
- 用户 / push / status / alive 格式与下文一致  

节点侧补齐项（运维）：证书路径、`RealityMinClientVer`、CDN 本地 `Transport/XHTTP` 覆盖等。

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
│  · 拉 config/user，推 traffic/status/alive                 │
│  · ControllerConfig 可覆盖传输/TLS/XHTTP/PQ                 │
│  · 证书文件、限速、Fallback、监听 IP 等运维项                 │
│  · 驱动 xray-core 26.7.11                                  │
└─────────────────────────────────────────────────────────┘
```

| 归面板 | 归节点 |
|---|---|
| 用户与订单 | 证书文件路径 / ACME |
| 订阅链接 | ListenIP / 限速 / 设备限制 |
| REALITY 密钥材料（建议） | XHTTP/TLS 本地强制覆盖（CDN 救急） |
| 线路类型选择 | 日志、Fallback、PROXY protocol |

---

## 12. 故障速查

| 现象 | 处理 |
|---|---|
| 升级后 Mihomo 等连不上 REALITY | 下发 `min_client_ver: "1.8.0"` 或节点 `RealityMinClientVer` |
| CDN 无流量 | 查 path/host、源站证书、CDN SSL=Full Strict、节点 `Transport/XHTTP` |
| 开 Encryption 启动失败 | 关闭 Fallback；检查 decryption 串（`xray vlessenc`） |
| ML-DSA 后仅新客户端能连 | 订阅必须带 `mldsa65Verify`，或关闭 seed |
| 想确认 REALITY 是否 PQ KEX | 节点 `RealityShow: true` 看日志 |

---

## 13. 相关文档

| 文档 | 内容 |
|---|---|
| 本文 | **K2 对接参考（API / 字段）** |
| [TLS_XHTTP_CDN_PQ.md](TLS_XHTTP_CDN_PQ.md) | 节点侧 CDN 栈运维与 yml |
| [README.md](README.md) | VPS 部署与安装脚本 |
| [../INSTALL.md](../INSTALL.md) | 安装详解 |
| [../ARCHITECTURE.md](../ARCHITECTURE.md) | 代码架构 |

---

*XrayR4u v0.9.14 · xray-core v26.7.11 · REALITY/Vision/PQ + TLS/XHTTP/CDN + AnyTLS · XrayR tools*
