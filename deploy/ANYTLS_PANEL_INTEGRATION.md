# AnyTLS 面板对接说明

本文档说明如何让 V2Board/K2Board/UniProxy 面板支持 AnyTLS 协议节点。

---

## 一、AnyTLS 协议特点

AnyTLS 是"TLS + 密码认证"协议。与 VLESS + REALITY 的核心差异：

| 维度 | VLESS + REALITY | AnyTLS + TLS |
|------|-----------------|--------------|
| 端口 | 任意（通常 443） | 任意（通常 443 或 8443） |
| TLS | 无需证书（REALITY 窃取） | **必须有有效域名证书** |
| 认证 | UUID + Vision 流控 | SHA256(密码) |
| 多路复用 | 无（Vision 单流） | **內建多路复用（多 Stream）** |
| 流量填充 | 无 | **可自定义 per-packet padding** |
| 抗封锁 | REALITY 防中间件探测 | TLS 标准加密 + padding 混淆 |
| 用户密码 | UUID | UUID（复用 V2Board 用户 UUID 字段） |

**面板最核心的变化**：AnyTLS 节点要求 `tls=1`（不是 `tls=2` REALITY），且节点必须有正确的域名 TLS 证书配置。用户管理完全复用 VLESS 的 UUID 体系。

---

## 二、面板 API 改动

### 2.1 请求参数变化

所有 API 请求都带 `node_type` 参数。当前 XrayR4u 客户端硬编码发送 `node_type=vless`。

**需要改动 XrayR4u 侧**：`api/v2board/v2board.go` 将 `node_type` 从硬编码改为读取配置中的 `NodeType` 字段。涉及 5 个方法：

```go
// 修改前
req.SetQueryParam("node_type", "vless")

// 修改后
req.SetQueryParam("node_type", c.NodeType)
```

| 方法 | API 路径 | 影响 |
|------|---------|------|
| `GetNodeInfo()` | `/api/v1/server/UniProxy/config` | 获取节点配置 |
| `GetUserList()` | `/api/v1/server/UniProxy/user` | 获取用户列表 |
| `ReportUserTraffic()` | `/api/v1/server/UniProxy/push` | 上报流量 |
| `ReportNodeStatus()` | `/api/v1/server/UniProxy/status` | 上报节点状态 |
| `ReportNodeOnlineUsers()` | `/api/v1/server/UniProxy/alive` | 上报在线用户 |

### 2.2 节点配置 API（`/api/v1/server/UniProxy/config`）

#### AnyTLS 请求（XrayR4u → 面板）

```
GET /api/v1/server/UniProxy/config?node_id=2&node_type=anytls&token=xxx&local_port=1
```

#### AnyTLS 响应（面板 → XrayR4u）

```json
{
  "server_port": 443,
  "network": "tcp",
  "tls": 1,
  "tls_settings": {
    "server_name": "anytls.your-domain.com"
  }
}
```

**字段说明**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `server_port` | int | 是 | 节点监听端口 |
| `network` | string | 否 | `"tcp"` / `"websocket"` / `"grpc"`，默认 `"tcp"` |
| `tls` | int | **是** | **必须为 `1`**（1=TLS，2=REALITY，0=无） |
| `tls_settings.server_name` | string | 否 | TLS SNI，通常与证书域名一致 |

**与 VLESS + REALITY 响应的差异**：

```diff
- "tls": 2,
+ "tls": 1,

- "flow": "xtls-rprx-vision",
+ (无需 flow 字段)

- "tls_settings": {
-     "public_key": "...",
-     "private_key": "...",
-     "short_id": "...",
-     "dest": "...",
-     "server_port": "443"
- }
+ "tls_settings": {
+     "server_name": "anytls.your-domain.com"
+ }
```

**面板后端需要做的**：
1. 识别 `node_type=anytls` 的请求
2. 从数据库读取对应节点的端口和 TLS 配置
3. 返回 `tls=1`（而不是 REALITY 的 `tls=2`）
4. 不返回 `flow` / `public_key` / `private_key` / `short_id` 等 REALITY 专有字段

### 2.3 用户列表 API（`/api/v1/server/UniProxy/user`）

#### 请求

```
GET /api/v1/server/UniProxy/user?node_id=2&node_type=anytls&token=xxx
```

#### 响应

```json
{
  "users": [
    {
      "id": 1001,
      "uuid": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
      "email": "user@example.com",
      "speed_limit": 0
    },
    {
      "id": 1002,
      "uuid": "b2c3d4e5-f6a7-8901-bcde-f12345678901",
      "email": "user2@example.com",
      "speed_limit": 10
    }
  ]
}
```

**与 VLESS 用户格式完全一致，无需任何改动。** `uuid` 字段作为 AnyTLS 的认证密码，SHA256 哈希后在服务端匹配。

### 2.4 流量/状态/在线上报 API

这些 API **无需任何改动**。AnyTLS 与 VLESS 使用完全相同的上报格式：

| API | 路径 | 响应要求 |
|-----|------|---------|
| 流量上报 | `/api/v1/server/UniProxy/push` | 不变 |
| 节点状态 | `/api/v1/server/UniProxy/status` | 不变 |
| 在线用户 | `/api/v1/server/UniProxy/alive` | 不变 |

---

## 三、面板数据库/后台改动

### 3.1 节点管理

在面板后台的节点管理中，需要支持添加 **AnyTLS** 类型的节点。最小改动：

1. **节点类型字段**：数据库中节点表的 `type` 或 `node_type` 字段支持 `"anytls"` 值
2. **端口**：与 VLESS 相同，支持配置监听端口
3. **TLS 设置**：AnyTLS 节点必须配置 TLS 证书域名。证书本身在 VPS 上管理（不通过面板），面板只需要知道域名（SNI）
4. **传输协议**：与 VLESS 相同，支持 `tcp` / `websocket` / `grpc`

### 3.2 用户管理

**完全复用现有 VLESS 用户体系**。AnyTLS 使用用户 UUID 作为密码：

```
v2board_users
  ├── id: 1001
  ├── email: "user@example.com"
  ├── uuid: "a1b2c3d4-e5f6-7890-abcd-ef1234567890"  ← 同时作为 VLESS ID 和 AnyTLS 密码
  ├── speed_limit: 0       (Mbps, 0=不限速)
  └── device_limit: 0      (0=不限设备)
```

不需要新增数据库字段。

### 3.3 建议的节点配置文件示例

```php
// V2Board App/Http/Controllers/V1/Server/UniProxyController.php

// config 方法中添加 anytls 分支
if ($request->node_type === 'anytls') {
    return response()->json([
        'server_port' => $node->port,
        'network'     => $node->network ?? 'tcp',
        'tls'         => 1,           // TLS 模式，不是 REALITY
        'tls_settings' => [
            'server_name' => $node->tls_domain,  // 证书域名（SNI）
        ],
    ]);
}
```

---

## 四、XrayR4u 侧需要的配合改动

### 4.1 `node_type` 参数动态化

`api/v2board/v2board.go` 目前硬编码 `node_type=vless`：

```go
// 当前代码 — 所有 5 个 API 方法都硬编码了 "vless"
req.SetQueryParam("node_type", "vless")

// 需要改为读取配置
req.SetQueryParam("node_type", c.NodeType)
```

`c.NodeType` 来自 `config.yml` 中 `ApiConfig.NodeType: anytls`，已在 `New()` 构造函数中赋值。

### 4.2 配置示例

```yaml
Nodes:
  - PanelType: "V2board"
    ApiConfig:
      ApiHost: "https://panel.your-domain.com"
      ApiKey: "your-api-token"
      NodeID: 2                     # 面板上的 AnyTLS 节点 ID
      NodeType: anytls              # 触发 AnyTLS 代码路径
    ControllerConfig:
      ListenIP: 0.0.0.0
      CertConfig:
        CertMode: file
        CertDomain: "anytls.your-domain.com"
        CertFile: "/etc/ssl/anytls.your-domain.com.crt"
        KeyFile: "/etc/ssl/anytls.your-domain.com.key"
```

---

## 五、端到端测试检查清单

### 面板侧

- [ ] 后台可添加 `node_type=anytls` 的节点
- [ ] 节点配置 API 对 `node_type=anytls` 请求返回 `tls=1`
- [ ] 节点配置 API **不**返回 REALITY 专有字段（`public_key` / `private_key` / `short_id` / `flow`）
- [ ] 用户列表 API 对 anytls 节点正常返回
- [ ] 流量/状态/在线上报 API 正常接受 anytls 节点的数据

### VPS 侧

- [ ] 域名 DNS A 记录指向 VPS IP
- [ ] TLS 证书文件存在于配置的路径
- [ ] 证书包含正确的域名且未过期
- [ ] `curl -v https://域名:端口` 能看到 TLS 握手成功

### 客户端侧

- [ ] AnyTLS 客户端可使用面板分配的 UUID 连接
- [ ] 多用户同时在线正常
- [ ] 流量统计在面板显示正确
- [ ] 设备限制生效

---

## 六、常见问题

### Q: AnyTLS 和 VLESS 可以用同一个端口吗？

不能。不同协议需要监听不同端口。在面板上注册两个节点（分别用 VLESS 和 AnyTLS），分配不同端口，在 VPS 上通过 XrayR4u 的 `Nodes` 多节点配置同时运行。

### Q: AnyTLS 支持 CDN / WebSocket 吗？

AnyTLS 本身是基于 TCP+TLS 的协议。如果面板配置了 `network: websocket`，XrayR4u 会在 TLS 层之上叠加 WebSocket 传输——但这属于传输层配置，与 AnyTLS 协议本身无关。建议先验证 `tcp` 模式，再尝试其他传输方式。

### Q: 证书续期怎么处理？

使用 acme.sh 的 `--reloadcmd` hook 在证书更新后重启 XrayR：
```bash
acme.sh --install-cert -d anytls.your-domain.com \
  --key-file   /etc/ssl/anytls.your-domain.com.key \
  --fullchain-file /etc/ssl/anytls.your-domain.com.crt \
  --reloadcmd  "systemctl restart xrayr"
```
