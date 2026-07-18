# XrayR 安装说明文档

## 1. 环境要求

| 项目 | 要求 |
|------|------|
| 操作系统 | Linux (推荐 Debian 10+/Ubuntu 18.04+/CentOS 7+) |
| 架构 | amd64 / arm64 |
| Go 版本 | 1.26+ (仅源码编译需要) |
| 内存 | 最低 256MB，推荐 512MB+ |
| 磁盘 | 最低 100MB |
| 网络 | 能访问面板 API 地址，公网节点需开放对应端口 |

### 依赖项

- **Xray-core**: 已内嵌 **v26.7.11**，无需单独安装。支持 VLESS + REALITY + Vision、TLS + XHTTP（CDN）、VLESS Encryption / REALITY 后量子、AnyTLS
- **证书**: REALITY 模式无需证书。TLS / CDN 源站需配置 CertConfig；Let's Encrypt 需域名解析到服务器
- **面板对接**: 见 [deploy/K2BOARD_INTEGRATION.md](deploy/K2BOARD_INTEGRATION.md)

---

## 2. 安装方式

### 2.1 一键脚本安装 (推荐)

```bash
bash <(curl -sSL https://raw.githubusercontent.com/HenZenKuriRIP/XrayR4u/main/deploy/install.sh)
```

脚本会自动完成：
- 检测系统环境
- 下载并安装 XrayR
- 创建 systemd 服务
- 引导配置 config.yml

---

### 2.2 Docker 安装

#### 2.2.1 准备配置文件

```bash
# 创建配置目录
mkdir -p /etc/XrayR/cert/certificates

# 下载配置模板
curl -o /etc/XrayR/config.yml https://raw.githubusercontent.com/HenZenKuriRIP/XrayR4u/main/main/config.yml.example

# 编辑配置文件 (必须修改面板信息)
vi /etc/XrayR/config.yml
```

#### 2.2.2 准备核心数据文件 (可选)

```bash
# 下载 GeoIP 和 GeoSite 数据库
cd /etc/XrayR
curl -L -o geoip.dat https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat
curl -L -o geosite.dat https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat
```

#### 2.2.3 构建 Docker 镜像

```bash
# 克隆仓库
git clone https://github.com/HenZenKuriRIP/XrayR4u.git
cd XrayR4u

# 构建镜像
docker build -t xrayr:latest .
```

#### 2.2.4 运行容器

```bash
# 使用 host 网络模式运行 (推荐)
docker run -d \
  --name xrayr \
  --restart always \
  --network host \
  -v /etc/XrayR:/etc/XrayR \
  xrayr:latest

# 或使用端口映射方式
docker run -d \
  --name xrayr \
  --restart always \
  -v /etc/XrayR:/etc/XrayR \
  -p 443:443 \
  -p 80:80 \
  -p 10000-10100:10000-10100 \
  xrayr:latest
```

#### 2.2.5 查看日志

```bash
# 查看运行日志
docker logs -f xrayr

# 查看访问/错误日志 (如已配置)
tail -f /etc/XrayR/access.log
tail -f /etc/XrayR/error.log
```

---

### 2.3 源码编译安装

#### 2.3.1 安装 Go 环境

```bash
# 下载 Go 1.26+
wget https://go.dev/dl/go1.26.4.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.26.4.linux-amd64.tar.gz

# 配置环境变量
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
echo 'export GOPATH=$HOME/go' >> ~/.bashrc
source ~/.bashrc

# 验证安装
go version
```

#### 2.3.2 克隆并编译

```bash
# 克隆仓库
git clone https://github.com/HenZenKuriRIP/XrayR4u.git
cd XrayR4u

# 下载依赖
go mod download

# 编译
CGO_ENABLED=0 go build -v -o XrayR -trimpath -ldflags "-s -w -buildid=" ./main

# 验证编译产物
./XrayR --version
# 输出: XrayR 0.8.0 (A Xray backend that supports many panels)
```

#### 2.3.3 安装二进制文件

```bash
# 创建目录结构
sudo mkdir -p /etc/XrayR/cert/certificates

# 复制二进制文件
sudo cp XrayR /usr/local/bin/
sudo chmod +x /usr/local/bin/XrayR

# 复制配置文件
sudo cp main/config.yml.example /etc/XrayR/config.yml
sudo cp main/dns.json /etc/XrayR/dns.json
sudo cp main/route.json /etc/XrayR/route.json
sudo cp main/custom_inbound.json /etc/XrayR/custom_inbound.json
sudo cp main/custom_outbound.json /etc/XrayR/custom_outbound.json
sudo cp main/geoip.dat /etc/XrayR/ 2>/dev/null || true
sudo cp main/geosite.dat /etc/XrayR/ 2>/dev/null || true

# 编辑配置
sudo vi /etc/XrayR/config.yml
```

---

## 3. Systemd 服务配置

### 3.1 创建服务文件

```bash
sudo tee /etc/systemd/system/xrayr.service << 'EOF'
[Unit]
Description=XrayR - A Xray backend framework
Documentation=https://github.com/HenZenKuriRIP/XrayR4u
After=network.target nss-lookup.target
Wants=network.target

[Service]
Type=simple
User=root
CapabilityBoundingSet=CAP_NET_BIND_SERVICE CAP_NET_RAW CAP_DAC_OVERRIDE
AmbientCapabilities=CAP_NET_BIND_SERVICE CAP_NET_RAW CAP_DAC_OVERRIDE
NoNewPrivileges=yes

# 启动命令 (根据需要修改参数)
ExecStart=/usr/local/bin/XrayR --config /etc/XrayR/config.yml

# 优雅关闭
ExecStop=/bin/kill -s SIGINT $MAINPID

# 自动重启
Restart=on-failure
RestartSec=5s

# 资源限制
LimitNOFILE=1048576
LimitNPROC=512

# 日志
StandardOutput=journal
StandardError=journal
SyslogIdentifier=xrayr

[Install]
WantedBy=multi-user.target
EOF
```

### 3.2 启动与管理

```bash
# 重新加载 systemd 配置
sudo systemctl daemon-reload

# 启动 XrayR
sudo systemctl start xrayr

# 设置开机自启
sudo systemctl enable xrayr

# 查看运行状态
sudo systemctl status xrayr

# 查看日志
sudo journalctl -u xrayr -f

# 重启服务 (修改配置文件后)
sudo systemctl restart xrayr

# 停止服务
sudo systemctl stop xrayr
```

---

## 4. 配置模板

详细配置模板请参考 `config.yml.template` 文件。以下是最小可运行配置示例：

### 4.1 最小配置 (V2Board + Vless)

```yaml
Log:
  Level: warning
Nodes:
  - PanelType: "V2board"
    ApiConfig:
      ApiHost: "https://your-v2board.com"
      ApiKey: "your-token"
      NodeID: 1
      NodeType: Vless
    ControllerConfig:
      ListenIP: 0.0.0.0
      UpdatePeriodic: 60
      CertConfig:
        CertMode: none
```

### 4.2 最小配置 (V2Board + TLS)

```yaml
Log:
  Level: warning
Nodes:
  - PanelType: "V2board"
    ApiConfig:
      ApiHost: "https://your-v2board.com"
      ApiKey: "your-token"
      NodeID: 1
      NodeType: Vless
    ControllerConfig:
      ListenIP: 0.0.0.0
      UpdatePeriodic: 60
      CertConfig:
        CertMode: file
        CertDomain: "node.example.com"
        CertFile: /etc/XrayR/cert/fullchain.pem
        KeyFile: /etc/XrayR/cert/privkey.pem
```

---

## 5. 面板对接配置参考

### 5.1 V2Board 对接

**面板端要求:**
- V2Board 版本 >= 1.5.5
- 在面板中创建服务器节点

**XrayR 端配置要点:**
```yaml
PanelType: "V2board"
ApiConfig:
  ApiHost: "https://面板地址"
  ApiKey: "面板通信密钥(token)"
  NodeID: 面板中节点的 ID
  NodeType: Vless
  EnableReality: true    # 启用 REALITY 协议 (推荐)
```

**注意:**
- V2Board 的 API Key 使用 `token` 参数传递
- **REALITY 模式下无需配置 TLS 证书，面板端需配置 serverNames/privateKey/shortIds 等参数**

---

## 6. TLS 证书配置详解

### 6.1 使用已有证书 (CertMode: file)

```yaml
CertConfig:
  CertMode: file
  CertDomain: "node.example.com"
  CertFile: /etc/XrayR/cert/fullchain.pem
  KeyFile: /etc/XrayR/cert/privkey.pem
```

可从以下方式获取证书文件：
- Let's Encrypt 手动申请 (certbot)
- 商业 SSL 证书
- Cloudflare Origin Certificate

### 6.2 DNS 验证自动申请 (CertMode: dns)

```yaml
CertConfig:
  CertMode: dns
  CertDomain: "node.example.com"
  Provider: alidns              # DNS 提供商
  Email: "your-email@example.com"
  DNSEnv:
    ALICLOUD_ACCESS_KEY: "your-key"
    ALICLOUD_SECRET_KEY: "your-secret"
```

**常用 DNS 提供商环境变量:**

| 提供商 | Provider | 环境变量 |
|--------|----------|---------|
| 阿里云 DNS | alidns | `ALICLOUD_ACCESS_KEY`, `ALICLOUD_SECRET_KEY` |
| Cloudflare | cloudflare | `CF_DNS_API_TOKEN` 或 `CF_API_EMAIL` + `CF_API_KEY` |
| DNSPod | dnspod | `DNSPOD_API_KEY` |
| GoDaddy | godaddy | `GODADDY_API_KEY`, `GODADDY_API_SECRET` |
| Namecheap | namecheap | `NAMECHEAP_API_USER`, `NAMECHEAP_API_KEY` |
| 华为云 DNS | huaweicloud | `HUAWEICLOUD_ACCESS_KEY_ID`, `HUAWEICLOUD_SECRET_ACCESS_KEY` |

> 完整支持列表: https://go-acme.github.io/lego/dns/

### 6.3 HTTP 验证自动申请 (CertMode: http)

```yaml
CertConfig:
  CertMode: http
  CertDomain: "node.example.com"
  Email: "your-email@example.com"
```

**前提条件:**
- 服务器 80 端口必须对外开放
- 域名必须已解析到服务器 IP
- 确保 80 端口未被其他程序占用

### 6.4 REALITY 模式（无需证书，推荐）

```yaml
# XrayR 端无需配置 CertConfig，REALITY 参数由面板下发
ApiConfig:
  EnableReality: true
  EnableVless: true
```

**面板端需配置的 REALITY 参数：**

| 参数 | 说明 | 示例 |
|------|------|------|
| `serverNames` | 伪装的目标网站域名列表 | `["www.microsoft.com", "www.apple.com"]` |
| `privateKey` | 服务端临时私钥 (xray x25519 生成) | 运行 `xray x25519` 获取 |
| `shortIds` | 短 ID 列表，客户端必须携带其一 | `["abc123", "def456"]` |
| `dest` | 回退目标地址 | `"www.microsoft.com:443"` |
| `min_client_ver` | 可选，面板下发的最低客户端版本 | `"1.8.0"` |
| `mldsa65_seed` | 可选，REALITY ML-DSA-65 后量子签名 seed | `xray mldsa65` 的 Seed |
| 顶层 `decryption` | 可选，VLESS Encryption 服务端串 | 默认 `none` |

**节点端 REALITY / VLESS 解析优先级（config.yml 非空优先）：**

| 项 | 优先级 |
|---|---|
| `minClientVer` | `RealityMinClientVer` → 面板 → `"1.8.0"` |
| `mldsa65Seed` | `RealityMldsa65Seed` → 面板 → 关闭 |
| `decryption` | `VlessDecryption` → 面板 → `"none"` |

> xray-core ≥ 26.7.11：若最终配置未设置 `minClientVer`，核心会默认 `26.3.27`，
> Mihomo 等第三方客户端常上报 `1.8.x` 会被 REALITY 拒绝。**请保持显式配置**，
> 兼容场景推荐 `1.8.0`，仅允许较新官方客户端时可设 `26.3.27`。

**后量子与 CDN（详见 [deploy/K2BOARD_INTEGRATION.md](deploy/K2BOARD_INTEGRATION.md) / [deploy/TLS_XHTTP_CDN_PQ.md](deploy/TLS_XHTTP_CDN_PQ.md)）：**

1. **ML-DSA-65**：可选签名，面板 `mldsa65_seed`  
2. **X25519MLKEM768**：dest 支持时自动协商，无需字段  
3. **VLESS Encryption**：可选 `decryption`，不可与 Fallback 同开  
4. **TLS + XHTTP + CDN**：节点 `Transport/XHTTP/Security` 可本地完整覆盖  

**优势：**
- 无需购买或申请 TLS 证书
- 流量与目标网站完全不可区分
- 可抵抗主动探测（探测请求返回目标网站真实内容）
- 可选后量子签名 / 密钥协商 / 载荷加密

### 6.5 禁用 TLS (CertMode: none)

```yaml
CertConfig:
  CertMode: none
```

适用于：
- CDN 回源场景 (CDN 已提供 TLS)
- 内网节点

---

## 7. 可选配置文件

### 7.1 DNS 配置 (dns.json)

```json
{
    "servers": [
        "1.1.1.1",
        "8.8.8.8",
        "localhost"
    ],
    "tag": "dns_inbound"
}
```

### 7.2 路由配置 (route.json)

```json
{
    "domainStrategy": "IPOnDemand",
    "rules": [
        {
            "type": "field",
            "outboundTag": "block",
            "ip": ["geoip:private"]
        },
        {
            "type": "field",
            "outboundTag": "block",
            "protocol": ["bittorrent"]
        }
    ]
}
```

### 7.3 自定义出站 (custom_outbound.json)

```json
[
    {
        "tag": "IPv4_out",
        "protocol": "freedom"
    },
    {
        "tag": "IPv6_out",
        "protocol": "freedom",
        "settings": {
            "domainStrategy": "UseIPv6"
        }
    },
    {
        "tag": "block",
        "protocol": "blackhole"
    }
]
```

> **注意**: xray-core v1.26x 起已彻底移除 `http`/`quic` 传输协议、`xtls` 安全类型。
> 新增 `splithttp`（XHTTP）传输协议、`reality` 安全类型。

### 7.4 自定义入站 (custom_inbound.json)

```json
[
    {
        "listen": "0.0.0.0",
        "port": 1080,
        "protocol": "socks",
        "settings": {
            "auth": "noauth",
            "udp": true
        }
    }
]
```

### 7.5 本地审计规则 (rulelist)

```
# 每行一个正则表达式
# 匹配的域名/地址将被拒绝连接
.*\.example\.com
.*torrent.*
.*porn.*
.*\.gov\.cn
```

---

## 8. 防火墙配置

```bash
# 如果使用 UFW
sudo ufw allow 443/tcp      # HTTPS / VLESS+TLS
sudo ufw allow 80/tcp       # HTTP (证书验证)
sudo ufw allow 10000:10100  # 用户端口范围 (根据实际配置调整)

# 如果使用 firewalld
sudo firewall-cmd --permanent --add-port=443/tcp
sudo firewall-cmd --permanent --add-port=80/tcp
sudo firewall-cmd --permanent --add-port=10000-10100/tcp
sudo firewall-cmd --reload

# 如果使用 iptables
sudo iptables -A INPUT -p tcp --dport 443 -j ACCEPT
sudo iptables -A INPUT -p tcp --dport 80 -j ACCEPT
```

---

## 9. 性能优化建议

### 9.1 系统优化

```bash
# 提高文件描述符限制
echo "fs.file-max = 1048576" | sudo tee -a /etc/sysctl.conf

# 优化 TCP 参数
sudo tee -a /etc/sysctl.conf << 'EOF'
net.core.default_qdisc = fq
net.ipv4.tcp_congestion_control = bbr
net.ipv4.tcp_rmem = 4096 87380 16777216
net.ipv4.tcp_wmem = 4096 65536 16777216
net.ipv4.tcp_slow_start_after_idle = 0
net.core.netdev_max_backlog = 16384
EOF

sudo sysctl -p
```

### 9.2 XrayR 配置优化

```yaml
ConnetionConfig:
  BufferSize: 64       # 缓存大小，可以根据内存调整 (32-128 kB)
  Handshake: 4         # 握手超时
  ConnIdle: 30         # 空闲连接超时

ControllerConfig:
  UpdatePeriodic: 60   # 同步间隔，负载大的节点可调大到 120-300
```

---

## 10. 故障排除

### 10.1 常见问题

| 问题 | 原因 | 解决方案 |
|------|------|---------|
| 服务无法启动 | 配置文件格式错误 | 检查 YAML 缩进，使用 `yamllint` 验证 |
| 证书申请失败 | DNS 记录未生效 / API 密钥错误 | 确认域名解析，检查 DNSEnv 环境变量 |
| 无法连接面板 API | 面板地址不可达 / 防火墙拦截 | `curl` 测试面板连接，检查防火墙 |
| 用户无法连接 | 端口未开放 / 证书问题 | 检查端口监听 `ss -tlnp`，检查证书路径 |
| 频繁重启 | 系统资源不足 / 配置热重载冲突 | `journalctl -u xrayr -n 50` 查看日志 |

### 10.2 查看日志

```bash
# systemd 日志
journalctl -u xrayr -f --no-pager

# 容器日志
docker logs -f xrayr

# 配置文件中的访问/错误日志
tail -f /etc/XrayR/access.log
tail -f /etc/XrayR/error.log
```

### 10.3 调试模式

临时启用调试日志：
```yaml
Log:
  Level: debug
```

修改后重启服务即可看到详细的调试输出。

---

## 11. 升级指南

### Docker 升级

```bash
# 拉取最新代码
cd XrayR4u && git pull

# 重新构建
docker build -t xrayr:latest .

# 停止旧容器
docker stop xrayr && docker rm xrayr

# 启动新容器
docker run -d \
  --name xrayr \
  --restart always \
  --network host \
  -v /etc/XrayR:/etc/XrayR \
  xrayr:latest
```

### 源码升级

```bash
# 拉取最新代码
cd XrayR4u && git pull

# 重新编译
CGO_ENABLED=0 go build -v -o XrayR -trimpath -ldflags "-s -w -buildid=" ./main

# 替换二进制文件
sudo systemctl stop xrayr
sudo cp XrayR /usr/local/bin/
sudo systemctl start xrayr
```

---

## 12. 快速检查清单

安装完成后，按以下步骤验证：

- [ ] `XrayR --version` 能否正常输出版本
- [ ] `systemctl status xrayr` 显示 `active (running)`
- [ ] `journalctl -u xrayr -n 20` 无报错，显示 "Xray Core Version"
- [ ] 日志中显示 `[Vless: NodeID] Start monitor node status`
- [ ] 面板后台节点显示在线
- [ ] 用户能正常连接并使用代理
- [ ] 面板后台能看到用户流量统计数据
- [ ] 如启用 TLS，浏览器访问 `https://节点域名` 证书正常
