# XrayR 代码架构说明书

## 1. 项目概述

XrayR 是一个基于 Go 语言开发的后端代理管理框架，它作为**前端管理面板**与 **Xray-core 代理核心**之间的桥梁，实现了从 V2Board 面板自动同步节点配置、用户信息和流量数据，并驱动 Xray-core 进行代理服务。

- **版本**: v0.9.14
- **Go 版本**: 1.26
- **核心依赖**: Xray-core v26.7.11 (`v1.260327.1-0.20260711155151-50231eaff98c`)
- **许可证**: Mozilla Public License Version 2.0
- **面板**: K2Board UniProxy

### 核心特性

| 特性 | VLESS + REALITY | VLESS + TLS + XHTTP | AnyTLS + TLS |
|------|:---:|:---:|:---:|
| 获取节点 / 用户 / 流量 | ✓ | ✓ | ✓ |
| 服务器信息上报 | ✓ | ✓ | ✓ |
| 自动申请/续签 TLS 证书 | — | ✓ | ✓ |
| 在线人数统计/IP 限制 | ✓ | ✓ | ✓ |
| 审计规则 / 限速 | ✓ | ✓ | ✓ |
| REALITY 无证书防探测 | ✓ | — | — |
| REALITY minClientVer / ML-DSA / MLKEM | ✓ | — | 共享 stream* |
| TLS + XHTTP + CDN（本地可完整覆盖） | — | ✓ | — |
| TLS PQ 曲线默认 (X25519MLKEM768) | — | ✓ | ✓ |
| VLESS Encryption（可选载荷 PQ） | ✓ | ✓ | — |
| 內建多路复用 / padding / Fallback | — | — | ✓ |

\* AnyTLS 使用 REALITY 传输时共享 stream 构建中的 REALITY 选项。

---

## 2. 项目目录结构

```
XrayR/
├── main/                          # 程序入口与配置文件
│   ├── main.go                    # 主入口：tools 子命令 / 面板服务
│   ├── tools/
│   │   └── tools.go               # XrayR tools（x25519/mldsa65/vlessenc…）
│   ├── config.yml.example         # 配置模板
│   ├── dns.json                   # DNS 配置示例
│   ├── route.json                 # 路由规则示例
│   ├── custom_inbound.json        # 自定义入站配置示例
│   ├── custom_outbound.json       # 自定义出站配置示例
│   ├── geoip.dat                  # GeoIP 数据库
│   ├── geosite.dat                # GeoSite 数据库
│   └── distro/
│       └── all/
│           └── all.go             # Xray-core 功能模块 + CLI 命令注册
│
├── panel/                         # 核心面板逻辑层
│   ├── panel.go                   # Panel 结构体：生命周期管理、核心加载
│   ├── config.go                  # 顶级配置结构定义
│   └── defaultConfig.go           # 默认值定义
│
├── api/                           # 面板 API 抽象层
│   ├── api.go                     # API 接口定义 (核心契约)
│   ├── apimodel.go                # 通用数据模型
│   ├── security.go                # 安全类型解析 (TLS/Reality/XTLS → 统一策略)
│   ├── v2board/                   # V2Board 实现
│   │   ├── v2board.go
│   │   └── model.go

│
├── service/                       # 服务抽象层
│   ├── service.go                 # Service 接口定义
│   └── controller/               # 节点控制器 (核心业务逻辑)
│       ├── controller.go          # 控制器主逻辑：启动、监控、同步
│       ├── config.go              # ControllerConfig（REALITY/XHTTP/PQ/CDN）
│       ├── stream_resolve.go      # 传输/TLS/XHTTP/Vision 解析与构建
│       ├── inboundbuilder.go      # 入站配置构建器
│       ├── outboundbuilder.go     # 出站配置构建器
│       ├── userbuilder.go         # 用户账号构建器
│       └── control.go             # Xray-core Handler 管理操作
│
├── common/                        # 通用工具模块
│   ├── common.go                  # 包说明
│   ├── limiter/                   # 速率与设备限制
│   │   ├── limiter.go             # 核心限速器 (令牌桶 + 在线IP)
│   │   ├── rate.go                # 限速写入器
│   │   └── errors.go              # 错误定义
│   ├── rule/                      # 审计规则引擎
│   │   ├── rule.go                # 规则匹配与违规记录
│   │   └── errors.go
│   ├── regexutil/                 # 安全正则 (防 ReDoS)
│   │   └── regexutil.go
│   ├── serverstatus/              # 系统状态采集
│   │   └── serverstatus.go        # CPU/内存/磁盘/运行时间
│   └── legocmd/                   # Let's Encrypt 证书管理
│       ├── lego.go                # Lego 命令行封装
│       └── cmd/                   # 子命令实现
│
├── app/                           # Xray-core 应用层插件
│   └── mydispatcher/              # 自定义调度器 (替换默认 Dispatcher)
│       ├── dispatcher.go          # 包说明
│       ├── default.go             # 调度器核心逻辑 (限速/统计/规则)
│       ├── sniffer.go             # 协议嗅探 (HTTP/TLS/BT/FakeDNS)
│       ├── fakednssniffer.go      # FakeDNS 嗅探支持
│       ├── stats.go               # 流量统计写入器
│       ├── config.proto           # Protobuf 配置定义
│       └── config.pb.go           # 自动生成的 Protobuf 代码
│
├── proxy/                         # 自定义代理协议
│   └── anytls/                    # AnyTLS 协议实现 (TLS + 密码认证 + mux + padding)
├── Dockerfile                     # Docker 构建文件
├── config.yml.template            # 完整配置模板
├── INSTALL.md                     # 详细安装文档
├── ARCHITECTURE.md                # 架构说明与扩展指南
├── go.mod                         # Go 模块依赖
├── go.sum                         # 依赖校验
├── LICENSE                        # MPL-2.0 许可证
└── README.md                      # 项目说明
```

---

## 3. 分层架构

```
┌─────────────────────────────────────────────────────────┐
│                      用户流量                             │
└─────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│                   Xray-core 代理层                       │
│  ┌──────────┐ ┌──────────┐ ┌──────────────────────────┐ │
│  │  VLESS   │ │  AnyTLS  │ │  自定义入站/出站 (可选)   │ │
│  └──────────┘ └──────────┘ └──────────────────────────┘ │
└─────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│              mydispatcher (自定义调度器)                  │
│  ┌──────────────┐ ┌───────────┐ ┌───────────────────┐  │
│  │ 速率限制     │ │ 流量统计  │ │ 审计规则检测      │  │
│  │ (令牌桶)     │ │ (Counter) │ │ (正则匹配+违规记录)│  │
│  └──────────────┘ └───────────┘ └───────────────────┘  │
└─────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│                   panel (面板层)                          │
│  ┌──────────────────┐  ┌─────────────────────────────┐  │
│  │ loadCore()       │  │ Start()/Close()             │  │
│  │ Xray-core 实例化 │  │ 生命周期管理                │  │
│  └──────────────────┘  └─────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│               service/controller (控制器层)               │
│  ┌───────────────┐ ┌─────────────┐ ┌────────────────┐  │
│  │ nodeInfoMonitor│ │userInfoMonitor│ │ addNewTag()  │  │
│  │ 节点信息同步   │ │ 流量上报    │ │ 动态添加入站  │  │
│  └───────────────┘ └─────────────┘ └────────────────┘  │
└─────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│                    api (API层)                            │
│  ┌──────────┐ ┌──────────┐ ┌────────────┐ ┌──────────┐ │
│  │ V2Board  │ │
│  └──────────┘ └──────────┘ └────────────┘ └──────────┘ │
└─────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│                   前端管理面板                            │
└─────────────────────────────────────────────────────────┘
```

---

## 4. 核心模块详解

### 4.1 入口 (main/main.go)

程序启动流程：

```
main()
  ├── flag.Parse()           # 解析命令行参数
  ├── showVersion()          # 输出版本信息
  ├── getConfig()            # 加载 YAML 配置 (Viper)
  │   ├── 设置 XRAY_LOCATION_ASSET / XRAY_LOCATION_CONFIG
  │   └── config.WatchConfig()  # 启动配置文件监控
  ├── p = panel.New(config)  # 创建 Panel 实例
  ├── p.Start()              # 启动 Panel
  ├── 注册热重载回调          # OnConfigChange → p.Close() → p.Start()
  └── 阻塞等待 OS 信号        # SIGINT/SIGKILL/SIGTERM
```

**关键设计点**:
- 使用 Viper 进行配置管理，支持 YAML 格式
- 配置文件监控采用防抖机制（3秒内重复变更仅触发一次重载）
- 热重载时主动触发 GC 清理旧实例

---

### 4.2 面板层 (panel/)

#### Panel 结构体

```go
type Panel struct {
    access      sync.Mutex
    panelConfig *Config
    Server      *core.Instance      // Xray-core 实例
    Service     []service.Service   // 控制器服务列表
    Running     bool
}
```

#### Panel.Start() 流程

```
Start()
  │
  ├── 1. loadCore(config)
  │   ├── 加载日志配置 (LogConfig)
  │   ├── 加载 DNS 配置 (dns.json)
  │   ├── 加载路由配置 (route.json)
  │   ├── 加载自定义入站配置 (custom_inbound.json)
  │   ├── 加载自定义出站配置 (custom_outbound.json)
  │   ├── 加载连接策略配置 (handshake/idle/buffer)
  │   ├── 注册核心模块:
  │   │   ├── LogConfig
  │   │   ├── mydispatcher (自定义调度器 ★)
  │   │   ├── Stats (流量统计)
  │   │   ├── Inbound/Outbound Manager
  │   │   ├── Policy (连接策略)
  │   │   ├── DNS
  │   │   └── Router
  │   └── core.New(config) → server.Start()
  │
  ├── 2. 遍历 Nodes 配置
  │   ├── 创建 API 客户端 (V2board)
  │   ├── 合并 ControllerConfig (默认值 + 用户配置)
  │   ├── 创建 Controller 服务
  │   └── 添加到 Service 列表
  │
  └── 3. 启动所有 Service
      └── s.Start() 对每个 Controller
```

#### 配置结构 (Config)

```go
type Config struct {
    LogConfig          *LogConfig        // 日志配置
    DnsConfigPath      string            // DNS 配置文件路径
    InboundConfigPath  string            // 自定义入站配置路径
    OutboundConfigPath string            // 自定义出站配置路径
    RouteConfigPath    string            // 路由配置路径
    ConnetionConfig    *ConnetionConfig  // 连接策略
    NodesConfig        []*NodesConfig    // 节点列表(支持多节点)
}

type NodesConfig struct {
    PanelType        string              // 面板类型: V2board
    ApiConfig        *api.Config         // API 连接配置
    ControllerConfig *controller.Config  // 控制器配置
}
```

---

### 4.3 API 抽象层 (api/)

#### 安全类型统一解析 (api/security.go)

将旧版 XTLS、标准 TLS、新版 REALITY 三种安全类型统一解析为 `(enableTLS, tlsType, enableVision)` 三要素：

```go
func ResolveSecurity(security string, enableXTLS, enableReality, hasRealitySettings bool) (
    enableTLS bool, tlsType string, enableVision bool,
)
```

| 输入 | 输出 | 说明 |
|------|------|------|
| `security="reality"` | `tls, "reality", vision=true` | 直接启用 REALITY+Vision |
| `security="tls"` | `tls, "tls", vision=false` | 标准 TLS |
| `security="xtls"` | `tls, "tls", vision=true` | 旧 XTLS → 降级为 TLS+Vision |
| `enableReality=true` | `tls, "reality", vision=true` | 配置强制 REALITY |
| `enableXTLS=true` | `tls, "tls", vision=true` | 配置强制 TLS+Vision |

#### API 接口定义 (核心契约)

```go
type API interface {
    GetNodeInfo() (*NodeInfo, error)                          // 获取节点配置
    GetUserList() (*[]UserInfo, error)                        // 获取用户列表
    ReportNodeStatus(*NodeStatus) error                       // 上报服务器状态
    ReportNodeOnlineUsers(*[]OnlineUser) error                // 上报在线用户
    ReportUserTraffic(*[]UserTraffic) error                   // 上报用户流量
    Describe() ClientInfo                                     // 客户端信息
    GetNodeRule() (*[]DetectRule, error)                      // 获取审计规则
    ReportIllegal(*[]DetectResult) error                      // 上报违规记录
    Debug()                                                   // 调试模式
}
```

#### 四种面板实现对比

| 功能 | V2Board |
|------|---------|
| 认证方式 | URL Query 参数 `key` | URL Query 参数 `token` | HTTP Header `key` + `timestamp` | HTTP Header `key` |
| 请求库 | resty | resty | resty | resty |
| 响应解析 | JSON 反序列化 | simplejson 动态解析 | JSON 反序列化 | JSON 反序列化 |
| 节点状态上报 | ✓ | ✓ | ✓ | ✗ (空实现) |
| 在线用户上报 | ✓ | ✗ (空实现) | ✓ | ✓ |
| 审计规则 | 远程 + 本地文件 | 远程(仅V2ray) + 本地文件 | 远程(仅 reject 模式) + 本地文件 | 远程 + 本地文件 |
| 违规上报 | ✓ | ✗ (空实现) | ✓ | ✗ (空实现) |
| Shadowsocks | 单端口多用户 | 标准端口 | 标准端口 | 标准端口 |
| REALITY 协议 | ✓ (EnableReality) | ✓ (面板下发配置) | ✓ | ✓ |
| VLESS Vision | ✓ (EnableVision) | ✓ (自动判定) | ✓ | ✓ |

#### 通用数据模型

```
NodeInfo (节点信息)          UserInfo (用户信息)
├── NodeType                ├── UID
├── NodeID                  ├── Email
├── Port                    ├── Passwd / UUID
├── SpeedLimit (Bps)       ├── SpeedLimit (Bps)
├── AlterID                 ├── DeviceLimit
├── TransportProtocol       ├── Protocol
├── Host / Path             ├── AlterID
├── EnableTLS / TLSType     └── Method (SS密码算法)
├── EnableReality / EnableVision
├── RealitySettings (REALITY配置)
├── CypherMethod
├── ServiceName (gRPC)
└── Header
```

---

### 4.4 服务控制器层 (service/controller/)

这是系统的**核心业务逻辑层**，负责管理单个节点在 Xray-core 中的完整生命周期。

#### Controller 结构体

```go
type Controller struct {
    server                  *core.Instance       // Xray-core 实例引用
    config                  *Config              // 控制器配置
    apiClient               api.API             // API 客户端
    nodeInfo                *api.NodeInfo        // 当前节点信息
    Tag                     string              // 入站/出站标签 (格式: NodeType_ListenIP_Port)
    userList                *[]api.UserInfo      // 当前用户列表
    nodeInfoMonitorPeriodic *task.Periodic       // 节点信息监控定时器
    userReportPeriodic      *task.Periodic       // 用户上报定时器
    panelType               string              // 面板类型
}
```

#### Controller.Start() 流程

```
Start()
  │
  ├── 1. 获取节点信息 (GetNodeInfo)
  │   └── 保存 nodeInfo, 生成 Tag
  │
  ├── 2. addNewTag(nodeInfo)
  │   ├── InboundBuilder() → 构建入站配置
  │   │   ├── normalizeSecurityType() → xtls→tls, reality直通
  │   │   ├── normalizeTransportProtocol() → http/quic→xhttp
  │   │   ├── V2ray  → VMess/VLESS 协议
  │   │   ├── Trojan → Trojan 协议
  │   │   ├── Shadowsocks → SS 协议
  │   │   └── Shadowsocks-Plugin → SS + dokodemo-door
  │   ├── addInbound() → 注册到 Xray-core
  │   ├── OutboundBuilder() → 构建 freedom 出站
  │   └── addOutbound() → 注册到 Xray-core
  │
  ├── 3. 获取用户列表 (GetUserList)
  │   └── 根据协议构建用户账号
  │       ├── buildVmessUser()  → VMess Account
  │       ├── buildVlessUser()  → VLESS Account (xtls-rprx-vision / 空 flow)
  │       ├── buildTrojanUser() → Trojan Account
  │       ├── buildSSUser()     → Shadowsocks Account
  │       └── buildSSPluginUser() → AEAD 加密算法过滤
  │
  ├── 4. addUsers() → 批量注册用户到 Xray-core
  │
  ├── 5. AddInboundLimiter() → 添加节点/用户级别限速
  │
  ├── 6. UpdateRule() → 加载审计规则
  │
  ├── 7. 启动定时器 (延迟 UpdatePeriodic 秒后开始)
  │   ├── nodeInfoMonitorPeriodic → 节点信息变更检测
  │   └── userReportPeriodic → 流量/在线/违规上报
  │
  └── 8. 证书管理 (如果启用 TLS)
      └── legocmd.RenewCert() 定时续签
```

#### 节点信息监控 (nodeInfoMonitor)

每 `UpdatePeriodic` 秒执行：

```
nodeInfoMonitor()
  │
  ├── 获取最新节点信息 (GetNodeInfo)
  ├── 获取最新用户列表 (GetUserList)
  │
  ├── 节点信息变更?
  │   ├── YES → 移除旧 Tag + 创建新 Tag + 更新限速器
  │   └── NO  → 增量更新用户 (compareUserList)
  │       ├── 已删除用户 → removeUsers()
  │       └── 新增用户   → addNewUser() + 更新限速器
  │
  ├── 检查审计规则更新 (GetNodeRule)
  ├── 检查 TLS 证书续签 (Legocmd.RenewCert)
  └── 更新用户列表缓存
```

#### 定时上报监控 (userInfoMonitor)

每 `UpdatePeriodic` 秒执行：

```
userInfoMonitor()
  │
  ├── 采集系统状态 (CPU/内存/磁盘/运行时间)
  ├── ReportNodeStatus() → 上报到面板
  │
  ├── 采集用户流量 (getTraffic)
  │   └── 从 Xray-core Stats Manager 读取计数并清零
  ├── ReportUserTraffic() → 上报流量
  │
  ├── 采集在线设备 (GetOnlineDevice)
  ├── ReportNodeOnlineUsers() → 上报在线 IP
  │
  ├── 采集违规记录 (GetDetectResult)
  └── ReportIllegal() → 上报违规行为
```

---

### 4.5 自定义调度器 (app/mydispatcher/)

这是 XrayR **最关键的自定义组件**，替换了 Xray-core 默认的 Dispatcher。它在流量转发的关键路径上实现了：

```
Dispatch(ctx, destination)
  │
  ├── 1. getLink(ctx) → 建立传输链路
  │   ├── 检查用户 Email 是否存在
  │   ├── GetUserBucket() → 设备数限制检查
  │   │   ├── 超过限制 → 拒绝连接 (Reject)
  │   │   └── 通过检查 → 记录在线 IP
  │   ├── 速率限制 (令牌桶算法)
  │   │   └── RateWriter 包装写入器
  │   └── 流量统计注册
  │       ├── user>>>{email}>>>traffic>>>uplink
  │       └── user>>>{email}>>>traffic>>>downlink
  │
  ├── 2. 协议嗅探 (sniffer)
  │   ├── HTTP 嗅探
  │   ├── TLS SNI 嗅探
  │   ├── BitTorrent 检测
  │   └── FakeDNS 嗅探
  │
  └── 3. routedDispatch() → 路由分发
      ├── 审计规则检测 (RuleManager.Detect)
      │   ├── 命中规则 → 中断连接
      │   └── 未命中 → 继续
      ├── 路由选择 (Router.PickRoute)
      └── 出站分发 (Handler.Dispatch)
```

**限速算法**: 取节点限速和用户限速中的较小值（非零最小值原则），使用令牌桶 (token bucket) 实现平滑限速。

---

### 4.6 通用工具模块 (common/)

#### 限速器 (limiter)

```
Limiter
├── InboundInfo (sync.Map: Tag → InboundInfo)
│   ├── Tag (入站标签)
│   ├── NodeSpeedLimit (节点总带宽限制, Bps)
│   ├── UserInfo (sync.Map: Email → UserInfo)
│   ├── BucketHub (sync.Map: Email → *ratelimit.Bucket)
│   └── UserOnlineIP (sync.Map: Email → sync.Map: IP → UID)
│
├── AddInboundLimiter()    → 注册入站限速器
├── UpdateInboundLimiter() → 增量更新用户限速配置
├── DeleteInboundLimiter() → 移除入站限速器
├── GetOnlineDevice()      → 获取在线设备列表 (同时清理离线用户令牌桶)
└── GetUserBucket()        → 获取用户令牌桶 (同时检测设备数限制)
```

**设备限制逻辑**: 为每个用户的 Email 维护一个在线 IP 列表 (sync.Map)，新 IP 连接时检查计数是否超过 DeviceLimit。

#### 审计规则 (rule)

```
RuleManager
├── InboundRule (sync.Map: Tag → []DetectRule)
├── InboundDetectResult (sync.Map: Tag → Set<DetectResult>)
│
├── UpdateRule()    → 更新规则列表
├── Detect()        → 正则匹配检测 (ReDoS 保护)
└── GetDetectResult() → 获取并清空本周期违规记录
```

#### 安全正则 (regexutil)

防止正则表达式拒绝服务攻击 (ReDoS)：
- **最大长度限制**: 1024 字节
- **编译超时**: 5 秒
- 超时/编译失败的模式 → 记录警告，跳过该规则

#### TLS 证书管理 (legocmd)

基于 Lego (ACME 客户端) 封装：
- **DNS 验证模式**: 支持 100+ DNS 提供商（阿里云、Cloudflare、DNSPod 等）
- **HTTP 验证模式**: 端口 80 验证
- **证书路径**: `{config_dir}/cert/certificates/{domain}.crt`
- **自动续签**: 到期前 30 天自动续签

---

## 5. 数据流全景图

```
 ┌──────────────┐        ┌──────────────────┐
 │  前端面板     │        │   XrayR 后端      │
 │              │  HTTP  │                  │
 │ V2Board      │◄──────►│  panel.Panel     │
 │ V2Board      │  API   │  ├─ Config Load  │
 │              │        │  ├─ Core Start   │
 │              │        │  └─ Service Mgr  │
 └──────────────┘        └────────┬─────────┘
                                  │
                    ┌─────────────┼─────────────┐
                    │             │             │
              ┌─────▼────┐ ┌─────▼────┐ ┌─────▼────┐
              │Controller│ │Controller│ │Controller│
              │  Node 1  │ │  Node 2  │ │  Node N  │
              └─────┬────┘ └─────┬────┘ └─────┬────┘
                    │             │             │
                    └─────────────┼─────────────┘
                                  │
                    ┌─────────────▼─────────────┐
                    │     Xray-core Instance     │
                    │  ┌──────────────────────┐ │
                    │  │   mydispatcher       │ │
                    │  │  ├─ Limiter          │ │
                    │  │  ├─ RuleManager      │ │
                    │  │  └─ Stats            │ │
                    │  └──────────────────────┘ │
                    │  ┌──────────────────────┐ │
                    │  │   Proxy Handlers     │ │
                    │  │  ├─ Inbound          │ │
                    │  │  └─ Outbound         │ │
                    │  └──────────────────────┘ │
                    └───────────────────────────┘
                                  │
                           用户流量入口/出口
```

---

## 6. Tag 命名规范

系统内部使用 Tag 来标识和管理入站/出站规则：

| 组件 | Tag 格式 | 示例 |
|------|---------|------|
| 入站/出站 | `{NodeType}_{ListenIP}_{Port}` | `V2ray_0.0.0.0_443` |
| SS-Plugin | `dokodemo-door_{NodeType}_{ListenIP}_{Port}+1` | `dokodemo-door_Shadowsocks-Plugin_0.0.0.0_1235` |
| 用户标识 | `{Tag}\|{Email}\|{UID}` | `V2ray_0.0.0.0_443\|user@test.com\|1001` |
| 流量统计 | `user>>>{UserTag}>>>traffic>>>uplink` | `user>>>V2ray_0.0.0.0_443\|...>>>traffic>>>uplink` |

---

## 7. 支持的协议与传输方式

| 协议类型 | 传输协议 | TLS/REALITY | Fallback | 多路复用 |
|---------|---------|-------------|----------|----------|
| VLESS | tcp, ws, splithttp, grpc | TLS / REALITY | ✓ | ✗ |
| AnyTLS | tcp | TLS | ✓ | ✓ (內建) |

**安全类型演进：**
- `tls` → 标准 TLS，需配置证书
- `reality` → **推荐**，无证书防探测，需面板下发 REALITY 参数

**加密方式**:
- VLESS: none (无内层加密，依赖外层 TLS/REALITY)
- AnyTLS: SHA256(密码) 认证 + TLS 加密 + 可自定义 padding 混淆

---

## 8. 热重载机制

```
config.yml 变更
       │
       ▼ (fsnotify 事件, 3秒防抖)
  p.Close()
       │
       ├── 停止所有 Controller (关闭定时器)
       ├── 关闭 Xray-core 实例
       └── 清空 Service 列表
       │
       ▼ (触发 GC)
  config.Unmarshal() → 重新读取配置
       │
       ▼
  p.Start()
       │
       ├── 创建新的 Xray-core 实例
       ├── 创建新的 Controller 列表
       └── 启动所有服务
```

---

## 9. 关键设计模式

| 模式 | 应用位置 | 说明 |
|------|---------|------|
| **策略模式** | api.API 接口 | 不同面板 API 可插拔替换 |
| **观察者模式** | fsnotify + OnConfigChange | 配置文件变更自动响应 |
| **模板方法** | InboundBuilder | 统一的入站构建流程，根据协议不同实现细节 |
| **装饰器模式** | RateWriter / SizeStatWriter | 在原有 Writer 上包装限速/统计功能 |
| **单例模式** | core.Instance | 全局唯一 Xray-core 实例 |
| **工厂方法** | panel.New() / controller.New() | 统一的对象创建入口 |

---

## 10. 构建与部署

### 构建命令

```bash
# 编译
go build -v -o XrayR -trimpath -ldflags "-s -w -buildid=" ./main

# Docker 构建
docker build -t xrayr .
```

### 运行时文件布局

```
/etc/XrayR/
├── config.yml           # 主配置文件 (YAML)
├── dns.json             # DNS 配置 (可选)
├── route.json           # 路由规则 (可选)
├── custom_inbound.json  # 自定义入站 (可选)
├── custom_outbound.json # 自定义出站 (可选)
├── rulelist             # 本地审计规则 (可选)
├── geoip.dat            # GeoIP 数据库
├── geosite.dat          # GeoSite 数据库
└── cert/                # TLS 证书目录
    └── certificates/
        ├── example.com.crt
        └── example.com.key
```

### Docker 运行

```bash
docker run -d \
  --name xrayr \
  --restart always \
  --network host \
  -v /etc/XrayR:/etc/XrayR \
  xrayr
```

---

## 11. 架构扩展指南

本文档说明如何在当前架构上扩展支持其他协议（Trojan、Shadowsocks、VMess）或新增面板适配器。

### 11.1 扩展分层

```
┌─────────────────────────────────────────────┐
│                  panel/                      │  ← 入口层：根据 PanelType 创建 API 客户端
│   panel.go (switch PanelType → New adapter) │
├─────────────────────────────────────────────┤
│                   api/                       │  ← 适配器层：对接面板 API，返回统一数据模型
│   api.go (API 接口定义)                      │
│   apimodel.go (NodeInfo / UserInfo 模型)     │
│   v2board/ (面板适配器实现)                   │
├─────────────────────────────────────────────┤
│              service/controller/             │  ← 控制层：将 NodeInfo → Xray-core inbound 配置
│   inboundbuilder.go  (协议 + 传输 + 安全)     │
│   userbuilder.go     (用户账号构建)           │
│   outboundbuilder.go (出站构建)               │
└─────────────────────────────────────────────┘
```

扩展任何一个方向都需要修改 **3 层**。

### 11.2 新增协议（如 Trojan、Shadowsocks）

以添加 **Trojan** 为例，需要修改以下文件：

#### 11.2.1 `api/apimodel.go` — 数据模型（可能无需改动）

现有 `NodeInfo` 和 `UserInfo` 已包含 Trojan 所需的全部字段：

| 字段 | 用途 |
|------|------|
| `NodeType` | 设为 `"Trojan"` |
| `Port` | 监听端口 |
| `Host` | TLS SNI |
| `EnableTLS` | Trojan 必须为 true |
| `UserInfo.UUID` | Trojan 用作用户密码 |

#### 11.2.2 `service/controller/inboundbuilder.go` — 协议入站构建

在 `InboundBuilder` 中添加 Trojan 协议分支：

```go
// 在 protocol 声明后添加
} else if nodeInfo.NodeType == "Trojan" {
    protocol = "trojan"
    if config.EnableFallback {
        fallbackConfigs, err := buildTrojanFallbacks(config.FallBackConfigs)
        if err != nil {
            return nil, err
        }
        proxySetting = &conf.TrojanServerConfig{Fallbacks: fallbackConfigs}
    } else {
        proxySetting = &conf.TrojanServerConfig{}
    }
```

同时需恢复 `buildTrojanFallbacks` 函数（约 25 行，见 git 历史 `b23b025` 之前）。

#### 11.2.3 `service/controller/userbuilder.go` — 用户账号构建

添加 `buildTrojanUser` 函数：

```go
func (c *Controller) buildTrojanUser(userInfo *[]api.UserInfo) []*protocol.User {
    users := make([]*protocol.User, len(*userInfo))
    for i, user := range *userInfo {
        trojanAccount := &trojan.Account{Password: user.UUID}
        users[i] = &protocol.User{
            Level:   0,
            Email:   c.buildUserTag(&user),
            Account: serial.ToTypedMessage(trojanAccount),
        }
    }
    return users
}
```

需要的 import：
```go
"github.com/xtls/xray-core/proxy/trojan"
```

#### 11.2.4 `service/controller/controller.go` — 用户构建调度

在 `addNewUser` 中添加 Trojan 分支：

```go
case nodeInfo.NodeType == "Trojan":
    users = c.buildTrojanUser(userInfo)
```

#### 11.2.5 适配器层 — 返回 `NodeType: "Trojan"`

面板适配器需在 `GetNodeInfo` 返回的 `NodeInfo` 中设置：
```go
nodeInfo.NodeType = "Trojan"
```

### 11.3 新增面板适配器

以添加 **SSPanel-UIM** 为例：

#### 11.3.1 创建 `api/sspanel/` 目录

```
api/sspanel/
├── model.go      # 面板特有数据结构
└── sspanel.go    # 实现 api.API 接口
```

#### 11.3.2 实现 `api.API` 接口

```go
type API interface {
    GetNodeInfo() (*NodeInfo, error)
    GetUserList() (*[]UserInfo, error)
    ReportNodeStatus(*NodeStatus) error
    ReportNodeOnlineUsers(*[]OnlineUser) error
    ReportUserTraffic(*[]UserTraffic) error
    Describe() ClientInfo
    GetNodeRule() (*[]DetectRule, error)
    ReportIllegal(*[]DetectResult) error
    Debug()
}
```

关键点：
- `GetNodeInfo()` 必须返回 `api.NodeInfo`，其中 `NodeType` 决定协议
- `GetUserList()` 必须返回 `[]api.UserInfo`，字段填充规则同上
- 不需要的接口方法可以空实现（如 `return nil`）

#### 11.3.3 注册到 `panel/panel.go`

```go
import "github.com/HenZenKuriRIP/XrayR4u/api/sspanel"

// 在 switch 中添加
case "SSpanel":
    apiClient = sspanel.New(nodeConfig.ApiConfig)
```

#### 11.3.4 添加配置示例

在 `config.yml.template` 中添加对应 `PanelType` 的配置示例。

### 11.4 协议与适配器的关系

```
面板 API 返回          →   NodeInfo.NodeType   →   InboundBuilder 协议分支
────────────────────────────────────────────────────────────────────
UniProxy /user         →   "Vless"             →   vless
Deepbwork /user        →   "V2ray"             →   vmess (需 EnableVless=false)
TrojanTidalab /user    →   "Trojan"            →   trojan
SSpanel mu /user       →   "Shadowsocks"       →   shadowsocks
```

**关键规则：** `NodeInfo.NodeType` 是连接适配器层和控制层的唯一协议标识。适配器负责从面板 API 响应中提取数据并填充 `NodeInfo`/`UserInfo` 通用模型，控制层根据 `NodeType` 选择对应的 Xray-core 协议配置。

### 11.5 UserInfo 字段按协议使用

| 字段 | VLESS | VMess | Trojan | Shadowsocks |
|------|-------|-------|--------|-------------|
| `UID` | ✅ | ✅ | ✅ | ✅ |
| `UUID` | ✅ (id) | ✅ (id) | ✅ (password) | - |
| `Email` | ✅ | ✅ | ✅ | ✅ (secret) |
| `SpeedLimit` | ✅ | ✅ | ✅ | ✅ |
| `DeviceLimit` | ✅ | ✅ | ✅ | ✅ |
| `Passwd` | - | - | - | ✅ |
| `Port` | - | - | - | ✅ |
| `Method` | - | - | - | ✅ |
| `AlterID` | - | ✅ | - | - |

### 11.6 NodeInfo 字段按协议使用

| 字段 | VLESS | VMess | Trojan | Shadowsocks |
|------|-------|-------|--------|-------------|
| `NodeType` | Vless | V2ray | Trojan | Shadowsocks |
| `Port` | ✅ | ✅ | ✅ | ✅ |
| `EnableVless` | true | false | - | - |
| `EnableTLS` | ✅ | ✅ | ✅ | - |
| `TLSType` | tls/reality | tls/reality | tls | - |
| `EnableVision` | ✅ | ✅ | - | - |
| `TransportProtocol` | tcp/ws/grpc | tcp/ws/grpc | tcp | tcp |
| `Host` | ✅ | ✅ | ✅ | - |
| `Path` | ✅ | ✅ | - | - |
| `ServiceName` | ✅ | ✅ | - | - |
| `RealitySettings` | ✅ | ✅ | - | - |
| `CypherMethod` | - | - | - | ✅ |
| `AlterID` | - | ✅ | - | - |
| `Header` | ✅ | ✅ | - | - |

### 11.7 恢复被删除代码

直接查阅 git 历史中删除前的代码：

```bash
# 查看完整协议分支的 inboundbuilder.go
git show b23b025~1:service/controller/inboundbuilder.go

# 查看完整用户构建的 userbuilder.go
git show b23b025~1:service/controller/userbuilder.go

# 查看 SSPanel 适配器（如需要）
git show 174302e~1:api/sspanel/sspanel.go
```

### 11.8 快速检查清单

添加新协议时，确认以下文件都已修改：

- [ ] `api/apimodel.go` — 数据字段是否够用
- [ ] `api/v2board/v2board.go` — 适配器返回正确的 `NodeType` + 字段
- [ ] `service/controller/inboundbuilder.go` — 协议入站构建 + 安全配置
- [ ] `service/controller/userbuilder.go` — 用户账号构建
- [ ] `service/controller/controller.go` — `addNewUser` 分发
- [ ] `service/controller/controller.go` — `nodeInfoEqual` 排除字段（如 `CypherMethod` 只对 SS 有效）
- [ ] `config.yml.template` — 配置示例
- [ ] `README.md` — 特性表更新

添加新面板适配器时：

- [ ] 创建 `api/<panel>/model.go` + `<panel>.go`
- [ ] 实现 `api.API` 全部 9 个方法
- [ ] 在 `panel/panel.go` switch 中注册
- [ ] 添加配置示例到 `config.yml.template`
- [ ] 更新 `README.md` 面板支持表
