# K2Board 后端对接升级说明：后量子（PQ）与 xray-core 26.7.11

面向 **K2Board / UniProxy 面板后端** 开发者：节点侧已升级到 **Xray-core v26.7.11**，并支持三层后量子能力。本文说明面板 **config API 需要新增/兼容的字段**、优先级、订阅下发与兼容策略。

---

## 1. 节点侧版本与能力总览

| 项 | 值 |
|---|---|
| 节点后端 | XrayR4u（本仓库） |
| 内嵌核心 | **xray-core v26.7.11** |
| 主协议 | VLESS + REALITY + Vision（既有） / AnyTLS + TLS |
| 后量子能力 | 见下表三阶段 |

### 三阶段后量子能力

| 阶段 | 能力 | 是否需要面板改字段 | 默认 |
|:---:|---|:---:|---|
| **1** | REALITY **ML-DSA-65** 证书额外签名 | 可选 `mldsa65_seed` | 关（不填即关） |
| **2** | REALITY **X25519MLKEM768** TLS 密钥协商 | **否**（自动） | dest 支持时自动开 |
| **3** | **VLESS Encryption**（`mlkem768x25519plus`） | 可选 `decryption` | `none`（不填即关） |

另有运维必配项（与 PQ 相关）：

| 字段 | 位置 | 说明 |
|---|---|---|
| `minClientVer` | REALITY | **强烈建议显式下发**；不写时节点默认 `1.8.0`（兼容第三方客户端）。xray-core ≥ 26.7.11 若最终为空会默认 `26.3.27`，易导致 Mihomo 等连不上。 |

---

## 2. UniProxy `/config` 响应：字段对照

现有接口：`GET /api/v1/server/UniProxy/config?node_id=&token=&node_type=vless`

### 2.1 完整示例（VLESS + REALITY + Vision + 可选 PQ）

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
    "public_key": "<对应 PublicKey / Password>",
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

### 2.2 字段说明（面板后端需支持）

#### 既有字段（保持兼容）

| JSON 字段 | 类型 | 必填 | 说明 |
|---|---|:---:|---|
| `server_port` | int | 是 | 监听端口 |
| `network` | string | 否 | 默认 `tcp`；亦可 `ws` / `grpc` / `xhttp` 等 |
| `tls` | int | 是 | `0` 无 / `1` TLS / `2` REALITY |
| `flow` | string | 否 | Vision 时为 `xtls-rprx-vision` |
| `tls_settings.server_name` | string | REALITY 是 | SNI / serverNames |
| `tls_settings.dest` | string | 否 | 回落目标；空则用 server_name |
| `tls_settings.server_port` | string | 否 | dest 端口，默认 `443` |
| `tls_settings.private_key` | string | REALITY 是 | `xray x25519` 私钥 |
| `tls_settings.public_key` | string | 是* | 公钥（订阅下发用；服务端构建也会带上） |
| `tls_settings.short_id` | string | REALITY 是 | shortId |
| `tls_settings.fingerprint` | string | 否 | 默认 `chrome` |

#### **新增 / 推荐** 字段

| JSON 字段 | 类型 | 必填 | 阶段 | 说明 |
|---|---|:---:|:---:|---|
| `tls_settings.min_client_ver` | string | **推荐** | 运维 | REALITY 最低客户端版本，建议 `"1.8.0"`。亦接受 camelCase `minClientVer`。 |
| `tls_settings.mldsa65_seed` | string | 否 | **1** | ML-DSA-65 seed（`xray mldsa65` 输出的 Seed，base64.RawURLEncoding）。亦接受 `mldsa65Seed`。空 = 不启用。 |
| `tls_settings.show` | bool | 否 | **2 调试** | `true` 时 REALITY 打印是否使用 X25519MLKEM768 / ML-DSA。生产默认 `false`。 |
| `decryption` | string | 否 | **3** | VLESS 入站 `settings.decryption`。默认视为 `none`。PQ 时为完整 `mlkem768x25519plus....` 服务端串。亦接受顶层 `vless_decryption`。 |

> **X25519MLKEM768 没有配置字段。** 阶段 2 仅要求：双方客户端/服务端足够新，且 `dest` 目标站 TLS 支持该曲线时自动协商。

---

## 3. 节点侧优先级（面板 vs config.yml）

节点 `ControllerConfig` 可本地覆盖（**非空优先于面板**），便于单机救急而不改面板：

| 配置项 | 覆盖对象 | 默认 |
|---|---|---|
| `RealityMinClientVer` | `minClientVer` | 面板 → 再 → `"1.8.0"` |
| `RealityMldsa65Seed` | `mldsa65Seed` | 面板 → 空则关闭 |
| `RealityShow` | `show` | 本地 `true` 可强制打开 |
| `VlessDecryption` | `decryption` | 面板 → `"none"` |

```yaml
ControllerConfig:
  RealityMinClientVer: "1.8.0"
  RealityMldsa65Seed: ""          # 或 xray mldsa65 的 Seed
  RealityShow: false
  VlessDecryption: ""             # 或完整 mlkem768x25519plus... 串
  EnableFallback: false           # VLESS Encryption 开启时必须为 false
```

---

## 4. 阶段落地细节（面板侧该做什么）

### 阶段 1 — ML-DSA-65（可选后量子签名）

**生成（运维/面板后台）：**

```bash
xray mldsa65
# Seed:   <服务端 tls_settings.mldsa65_seed>
# Verify: <客户端 reality.mldsa65Verify / 订阅字段>
```

**面板：**

1. 节点编辑增加「ML-DSA-65 Seed」输入（可选）。
2. UniProxy config 写入 `tls_settings.mldsa65_seed`。
3. 用户订阅/分享链接增加 **`mldsa65Verify`（公钥）**；未启用时不要下发该字段。
4. 未配置 seed 的旧节点行为不变（兼容）。

**注意：**

- Seed 与 REALITY `private_key` **不能相同**（xray-core 校验）。
- 客户端无 Verify 公钥时：服务端开了 seed 可能导致认证失败——**两端同时升级**或保持都关闭。

---

### 阶段 2 — X25519MLKEM768（自动）

**面板几乎无代码：**

1. 保证节点与主流客户端使用 **新 xray-core**（节点已 26.7.11）。
2. `dest` / `server_name` 尽量选 **支持 X25519MLKEM768 的真实站点**（可用 `xray tls ping <host:443>` 或浏览器安全面板查看）。
3. 调试：临时 `show: true` 或节点 `RealityShow: true`，日志会打印是否协商 PQ KEX。

**不需要** 新增 API 字段。

---

### 阶段 3 — VLESS Encryption（可选后量子载荷加密）

**生成密钥材料：**

```bash
xray x25519      # X25519 密钥对
xray mlkem768    # ML-KEM-768 seed / client
```

服务端 `decryption` / 客户端 `encryption` 格式（示意，以官方文档为准）：

```text
mlkem768x25519plus.<mode>.<rtt>.[padding...].<key material...>
# mode: native | xorpub | random
# rtt:  0rtt | 1rtt | 600s | 300-600s 等
```

**面板：**

1. 节点增加「VLESS decryption」高级字段（默认 `none` 或留空）。
2. UniProxy config 顶层返回 `"decryption": "..."`。
3. 订阅给客户端写 **对应的 `encryption`**（不是 decryption）。
4. **开启 Encryption 时禁止 Fallback**（节点侧会直接拒绝启动并报错）。
5. 旧客户端不支持时：保持 `none`，或做「PQ 专用节点」与「兼容节点」分流。

**与 Vision / REALITY：**

- 可与 `flow: xtls-rprx-vision`、`tls: 2` REALITY **叠加**。
- Encryption 保护 VLESS 载荷；**不能替代** TLS/REALITY 的 HTTPS 外观。
- 推荐组合：`VLESS + REALITY + Vision + (可选 ML-DSA) + (可选 Encryption)`。

---

## 5. 用户列表 / 流量 / 状态 API

**无需改动。**

- `GET .../user`、`POST` 流量、`POST` 节点状态格式不变。
- 用户仍用 UUID；Encryption 密钥在**节点级**，不在用户级。

---

## 6. 订阅 / 分享链接（客户端）建议字段

| 场景 | 新增客户端字段 |
|---|---|
| REALITY 基础 | 既有：`pbk` / `sid` / `sni` / `fp` / `flow` |
| 阶段 1 ML-DSA | `mldsa65Verify`（或面板约定名） |
| 阶段 2 MLKEM KEX | 无（核心自动） |
| 阶段 3 Encryption | `encryption` = 与服务端配对的客户端串 |

分享标准以 [Project X / Xray 文档](https://xtls.github.io/) 与官方 release note 为准；面板序列化时勿截断长密钥串。

---

## 7. 兼容性矩阵（面板产品策略建议）

| 客户端类型 | minClientVer=`1.8.0` | ML-DSA 开启 | VLESS Encryption 开启 |
|---|---|---|---|
| 新版 Xray-core 官方 | ✅ | 需带 Verify | 需带 encryption |
| Mihomo / 部分 Meta | ✅（版本号常报 1.8.x） | 视内核支持 | 视内核支持 |
| 很老的 Xray | 可能 ❌（若抬高 minClientVer） | ❌ | ❌ |

**产品建议：**

1. **默认路径**：REALITY + Vision + `min_client_ver=1.8.0`，PQ 全关 → 最大兼容。  
2. **增强路径**：同节点开 ML-DSA（阶段 1）+ 选 PQ dest（阶段 2）。  
3. **高安全路径**：独立节点开 VLESS Encryption（阶段 3），仅推送给支持的客户端。

---

## 8. 面板后端改造 Checklist

- [ ] UniProxy config 解析/下发 `tls_settings.min_client_ver`（推荐默认 `1.8.0`）
- [ ] 节点模型增加 `mldsa65_seed`（可选），写入 `tls_settings.mldsa65_seed`
- [ ] 订阅下发 `mldsa65Verify`（仅当节点启用 ML-DSA）
- [ ] 节点模型增加 `decryption`（可选，默认 none），写入 config 顶层
- [ ] 订阅下发客户端 `encryption`（仅当节点启用 VLESS Encryption）
- [ ] 启用 Encryption 时 UI 禁止配置 Fallback，并提示客户端要求
- [ ] 文档/后台说明：X25519MLKEM768 自动、依赖 dest
- [ ] 管理后台提供「生成 x25519 / mldsa65 / mlkem768」按钮或运维文档链接
- [ ] 回归：旧节点不填新字段时行为与升级前一致

---

## 9. 节点侧 config.yml 示例（对照）

```yaml
Nodes:
  - PanelType: "V2board"
    ApiConfig:
      ApiHost: "https://panel.example.com"
      ApiKey: "token"
      NodeID: 1
      NodeType: Vless
      EnableReality: true
    ControllerConfig:
      ListenIP: 0.0.0.0
      UpdatePeriodic: 60
      RealityMinClientVer: "1.8.0"
      RealityMldsa65Seed: ""      # 可选覆盖面板
      RealityShow: false
      VlessDecryption: ""        # 可选覆盖面板；非 none 时勿开 Fallback
      EnableFallback: false
```

---

## 10. 故障排查速查

| 现象 | 可能原因 |
|---|---|
| 升级后第三方客户端全挂 | `minClientVer` 变成 `26.3.27`；改为 `1.8.0` |
| 日志 `authentication failed`（REALITY） | shortId / 公钥 / 时间差 / minClientVer / ML-DSA 端不齐 |
| 开了 Encryption 启动失败 | 同时开了 Fallback；或 decryption 串非法 |
| 想确认是否 PQ KEX | `RealityShow: true` 或面板 `show: true` 看日志 X25519MLKEM768 |
| 流量/在线人数为 0 | 与 PQ 无关；查 Vision DispatchLink / 统计（既有逻辑） |

---

## 11. 参考命令

```bash
# REALITY 密钥
xray x25519

# 阶段 1 ML-DSA-65
xray mldsa65

# 阶段 3 VLESS Encryption 材料
xray mlkem768

# 探测目标站 TLS / 是否 PQ（以本机 xray 版本为准）
xray tls ping www.example.com:443

```

官方文档：

- 传输 / REALITY：https://xtls.github.io/config/transport.html  
- VLESS Encryption：https://github.com/XTLS/Xray-core/pull/5067 及后续 release notes  
- REALITY ML-DSA / X25519MLKEM768：Project X Channel 与 xray-core releases（v25.5+ / v25.7+）

---

## 12. 变更摘要（给面板版本发布说明可直接粘贴）

> 节点后端升级 xray-core **26.7.11**，并支持：  
> 1）REALITY `min_client_ver` 透传（默认兼容 1.8.0）；  
> 2）可选 REALITY **ML-DSA-65**（`mldsa65_seed` + 订阅 `mldsa65Verify`）；  
> 3）REALITY **X25519MLKEM768** 自动协商（依赖 dest，无新字段）；  
> 4）可选 **VLESS Encryption**（config 顶层 `decryption` + 订阅 `encryption`，默认 none，不可与 Fallback 同开）。  
> 未配置新字段的旧节点行为保持兼容。

---

*文档版本与 XrayR4u 后量子三阶段实现对齐。若面板字段命名需与现有 PHP 模型统一，优先 snake_case；节点同时接受 camelCase。*
