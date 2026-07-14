# XrayR4u

[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MPL%202.0-blue.svg)](LICENSE)
[![Xray-core](https://img.shields.io/badge/Xray--core-v1.260327.0-red.svg)](https://github.com/XTLS/Xray-core)

XrayR4u 是 [XrayR](https://github.com/XrayR-project/XrayR) 的社区维护分支，一个基于 Go 语言的后端代理管理框架，作为前端管理面板与 Xray-core 之间的桥梁。

> **XrayR4u = XrayR for you** — 开箱即用，持续更新。

## 免责声明

本项目仅供个人学习研究使用，不保证任何可用性，也不对使用本软件造成的任何后果负责。

## 核心特性

| 特性 | VLESS + REALITY | AnyTLS + TLS |
|------|:---:|:---:|
| 节点信息同步 | ✅ | ✅ |
| 用户流量统计 | ✅ | ✅ |
| 服务器信息上报 | ✅ | ✅ |
| 自动申请/续签 TLS 证书 | — | ✅ |
| 在线人数统计 & IP 限制 | ✅ | ✅ |
| 审计规则 | ✅ | ✅ |
| 节点端口 & 用户限速 | ✅ | ✅ |
| 自定义 DNS | ✅ | ✅ |
| REALITY 无证书防探测 | ✅ | — |
| 域名 TLS 证书 | ✅ | ✅ |
| 內建多路复用（mux） | — | ✅ |
| 流量填充混淆（padding） | — | ✅ |
| 抗主动探测 Fallback | — | ✅ |

- 永久开源且免费
- 支持 VLESS + REALITY + XTLS-Vision 流控
- 支持 AnyTLS 协议——TLS + 密码认证 + 內建多路复用 + 抗主动探测
- 支持单实例对接多面板、多节点，无需重复启动
- 支持限制在线 IP 数量
- 支持节点端口级别、用户级别限速
- 配置简单明了，修改配置自动热重载实例
- 內嵌 Xray-core v1.260327.0，无需单独安装

## 支持的前端面板

| 面板 | 协议 | 接口 |
|------|------|------|
| [K2Board](https://github.com/RIPHZK1998/K2board) / wyx2685/V2Board | VLESS + REALITY + Vision | UniProxy |
| [K2Board](https://github.com/RIPHZK1998/K2board) / wyx2685/V2Board | AnyTLS + TLS | UniProxy |

## 版本信息

| 项目 | 版本 |
|------|------|
| XrayR4u | v0.9.12 |
| Xray-core | v1.260327.0 |
| Go | 1.26+ |

## 快速安装

### 一键脚本部署（推荐）

```bash
bash <(curl -sSL https://raw.githubusercontent.com/HenZenKuriRIP/XrayR4u/main/deploy/install.sh)
```

脚本会交互式引导你输入面板信息并选择协议：

1. **面板地址** — 例如 `https://panel.your-domain.com`
2. **API Token** — 面板后台获取
3. **节点 ID** — 面板节点管理中对应的数字 ID
4. **协议选择** — VLESS + REALITY / AnyTLS + TLS / 双协议

输入完成后展示配置摘要，确认无误自动安装。

> 如果 VPS 无法访问 GitHub，可以先在本地编译 `XrayR` 再上传：`scp XrayR install.sh root@VPS_IP:/root/`，然后 `bash install.sh`（脚本优先使用本地文件）。

### Docker 部署

```bash
# 克隆仓库
git clone https://github.com/HenZenKuriRIP/XrayR4u.git
cd XrayR4u

# 构建镜像
docker build -t xrayr4u:latest .

# 运行容器
docker run -d \
  --name xrayr4u \
  --restart always \
  --network host \
  -v /etc/XrayR:/etc/XrayR \
  xrayr4u:latest
```

### 源码编译

```bash
git clone https://github.com/HenZenKuriRIP/XrayR4u.git
cd XrayR4u
go mod download
CGO_ENABLED=0 go build -v -o XrayR -trimpath -ldflags "-s -w -buildid=" ./main
```

> 详细安装说明请参考 [INSTALL.md](INSTALL.md) 和 [deploy/README.md](deploy/README.md)

## 项目结构

```
XrayR4u/
├── main/                    # 程序入口 & 配置文件
│   └── main.go              # 主入口：配置加载、启动、热重载
├── panel/                   # 面板对接层
├── api/                     # 各面板 API 实现
│   └── v2board/             # V2Board UniProxy 适配器
├── service/
│   └── controller/          # 控制器：节点监控、用户同步、入站/出站构建
├── app/
│   └── mydispatcher/        # Xray-core 自定义调度器（限速/统计/规则）
├── proxy/
│   └── anytls/              # AnyTLS 协议实现
├── common/                  # 公共工具库（限速器、规则引擎、TLS 证书管理）
├── deploy/                  # 部署文件
│   ├── install.sh           # 一键安装脚本
│   └── README.md            # VPS 部署说明
├── Dockerfile               # Docker 构建文件
├── config.yml.template      # 配置模板（完整）
├── INSTALL.md               # 详细安装文档
└── ARCHITECTURE.md          # 架构说明与扩展指南
```

## 配置示例

### VLESS + REALITY（推荐，无需证书）

```yaml
Nodes:
  - PanelType: "V2board"
    ApiConfig:
      ApiHost: "https://panel.your-domain.com"
      ApiKey: "your-api-token"
      NodeID: 1
      NodeType: Vless
      EnableReality: true
    ControllerConfig:
      ListenIP: 0.0.0.0
      UpdatePeriodic: 60
      CertConfig:
        CertMode: none
```

### AnyTLS + TLS（需域名证书 + 抗主动探测）

```yaml
Nodes:
  - PanelType: "V2board"
    ApiConfig:
      ApiHost: "https://panel.your-domain.com"
      ApiKey: "your-api-token"
      NodeID: 2
      NodeType: anytls
    ControllerConfig:
      ListenIP: 0.0.0.0
      UpdatePeriodic: 60
      AnyTLSFallback: "127.0.0.1:8443"   # 抗主动探测（强烈推荐）
      CertConfig:
        CertMode: file
        CertDomain: "anytls.your-domain.com"
        CertFile: /etc/ssl/anytls.your-domain.com.crt
        KeyFile: /etc/ssl/anytls.your-domain.com.key
```

完整配置模板见 [config.yml.template](config.yml.template)。

## 常用命令

```bash
systemctl status xrayr          # 查看运行状态
journalctl -u xrayr -f          # 实时日志
ss -tlnp | grep XrayR           # 查看监听端口
systemctl restart xrayr         # 重启服务
vi /etc/XrayR/config.yml        # 编辑配置
```

## 文档

- [ARCHITECTURE.md](ARCHITECTURE.md) — 架构说明、数据流、设计模式、扩展指南
- [INSTALL.md](INSTALL.md) — 详细安装文档
- [deploy/README.md](deploy/README.md) — VPS 部署说明与故障排除
- [deploy/ANYTLS_PANEL_INTEGRATION.md](deploy/ANYTLS_PANEL_INTEGRATION.md) — AnyTLS 面板对接指南

## 感谢

- [Project X](https://github.com/XTLS/)
- [XrayR](https://github.com/XrayR-project/XrayR) (上游项目)
- [V2Fly](https://github.com/v2fly)
- [Air-Universe](https://github.com/crossfw/Air-Universe)

## 许可证

[Mozilla Public License Version 2.0](LICENSE)
