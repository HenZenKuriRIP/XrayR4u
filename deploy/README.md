# XrayR4u v0.9.12 VPS 部署包

## 适用场景

| 项目 | 要求 |
|------|------|
| 系统 | Debian 12/13, Ubuntu 20.04+/22.04+/24.04+ |
| 架构 | amd64 / arm64 |
| 面板 | K2Board / wyx2685/V2Board (UniProxy 接口) |
| 协议 | VLESS + REALITY / AnyTLS + TLS |
| 核心 | Xray-core v1.260327.0 (內嵌) |

## 一行命令部署

```bash
bash <(curl -sSL https://raw.githubusercontent.com/HenZenKuriRIP/XrayR4u/main/deploy/install.sh)
```

脚本会引导你逐步输入：
1. 面板地址（如 `https://panel.your-domain.com`）
2. API Token（面板后台获取）
3. 节点 ID（数字）
4. 协议选择（VLESS + REALITY / AnyTLS + TLS / 双协议）
5. AnyTLS 模式下需额外输入域名和 TLS 证书配置

输入完成后展示配置摘要，确认无误后自动安装。**无需手动编辑脚本，无需手动下载二进制文件。**

> XrayR 二进制文件需本地编译后上传。将编译好的 `XrayR` 放在脚本同级目录，脚本会优先使用本地文件。

## 文件说明

```
deploy/
├── install.sh                     # 一键交互式部署脚本
├── README.md                      # 本文件
└── ANYTLS_PANEL_INTEGRATION.md    # AnyTLS 面板对接指南
```

## 部署步骤

### 方法一：一行命令（推荐）

```bash
# SSH 到 VPS 后直接运行
bash <(curl -sSL https://raw.githubusercontent.com/HenZenKuriRIP/XrayR4u/main/deploy/install.sh)
```

按提示输入面板信息 → 选择协议 → 确认 → 等待安装完成。

### 方法二：先下载脚本再运行

```bash
ssh root@VPS_IP

# 1. 下载脚本
wget https://raw.githubusercontent.com/HenZenKuriRIP/XrayR4u/main/deploy/install.sh

# 2. 运行（交互式输入配置）
bash install.sh
```

### 方法三：本地编译 + 上传（离线 / 自定义）

```bash
# 在本地编译 XrayR
cd XrayR4u
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -v -o XrayR -trimpath -ldflags "-s -w -buildid=" ./main

# 上传到 VPS（和 install.sh 放同一目录）
scp XrayR install.sh root@VPS_IP:/root/

# SSH 到 VPS 运行
ssh root@VPS_IP
bash install.sh
# 脚本检测到同级目录有 XrayR 文件会自动使用，跳过网络下载
```

## 安装过程

脚本会按以下步骤逐步执行（每步有停顿，可看清输出）：

| 步骤 | 内容 |
|------|------|
| 第一步 | 交互式输入面板连接信息 |
| 第二步 | 协议选择（VLESS / AnyTLS / 双协议）+ 域名&TLS 证书配置 |
| 第三步 | 展示配置摘要，确认无误后继续 |
| 第四步 | 系统环境检查（OS、架构、依赖） |
| 第五步 | 创建目录结构 |
| 第六步 | 安装 XrayR 核心程序（本地文件优先） |
| 第七步 | 下载 Geo 数据文件（geoip.dat / geosite.dat） |
| 第八步 | 生成配置文件 |
| 第九步 | 安装 systemd 服务 |
| 第十步 | 系统内核优化（BBR） |
| 第十一步 | 防火墙配置 |
| 第十二步 | 启动 XrayR 服务 |

## 面板端配置

### VLESS + REALITY 节点（NodeType=Vless）

控制面板中创建 VLESS 节点，配置以下参数：

```json
{
  "port": 443,
  "network": "tcp",
  "tls": 2,
  "flow": "xtls-rprx-vision",
  "tls_settings": {
    "server_name": "www.microsoft.com",
    "dest": "www.microsoft.com",
    "server_port": "443",
    "private_key": "用 xray x25519 生成",
    "public_key": "对应的公钥",
    "short_id": "自定义短ID",
    "allow_insecure": "0"
  }
}
```

- `tls: 0` = 无 TLS, `tls: 1` = TLS, `tls: 2` = REALITY
- REALITY 无需证书，`tls: 2` 时面板下发的 `tls_settings` 自动构建 REALITY 配置

### AnyTLS + TLS 节点（NodeType=anytls）

控制面板中创建 AnyTLS 节点，配置以下参数：

```json
{
  "port": 443,
  "network": "tcp",
  "tls": 1,
  "tls_settings": {
    "server_name": "anytls.your-domain.com"
  }
}
```

- `tls` **必须为 `1`**（不是 REALITY 的 `tls: 2`）
- 不需要 `flow` / `public_key` / `private_key` 等 REALITY 专有字段
- VPS 上需要配置对应域名的 TLS 证书
- 强烈建议配置 Fallback（抗主动探测），详见 [ANYTLS_PANEL_INTEGRATION.md](ANYTLS_PANEL_INTEGRATION.md)

## 管理命令

```bash
systemctl status xrayr          # 查看状态
journalctl -u xrayr -f          # 实时日志
ss -tlnp | grep XrayR           # 监听端口
systemctl restart xrayr         # 重启
systemctl stop xrayr            # 停止
vi /etc/XrayR/config.yml        # 编辑配置
```

## 故障排除

### 启动失败

```bash
journalctl -u xrayr -n 50       # 查看错误
```

常见原因：
- `permission denied` → `chown -R root:root /etc/XrayR && chmod 755 /etc/XrayR/log`
- `节点不存在` → 面板 NodeID/Token 不匹配，用 curl 测试面板 API
- `failed to create instance` → 配置文件 YAML 缩进错误
- AnyTLS: `TLS certificate not found` → 检查证书路径和文件权限

### 测试面板 API

```bash
# VLESS 节点
curl -s "https://面板地址/api/v1/server/UniProxy/config?node_id=1&token=你的token&node_type=vless&local_port=1"

# AnyTLS 节点
curl -s "https://面板地址/api/v1/server/UniProxy/config?node_id=2&token=你的token&node_type=anytls&local_port=1"
```

### 端口不通

按顺序排查：
1. **云控制台防火墙**（最常见） → 放行 TCP 443 和 80
2. 系统防火墙 → `iptables -L -n | grep 443`
3. XrayR4u 监听 → `ss -tlnp | grep XrayR`
4. 面板端口配置 → 节点 port 字段是否正确
5. 本地测试 → `nc -zv VPS_IP 443`

### 修改面板连接信息

安装后如需修改面板地址、Token 或节点 ID：
```bash
vi /etc/XrayR/config.yml    # 修改 ApiHost / ApiKey / NodeID
systemctl restart xrayr     # 重启生效
```

## 相关链接

- GitHub 仓库: [https://github.com/HenZenKuriRIP/XrayR4u](https://github.com/HenZenKuriRIP/XrayR4u)
- 详细文档: [../INSTALL.md](../INSTALL.md)
- 架构说明: [../ARCHITECTURE.md](../ARCHITECTURE.md)
- AnyTLS 面板对接: [ANYTLS_PANEL_INTEGRATION.md](ANYTLS_PANEL_INTEGRATION.md)
- 上游项目: [XrayR](https://github.com/XrayR-project/XrayR)
