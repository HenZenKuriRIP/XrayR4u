#!/usr/bin/env bash
# ============================================================================
# XrayR v0.9.12 VPS 一键部署脚本
# 支持: Debian 12/13, Ubuntu 20.04+/22.04+/24.04+
# 内置 xray-core v1.260327.0 | VLESS + REALITY / AnyTLS + TLS
#
# 用法:
#   bash install.sh             安装 XrayR
#   bash install.sh --uninstall  卸载 XrayR
# ============================================================================
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

log_info()  { echo -e "${GREEN}[✓]${NC}  $*"; }
log_warn()  { echo -e "${YELLOW}[!]${NC}  $*"; }
log_error() { echo -e "${RED}[✗]${NC} $*"; exit 1; }
log_step()  { echo -e "\n${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"; echo -e "${BOLD}${CYAN}  $*${NC}"; echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}\n"; }
pause()     { sleep "${1:-2}"; }

# ============================================================================
# 固定变量
# ============================================================================
XRAYR_INSTALL_DIR="/etc/XrayR"
XRAYR_BIN_DIR="/usr/local/bin"
XRAYR_BIN_NAME="XrayR"
XRAYR_SERVICE="/etc/systemd/system/xrayr.service"
XRAYR_REPO="HenZenKuriRIP/XrayR4u"
XRAYR_VERSION="v0.9.12"

# ============================================================================
# 卸载函数
# ============================================================================
uninstall_xrayr() {
    clear
    echo ""
    echo -e "${RED}╔═══════════════════════════════════════════════════╗${NC}"
    echo -e "${RED}║                                                   ║${NC}"
    echo -e "${RED}║           ⚠️  XrayR 卸载脚本                       ║${NC}"
    echo -e "${RED}║                                                   ║${NC}"
    echo -e "${RED}╚═══════════════════════════════════════════════════╝${NC}"
    echo ""
    pause 1

    # 确认卸载
    echo -e "  ${RED}${BOLD}此操作将完全移除 XrayR，包括：${NC}"
    echo ""
    echo -e "  ${YELLOW}• 停止并删除 xrayr 系统服务${NC}"
    echo -e "  ${YELLOW}• 删除二进制文件 ${XRAYR_BIN_DIR}/${XRAYR_BIN_NAME}${NC}"
    echo -e "  ${YELLOW}• 可选择保留或删除配置/日志目录${NC}"
    echo ""
    echo -e "  ${RED}此操作不可逆！${NC}"
    echo ""

    while true; do
        echo -e "  ${BOLD}确认卸载 XrayR？${NC}"
        echo -e "  ${RED}[Y]${NC} 确认卸载   ${GREEN}[N]${NC} 取消"
        read -r -n 1 -p "  → " confirm
        echo ""
        case "$confirm" in
            Y|y) break ;;
            N|n) echo ""; log_info "已取消卸载"; exit 0 ;;
            *) log_warn "请输入 Y 或 N" ;;
        esac
    done

    echo ""
    log_step "开始卸载 XrayR"

    # 1. 停止并禁用服务
    echo ""
    if systemctl is-active --quiet xrayr 2>/dev/null; then
        log_info "正在停止 xrayr 服务..."
        systemctl stop xrayr
        log_info "xrayr 服务已停止"
    else
        log_info "xrayr 服务未运行，跳过停止"
    fi

    if systemctl is-enabled --quiet xrayr 2>/dev/null; then
        log_info "正在禁用 xrayr 开机自启..."
        systemctl disable xrayr
        log_info "xrayr 开机自启已禁用"
    else
        log_info "xrayr 未设置开机自启，跳过"
    fi

    # 2. 删除 systemd 服务文件
    if [[ -f "$XRAYR_SERVICE" ]]; then
        rm -f "$XRAYR_SERVICE"
        systemctl daemon-reload
        log_info "已删除 systemd 服务文件: ${XRAYR_SERVICE}"
    else
        log_info "systemd 服务文件不存在，跳过"
    fi

    # 3. 删除二进制文件
    if [[ -f "${XRAYR_BIN_DIR}/${XRAYR_BIN_NAME}" ]]; then
        rm -f "${XRAYR_BIN_DIR}/${XRAYR_BIN_NAME}"
        log_info "已删除二进制文件: ${XRAYR_BIN_DIR}/${XRAYR_BIN_NAME}"
    else
        log_info "二进制文件不存在，跳过"
    fi

    # 4. 询问是否保留配置目录
    echo ""
    echo -e "  ${BOLD}是否保留配置文件、日志和证书？${NC}"
    echo ""
    echo -e "  ${YELLOW}目录: ${XRAYR_INSTALL_DIR}/${NC}"
    echo -e "  ${YELLOW}包含: config.yml, config/, log/, cert/${NC}"
    echo ""
    echo -e "  ${GREEN}[Y]${NC} 保留（卸载后重新安装可复用）"
    echo -e "  ${RED}[N]${NC} 彻底删除"
    echo ""

    while true; do
        read -r -n 1 -p "  → " keep_config
        echo ""
        case "$keep_config" in
            Y|y)
                log_info "已保留配置目录: ${XRAYR_INSTALL_DIR}/"
                break
                ;;
            N|n)
                if [[ -d "$XRAYR_INSTALL_DIR" ]]; then
                    rm -rf "$XRAYR_INSTALL_DIR"
                    log_info "已彻底删除配置目录: ${XRAYR_INSTALL_DIR}/"
                else
                    log_info "配置目录不存在，跳过"
                fi
                break
                ;;
            *) log_warn "请输入 Y 或 N" ;;
        esac
    done

    # 5. 防火墙规则提示
    echo ""
    echo -e "  ${YELLOW}💡 防火墙规则（如有）需要手动清理：${NC}"
    echo ""
    if command -v ufw &>/dev/null; then
        echo -e "  ${YELLOW}  ufw delete allow 443/tcp${NC}"
        echo -e "  ${YELLOW}  ufw delete allow 80/tcp${NC}"
    fi
    if command -v firewall-cmd &>/dev/null; then
        echo -e "  ${YELLOW}  firewall-cmd --permanent --remove-port=443/tcp${NC}"
        echo -e "  ${YELLOW}  firewall-cmd --permanent --remove-port=80/tcp${NC}"
        echo -e "  ${YELLOW}  firewall-cmd --reload${NC}"
    fi
    if command -v iptables &>/dev/null; then
        echo -e "  ${YELLOW}  iptables -D INPUT -p tcp --dport 443 -j ACCEPT${NC}"
        echo -e "  ${YELLOW}  iptables -D INPUT -p tcp --dport 80 -j ACCEPT${NC}"
    fi
    echo ""

    # 完成
    echo -e "${GREEN}╔═══════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║                                                   ║${NC}"
    echo -e "${GREEN}║        ✅  XrayR 卸载完成！                        ║${NC}"
    echo -e "${GREEN}║                                                   ║${NC}"
    echo -e "${GREEN}╚═══════════════════════════════════════════════════╝${NC}"
    echo ""
    echo -e "  ${YELLOW}📝 卸载摘要${NC}"
    echo -e "  • 服务状态: 已停止并删除"
    echo -e "  • 二进制文件: 已删除"
    if [[ "$keep_config" =~ ^[Yy]$ ]]; then
        echo -e "  • 配置目录: ${GREEN}已保留${NC} → ${XRAYR_INSTALL_DIR}/"
    else
        echo -e "  • 配置目录: 已彻底删除"
    fi
    echo ""
    exit 0
}

# ============================================================================
# 命令行参数解析
# ============================================================================
if [[ "${1:-}" == "--uninstall" || "${1:-}" == "uninstall" ]]; then
    if [[ $EUID -ne 0 ]]; then
        echo "请使用 root 用户运行卸载: sudo bash install.sh --uninstall"
        exit 1
    fi
    uninstall_xrayr
fi

# ============================================================================
# 0. 欢迎 & 收集配置
# ============================================================================
clear
echo ""
echo -e "${CYAN}╔═══════════════════════════════════════════════════╗${NC}"
echo -e "${CYAN}║                                                   ║${NC}"
echo -e "${CYAN}║        ${BOLD}XrayR v0.9.12 — VPS 一键部署脚本${NC}${CYAN}             ║${NC}"
echo -e "${CYAN}║        VLESS + REALITY / AnyTLS + TLS             ║${NC}"
echo -e "${CYAN}║        适配 K2Board (UniProxy) 面板                 ║${NC}"
echo -e "${CYAN}║                                                   ║${NC}"
echo -e "${CYAN}╚═══════════════════════════════════════════════════╝${NC}"
echo ""
pause 2

# ---------------------------------------------------------------------------
# 询问面板配置
# ---------------------------------------------------------------------------
log_step "第一步：面板连接信息"

echo -e "  ${YELLOW}请准备好以下信息（在 V2Board 面板后台可找到）：${NC}"
echo ""
echo -e "  1. 面板地址     — 例如: ${GREEN}https://panel.your-domain.com${NC}"
echo -e "  2. API Token    — 面板后台 → 系统设置 → API Token（32位字符串）"
echo -e "  3. 节点 ID      — 面板后台 → 节点管理 → 节点ID（数字）"
echo -e "  4. 节点类型     — Vless（UniProxy 接口）"
echo ""
pause 2

# 面板地址
while true; do
    echo ""
    echo -e "  ${BOLD}请输入面板地址：${NC}"
    echo -e "  ${YELLOW}格式: https://your-panel.com （务必带 https://）${NC}"
    read -r -p "  → " V2BOARD_HOST

    if [[ -z "$V2BOARD_HOST" ]]; then
        log_warn "面板地址不能为空，请重新输入"
        continue
    fi

    if [[ ! "$V2BOARD_HOST" =~ ^https?:// ]]; then
        log_warn "面板地址必须以 http:// 或 https:// 开头"
        continue
    fi

    break
done

echo ""
echo -e "  ${GREEN}面板地址已记录: ${V2BOARD_HOST}${NC}"
pause 1

# API Token
while true; do
    echo ""
    echo -e "  ${BOLD}请输入面板 API Token：${NC}"
    echo -e "  ${YELLOW}面板后台 → 系统设置 → API 接口 → Token（32位字符串）${NC}"
    read -r -p "  → " V2BOARD_KEY

    if [[ -z "$V2BOARD_KEY" ]]; then
        log_warn "API Token 不能为空，请重新输入"
        continue
    fi

    if [[ ${#V2BOARD_KEY} -lt 10 ]]; then
        log_warn "API Token 看起来太短（少于10位），请确认是否正确"
        echo -e "  ${YELLOW}输入 Y 继续使用，其他任意键重新输入：${NC}"
        read -r -n 1 -p "  → " confirm
        if [[ "$confirm" != "Y" && "$confirm" != "y" ]]; then
            continue
        fi
    fi

    break
done

echo ""
echo -e "  ${GREEN}API Token 已记录: ${V2BOARD_KEY:0:8}****（已隐藏）${NC}"
pause 1

# 节点 ID
while true; do
    echo ""
    echo -e "  ${BOLD}请输入节点 ID：${NC}"
    echo -e "  ${YELLOW}面板后台 → 节点管理 → 查看节点 → NodeID（纯数字）${NC}"
    read -r -p "  → " NODE_ID

    if [[ -z "$NODE_ID" ]]; then
        log_warn "节点 ID 不能为空，请重新输入"
        continue
    fi

    if [[ ! "$NODE_ID" =~ ^[0-9]+$ ]]; then
        log_warn "节点 ID 必须是纯数字"
        continue
    fi

    break
done

echo ""
echo -e "  ${GREEN}节点 ID 已记录: ${NODE_ID}${NC}"
pause 1

# ---------------------------------------------------------------------------
# 协议选择
# ---------------------------------------------------------------------------
log_step "第二步：协议选择"

echo ""
echo -e "  ${BOLD}请选择要安装的协议：${NC}"
echo ""
echo -e "  ${GREEN}[1]${NC} VLESS + REALITY     — 无需域名，抗封锁标准方案"
echo -e "  ${GREEN}[2]${NC} AnyTLS + TLS        — 需要域名，自动申请证书"
echo -e "  ${GREEN}[3]${NC} VLESS + AnyTLS 双协议 — 同一 VPS 同时运行两种协议"
echo ""

INSTALL_MODE=""
while true; do
    read -r -n 1 -p "  → " mode
    echo ""
    case "$mode" in
        1) INSTALL_MODE="vless"; break ;;
        2) INSTALL_MODE="anytls"; break ;;
        3) INSTALL_MODE="dual"; break ;;
        *) log_warn "请输入 1、2 或 3" ;;
    esac
done

INSTALL_VLESS=false
INSTALL_ANYTLS=false
case "$INSTALL_MODE" in
    vless) INSTALL_VLESS=true ;;
    anytls) INSTALL_ANYTLS=true ;;
    dual) INSTALL_VLESS=true; INSTALL_ANYTLS=true ;;
esac

echo ""
echo -e "  ${GREEN}已选择: ${INSTALL_MODE}${NC}"
pause 1

# ---------------------------------------------------------------------------
# 域名 & TLS 证书（AnyTLS 需要）
# ---------------------------------------------------------------------------
DOMAIN_NAME=""
CERT_MODE=""
CERT_EMAIL=""
CERT_FILE_PATH=""
KEY_FILE_PATH=""
ANYTLS_FALLBACK=""
ANYTLS_NODE_ID=$NODE_ID  # 默认复用主 NodeID

# 双协议模式：面板上 VLESS 和 AnyTLS 是不同的节点，NodeID 不同
if $INSTALL_VLESS && $INSTALL_ANYTLS; then
    echo ""
    echo -e "  ${YELLOW}双协议模式：请分别输入 VLESS 和 AnyTLS 在面板上的节点 ID${NC}"
    echo -e "  ${GREEN}已记录的 NodeID ${NODE_ID} 将用于 VLESS${NC}"
    echo ""

    while true; do
        echo -e "  ${BOLD}请输入 AnyTLS 节点 ID：${NC}"
        read -r -p "  → " ANYTLS_NODE_ID
        if [[ -z "$ANYTLS_NODE_ID" ]]; then
            log_warn "节点 ID 不能为空"
            continue
        fi
        if [[ ! "$ANYTLS_NODE_ID" =~ ^[0-9]+$ ]]; then
            log_warn "节点 ID 必须是纯数字"
            continue
        fi
        break
    done
    echo -e "  ${GREEN}VLESS NodeID: ${NODE_ID}  |  AnyTLS NodeID: ${ANYTLS_NODE_ID}${NC}"
fi

if $INSTALL_ANYTLS; then
    echo ""
    log_step "第二步续：AnyTLS 域名 & TLS 证书"

    # 域名
    while true; do
        echo ""
        echo -e "  ${BOLD}请输入 AnyTLS 节点域名：${NC}"
        echo -e "  ${YELLOW}例如: anytls.your-domain.com（需提前将 DNS A 记录指向本机 IP）${NC}"
        read -r -p "  → " DOMAIN_NAME

        if [[ -z "$DOMAIN_NAME" ]]; then
            log_warn "域名不能为空"
            continue
        fi
        if [[ ! "$DOMAIN_NAME" =~ \. ]]; then
            log_warn "请输入完整域名（如 node.example.com）"
            continue
        fi
        break
    done
    echo ""
    echo -e "  ${GREEN}域名已记录: ${DOMAIN_NAME}${NC}"

    # TLS 证书 — 自动检测已有证书，检测不到再手动输入
    echo ""
    echo -e "  ${BOLD}━━━ TLS 证书配置 ━━━${NC}"
    echo ""

    # 按优先级搜索常见证书路径
    # 格式: "描述|crt路径|key路径"
    CANDIDATES=(
        "acme.sh (ECC)|/root/.acme.sh/${DOMAIN_NAME}_ecc/fullchain.cer|/root/.acme.sh/${DOMAIN_NAME}_ecc/${DOMAIN_NAME}.key"
        "acme.sh (RSA)|/root/.acme.sh/${DOMAIN_NAME}/fullchain.cer|/root/.acme.sh/${DOMAIN_NAME}/${DOMAIN_NAME}.key"
        "Let's Encrypt|/etc/letsencrypt/live/${DOMAIN_NAME}/fullchain.pem|/etc/letsencrypt/live/${DOMAIN_NAME}/privkey.pem"
        "/etc/ssl|/etc/ssl/${DOMAIN_NAME}.crt|/etc/ssl/${DOMAIN_NAME}.key"
        "/etc/ssl (pem)|/etc/ssl/${DOMAIN_NAME}.pem|/etc/ssl/${DOMAIN_NAME}.key"
    )

    FOUND_CERT=""
    FOUND_KEY=""
    FOUND_DESC=""

    echo -e "  ${CYAN}正在搜索域名 ${DOMAIN_NAME} 的已有证书...${NC}"
    echo ""
    for candidate in "${CANDIDATES[@]}"; do
        IFS='|' read -r desc crt key <<< "$candidate"
        if [[ -f "$crt" && -f "$key" ]]; then
            FOUND_CERT="$crt"
            FOUND_KEY="$key"
            FOUND_DESC="$desc"
            log_info "发现已有证书: ${desc}"
            echo -e "      证书: ${GREEN}${crt}${NC}"
            echo -e "      私钥: ${GREEN}${key}${NC}"
            break
        fi
    done

    if [[ -n "$FOUND_CERT" ]]; then
        echo ""
        echo -e "  ${BOLD}是否使用自动检测到的证书？${NC}"
        echo -e "  ${GREEN}[Y]${NC} 使用检测到的证书  ${YELLOW}[N]${NC} 手动指定路径"
        read -r -n 1 -p "  → " use_found
        echo ""
        if [[ "$use_found" = "Y" || "$use_found" = "y" ]]; then
            CERT_FILE_PATH="$FOUND_CERT"
            KEY_FILE_PATH="$FOUND_KEY"
            log_info "已自动配置: ${FOUND_DESC}"
        fi
    fi

    # 未自动检测到或用户选择手动输入
    if [[ -z "$CERT_FILE_PATH" ]]; then
        if [[ -n "$FOUND_CERT" ]]; then
            # 用户拒绝了已检测到的证书
            echo ""
        else
            echo -e "  ${YELLOW}未检测到域名 ${DOMAIN_NAME} 的已有证书${NC}"
        fi

        echo ""
        echo -e "  ${BOLD}请选择证书获取方式：${NC}"
        echo ""
        echo -e "  ${GREEN}[1]${NC} 自动申请 Let's Encrypt（HTTP 验证，需 80 端口对外开放）"
        echo -e "  ${GREEN}[2]${NC} 手动输入已有证书路径"
        echo ""

        while true; do
            read -r -n 1 -p "  → " cert_choice
            echo ""
            case "$cert_choice" in
                1)
                    CERT_MODE="http"
                    # 收集 Let's Encrypt 邮箱
                    while true; do
                        echo -e "  ${BOLD}请输入通知邮箱（Let's Encrypt 要求）：${NC}"
                        read -r -p "  → " CERT_EMAIL
                        if [[ -z "$CERT_EMAIL" || ! "$CERT_EMAIL" =~ @ ]]; then
                            log_warn "请输入有效的邮箱地址"
                            continue
                        fi
                        break
                    done
                    echo -e "  ${GREEN}证书模式: HTTP 自动申请${NC}"
                    echo -e "  ${GREEN}通知邮箱: ${CERT_EMAIL}${NC}"
                    echo ""
                    echo -e "  ${YELLOW}XrayR 启动时会自动向 Let's Encrypt 申请证书并保存到：${NC}"
                    echo -e "  ${YELLOW}  ${XRAYR_INSTALL_DIR}/cert/certificates/${DOMAIN_NAME}.crt${NC}"
                    echo -e "  ${YELLOW}  ${XRAYR_INSTALL_DIR}/cert/certificates/${DOMAIN_NAME}.key${NC}"
                    log_info "证书到期前 XrayR 会自动续期，无需手动维护"
                    break
                    ;;
                2)
                    CERT_MODE="file"
                    while true; do
                        echo -e "  ${BOLD}请输入证书文件完整路径（.crt / .pem）：${NC}"
                        read -r -p "  → " CERT_FILE_PATH
                        if [[ -f "$CERT_FILE_PATH" ]]; then
                            break
                        fi
                        log_warn "文件不存在: $CERT_FILE_PATH"
                    done
                    while true; do
                        echo -e "  ${BOLD}请输入私钥文件完整路径（.key）：${NC}"
                        read -r -p "  → " KEY_FILE_PATH
                        if [[ -f "$KEY_FILE_PATH" ]]; then
                            break
                        fi
                        log_warn "文件不存在: $KEY_FILE_PATH"
                    done
                    break
                    ;;
                *) log_warn "请输入 1 或 2" ;;
            esac
        done
    else
        CERT_MODE="file"
    fi

    if [[ "$CERT_MODE" == "file" ]]; then
        echo -e "  ${GREEN}证书: ${CERT_FILE_PATH}${NC}"
        echo -e "  ${GREEN}私钥: ${KEY_FILE_PATH}${NC}"
        echo ""
        echo -e "  ${YELLOW}💡 推荐使用 acme.sh 自动续期：${NC}"
        echo -e "  ${YELLOW}   curl https://get.acme.sh | sh${NC}"
        echo -e "  ${YELLOW}   acme.sh --issue -d ${DOMAIN_NAME} --nginx${NC}"
        echo -e "  ${YELLOW}   acme.sh --install-cert -d ${DOMAIN_NAME} \\${NC}"
        echo -e "  ${YELLOW}     --key-file   /etc/ssl/${DOMAIN_NAME}.key  \\${NC}"
        echo -e "  ${YELLOW}     --fullchain-file /etc/ssl/${DOMAIN_NAME}.crt${NC}"
    fi
    # ---- AnyTLS Fallback（抗主动探测） ----
    # The fallback server receives PLAINTEXT HTTP (TLS is already terminated by
    # xray-core before the anytls auth layer). This means nginx/caddy only needs
    # to serve HTTP on localhost — no certificates, no TLS configuration.
    echo ""
    echo -e "  ${BOLD}━━━ 抗主动探测（Fallback）━━━${NC}"
    echo -e "  ${YELLOW}GFW 会伪装成浏览器探测可疑端口。配置 Fallback 后，${NC}"
    echo -e "  ${YELLOW}非法连接被转发到本地 Web 服务器返回正常网页，${NC}"
    echo -e "  ${YELLOW}使 AnyTLS 端口行为与正常 HTTPS 网站完全不可区分。${NC}"
    echo -e "  ${YELLOW}（Fallback 走明文 HTTP——TLS 已被 xray-core 终结，无需证书）${NC}"
    echo ""
    echo -e "  ${BOLD}是否配置 Fallback？${NC}"
    echo -e "  ${GREEN}[Y]${NC} 自动安装 nginx + 配置  ${YELLOW}[N]${NC} 跳过（有指纹风险）"
    read -r -n 1 -p "  → " use_fallback
    echo ""
    ANYTLS_FALLBACK=""
    FALLBACK_PORT="8443"
    if [[ "$use_fallback" = "Y" || "$use_fallback" = "y" ]]; then
        ANYTLS_FALLBACK="127.0.0.1:${FALLBACK_PORT}"

        # Check if nginx is already installed
        NGINX_INSTALLED=false
        if command -v nginx &>/dev/null; then
            NGINX_INSTALLED=true
            log_info "检测到 nginx 已安装"
        fi

        # Check if already listening on fallback port
        if $NGINX_INSTALLED && ss -tlnp 2>/dev/null | grep -q "127.0.0.1:${FALLBACK_PORT}"; then
            echo -e "  ${CYAN}检测到 127.0.0.1:${FALLBACK_PORT} 已有服务在监听${NC}"
            log_info "将复用现有服务作为 Fallback"
        else
            echo ""
            echo -e "  ${CYAN}正在安装并配置 nginx 作为 Fallback 服务器...${NC}"

            # Install nginx if needed
            if ! $NGINX_INSTALLED; then
                if command -v apt &>/dev/null; then
                    apt update -qq && apt install -y -qq nginx 2>&1 | tail -1
                elif command -v yum &>/dev/null; then
                    yum install -y -q nginx 2>&1 | tail -1
                else
                    log_warn "无法自动安装 nginx，请手动安装后配置"
                fi
            fi

            # Create fallback web root with a realistic static page
            mkdir -p /var/www/fallback
            cat > /var/www/fallback/index.html << 'FALLBACK_HTML'
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Welcome</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
               max-width: 720px; margin: 80px auto; padding: 0 20px; color: #333; }
        h1 { font-weight: 400; border-bottom: 1px solid #eee; padding-bottom: 12px; }
        p { line-height: 1.6; color: #666; }
    </style>
</head>
<body>
    <h1>Welcome</h1>
    <p>This site is under construction. Please check back later.</p>
</body>
</html>
FALLBACK_HTML

            # Nginx config — listen on localhost only, plain HTTP
            cat > /etc/nginx/sites-available/fallback << NGINX_CONF
server {
    listen 127.0.0.1:${FALLBACK_PORT};
    root /var/www/fallback;
    index index.html;
    server_name _;
    access_log off;
    error_log /var/log/nginx/fallback-error.log;
}
NGINX_CONF

            # Enable the site (handle both Debian/Ubuntu and other layouts)
            if [[ -d /etc/nginx/sites-enabled ]]; then
                ln -sf /etc/nginx/sites-available/fallback /etc/nginx/sites-enabled/fallback
                # Remove default site to avoid port conflict
                rm -f /etc/nginx/sites-enabled/default
            else
                # RHEL/CentOS style — include directly in nginx.conf
                if ! grep -q "include /etc/nginx/sites-available" /etc/nginx/nginx.conf 2>/dev/null; then
                    sed -i '/http {/a\    include /etc/nginx/sites-available/*;' /etc/nginx/nginx.conf
                fi
            fi

            # Test and reload
            if nginx -t 2>&1 | tail -1; then
                systemctl enable nginx 2>/dev/null || true
                systemctl restart nginx 2>/dev/null || nginx -s reload 2>/dev/null || true
                log_info "nginx Fallback 已配置: http://${ANYTLS_FALLBACK}"
            else
                log_warn "nginx 配置测试失败，请手动检查"
            fi
        fi

        log_info "Fallback 已配置: ${ANYTLS_FALLBACK}"
    else
        echo -e "  ${YELLOW}⚠ Fallback 未配置。非法连接将被直接断开（可被指纹识别）${NC}"
    fi

    echo ""
    pause 2
fi

# REALITY 不需要域名/证书
if $INSTALL_VLESS; then
    echo ""
    echo -e "  ${GREEN}VLESS + REALITY: 无需域名，无需证书${NC}"
fi
pause 1

# ---------------------------------------------------------------------------
# 确认配置
# ---------------------------------------------------------------------------
log_step "第三步：确认配置"

echo ""
echo -e "  ${BOLD}请确认以下配置信息：${NC}"
echo ""
echo -e "  ┌─────────────────────────────────────────────┐"
echo -e "  │  面板地址:   ${GREEN}${V2BOARD_HOST}${NC}"
echo -e "  │  API Token:  ${GREEN}${V2BOARD_KEY:0:8}************************${NC}"
echo -e "  │  安装模式:   ${GREEN}${INSTALL_MODE}${NC}"
if $INSTALL_VLESS; then
echo -e "  │  🟢 VLESS 节点:  面板 NodeID ${NODE_ID}${NC}"
fi
if $INSTALL_ANYTLS; then
echo -e "  │  🔵 AnyTLS 节点:  面板 NodeID ${ANYTLS_NODE_ID}${NC}"
echo -e "  │     域名:     ${GREEN}${DOMAIN_NAME}${NC}"
if [[ "$CERT_MODE" == "http" ]]; then
echo -e "  │     证书:     ${GREEN}HTTP 自动申请 (${CERT_EMAIL})${NC}"
else
echo -e "  │     证书:     ${GREEN}${CERT_FILE_PATH}${NC}"
fi
fi
echo -e "  └─────────────────────────────────────────────┘"
echo ""

while true; do
    echo -e "  ${BOLD}以上信息是否正确？${NC}"
    echo -e "  ${GREEN}[Y]${NC} 确认安装   ${RED}[N]${NC} 取消   ${YELLOW}[R]${NC} 重新输入"
    read -r -n 1 -p "  → " confirm
    echo ""

    case "$confirm" in
        Y|y) break ;;
        N|n) echo ""; log_info "已取消安装"; exit 0 ;;
        R|r)
            echo ""
            echo -e "  ${YELLOW}需要重新输入哪个？${NC}"
            echo -e "  [1] 面板地址  [2] API Token  [3] 节点 ID  [4] 协议选择"
            read -r -n 1 -p "  → " redo
            echo ""
            case "$redo" in
                1)
                    read -r -p "  新面板地址 → " V2BOARD_HOST
                    echo -e "  ${GREEN}已更新: ${V2BOARD_HOST}${NC}"
                    ;;
                2)
                    read -r -p "  新 API Token → " V2BOARD_KEY
                    echo -e "  ${GREEN}已更新: ${V2BOARD_KEY:0:8}****${NC}"
                    ;;
                3)
                    while true; do
                        read -r -p "  新节点 ID → " NODE_ID
                        if [[ "$NODE_ID" =~ ^[0-9]+$ ]]; then break; fi
                        log_warn "节点 ID 必须是纯数字"
                    done
                    echo -e "  ${GREEN}已更新: ${NODE_ID}${NC}"
                    ;;
                4)
                    echo -e "  [1] VLESS+REALITY  [2] AnyTLS+TLS  [3] VLESS+AnyTLS 双协议"
                    read -r -n 1 -p "  → " mode
                    echo ""
                    case "$mode" in
                        1) INSTALL_MODE="vless"; INSTALL_VLESS=true; INSTALL_ANYTLS=false ;;
                        2) INSTALL_MODE="anytls"; INSTALL_VLESS=false; INSTALL_ANYTLS=true ;;
                        3) INSTALL_MODE="dual"; INSTALL_VLESS=true; INSTALL_ANYTLS=true ;;
                    esac
                    echo -e "  ${GREEN}已更新: ${INSTALL_MODE}${NC}"
                    ;;
            esac
            ;;
        *) log_warn "请输入 Y / N / R" ;;
    esac
done

# ============================================================================
# 1. 系统环境检查
# ============================================================================
log_step "第四步：检查系统环境"
pause 1

if [[ $EUID -ne 0 ]]; then
    log_error "请使用 root 用户运行此脚本（sudo bash install.sh）"
fi

# 检查系统
if [ -f /etc/os-release ]; then
    . /etc/os-release
    log_info "当前系统: ${NAME} ${VERSION_ID}"
else
    log_warn "无法识别系统版本，继续安装..."
fi

# 检查架构
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)  PLATFORM="amd64" ;;
    aarch64) PLATFORM="arm64" ;;
    *)       log_error "不支持的架构: $ARCH（需要 amd64 或 arm64）" ;;
esac
log_info "系统架构: ${ARCH} → ${PLATFORM}"

# 检查必要命令
for cmd in curl tar systemctl; do
    if ! command -v "$cmd" &>/dev/null; then
        log_error "缺少必要命令: $cmd，请先安装"
    fi
done
log_info "必要命令检查通过"

# 检查磁盘空间
available_space=$(df -m /usr | awk 'NR==2 {print $4}')
if [[ "$available_space" -lt 200 ]]; then
    log_warn "磁盘可用空间不足 200MB（当前: ${available_space}MB），安装可能失败"
else
    log_info "磁盘可用空间: ${available_space}MB"
fi
pause 2

# ============================================================================
# 2. 创建目录结构
# ============================================================================
log_step "第五步：创建目录结构"
pause 1

mkdir -p "$XRAYR_INSTALL_DIR"/{cert/certificates,log,config}
log_info "安装目录: ${XRAYR_INSTALL_DIR}"
log_info "  证书目录: ${XRAYR_INSTALL_DIR}/cert/certificates"
log_info "  日志目录: ${XRAYR_INSTALL_DIR}/log"
log_info "  配置目录: ${XRAYR_INSTALL_DIR}/config"
pause 1

# ============================================================================
# 3. 下载并安装 XrayR 二进制
# ============================================================================
log_step "第六步：安装 XrayR 核心程序"
pause 1

install_xrayr() {
    if [[ -f "./XrayR" ]]; then
        log_info "检测到本地 XrayR 二进制文件，直接安装..."
        cp ./XrayR "$XRAYR_BIN_DIR/$XRAYR_BIN_NAME"
        chmod +x "$XRAYR_BIN_DIR/$XRAYR_BIN_NAME"
        return 0
    fi

    # 从 GitHub Releases 下载对应架构二进制
    local asset="XrayR-linux-${PLATFORM}"
    local url="https://github.com/${XRAYR_REPO}/releases/download/${XRAYR_VERSION}/${asset}"
    local tmp="/tmp/${asset}"

    log_info "未找到本地二进制，尝试从 GitHub Releases 下载..."
    log_info "版本: ${XRAYR_VERSION} | 架构: ${PLATFORM}"
    log_info "URL: ${url}"

    if curl -fL --connect-timeout 15 --max-time 300 -o "$tmp" "$url"; then
        chmod +x "$tmp"
        cp "$tmp" "$XRAYR_BIN_DIR/$XRAYR_BIN_NAME"
        chmod +x "$XRAYR_BIN_DIR/$XRAYR_BIN_NAME"
        rm -f "$tmp"
        log_info "已从 GitHub Releases 安装 XrayR ${XRAYR_VERSION}"
        return 0
    fi

    rm -f "$tmp" 2>/dev/null || true

    # 下载失败 — 提示本地编译 + scp
    echo ""
    echo -e "  ${RED}无法从 GitHub 下载 XrayR 二进制文件${NC}"
    echo ""
    echo -e "  ${BOLD}请在本地机器编译并 scp 到 VPS：${NC}"
    echo ""
    echo -e "  ${CYAN}# 本地机器（macOS / Linux 开发机）${NC}"
    echo -e "  ${GREEN}git clone https://github.com/${XRAYR_REPO}.git && cd XrayR4u${NC}"
    echo -e "  ${GREEN}CGO_ENABLED=0 GOOS=linux GOARCH=${PLATFORM} \\${NC}"
    echo -e "  ${GREEN}  go build -v -o XrayR -trimpath \\${NC}"
    echo -e "  ${GREEN}    -ldflags \"-s -w -buildid=\" ./main${NC}"
    echo ""
    echo -e "  ${CYAN}# 上传到 VPS${NC}"
    echo -e "  ${GREEN}scp XrayR root@<VPS_IP>:/root/${NC}"
    echo ""
    echo -e "  ${YELLOW}上传完成后重新运行本脚本即可${NC}"
    return 1
}


if ! install_xrayr; then
    log_error "无法获取 XrayR 二进制文件，安装中止"
fi

# 验证二进制
local_size=$(stat -c%s "$XRAYR_BIN_DIR/$XRAYR_BIN_NAME" 2>/dev/null || stat -f%z "$XRAYR_BIN_DIR/$XRAYR_BIN_NAME" 2>/dev/null)
local_size_mb=$(( local_size / 1024 / 1024 ))
if [[ "$local_size" -lt 1000000 ]]; then
    log_warn "XrayR 二进制文件偏小（${local_size_mb}MB），可能不完整"
else
    log_info "XrayR 二进制已安装: ${XRAYR_BIN_DIR}/${XRAYR_BIN_NAME} (${local_size_mb}MB)"
fi
pause 2

# ============================================================================
# 4. 下载 Geo 数据文件
# ============================================================================
log_step "第七步：下载 Geo 数据文件"
pause 1

download_geo() {
    local name=$1 url=$2
    if [[ -f "$XRAYR_INSTALL_DIR/$name" ]]; then
        log_info "$name — 已存在，跳过"
    else
        echo -ne "  ${CYAN}下载 ${name} ...${NC}"
        if curl -sSL -o "$XRAYR_INSTALL_DIR/$name" "$url" --connect-timeout 15 --max-time 60; then
            echo -e " ${GREEN}完成${NC}"
        else
            echo -e " ${YELLOW}失败（创建空占位文件）${NC}"
            touch "$XRAYR_INSTALL_DIR/$name"
        fi
    fi
}

download_geo "geoip.dat"   "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat"
download_geo "geosite.dat" "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat"
pause 2

# ============================================================================
# 5. 生成配置文件
# ============================================================================
log_step "第八步：生成配置文件"
pause 1

# 主配置 — 根据安装模式动态生成 Nodes 列表
cat > "$XRAYR_INSTALL_DIR/config.yml" << YML
# ============================================================================
# XrayR 配置文件 — 安装模式: ${INSTALL_MODE}
# 内置 xray-core v1.260327.0
# ============================================================================
Log:
  Level: warning
  AccessPath: $XRAYR_INSTALL_DIR/log/access.log
  ErrorPath: $XRAYR_INSTALL_DIR/log/error.log

DnsConfigPath: $XRAYR_INSTALL_DIR/config/dns.json
RouteConfigPath: $XRAYR_INSTALL_DIR/config/route.json
InboundConfigPath:
OutboundConfigPath: $XRAYR_INSTALL_DIR/config/custom_outbound.json

ConnetionConfig:
  Handshake: 4
  ConnIdle: 30
  UplinkOnly: 2
  DownlinkOnly: 4
  BufferSize: 64

Nodes:
YML

# --- VLESS 节点 ---
if $INSTALL_VLESS; then
    cat >> "$XRAYR_INSTALL_DIR/config.yml" << YML
  - PanelType: "V2board"
    ApiConfig:
      ApiHost: "$V2BOARD_HOST"
      ApiKey: "$V2BOARD_KEY"
      NodeID: $NODE_ID
      NodeType: Vless
      Timeout: 30
      EnableReality: true
      SpeedLimit: 0
      DeviceLimit: 0
      DisableCustomConfig: false
      RuleListPath:
    ControllerConfig:
      ListenIP: 0.0.0.0
      SendIP: 0.0.0.0
      UpdatePeriodic: 60
      EnableDNS: false
      DNSType: AsIs
      EnableProxyProtocol: false
      EnableFallback: false
      DisableSniffing: false
      DisableUploadTraffic: false
      DisableGetRule: false
      DisableIVCheck: false
      CertConfig:
        CertMode: none
YML
    log_info "VLESS 节点配置已添加"
fi

# --- AnyTLS 节点 ---
if $INSTALL_ANYTLS; then
    cat >> "$XRAYR_INSTALL_DIR/config.yml" << YML
  - PanelType: "V2board"
    ApiConfig:
      ApiHost: "$V2BOARD_HOST"
      ApiKey: "$V2BOARD_KEY"
      NodeID: $ANYTLS_NODE_ID
      NodeType: anytls
      Timeout: 30
      SpeedLimit: 0
      DeviceLimit: 0
      DisableCustomConfig: false
      RuleListPath:
    ControllerConfig:
      ListenIP: 0.0.0.0
      SendIP: 0.0.0.0
      UpdatePeriodic: 60
      EnableDNS: false
      DNSType: AsIs
      EnableProxyProtocol: false
      EnableFallback: false
      DisableSniffing: false
      DisableUploadTraffic: false
      DisableGetRule: false
      DisableIVCheck: false
      $(if [[ -n "$ANYTLS_FALLBACK" ]]; then echo "AnyTLSFallback: \"$ANYTLS_FALLBACK\""; fi)
      CertConfig:
        CertMode: $CERT_MODE
        CertDomain: "$DOMAIN_NAME"
        $(if [[ "$CERT_MODE" == "file" ]]; then
            echo "CertFile: \"$CERT_FILE_PATH\""
            echo "        KeyFile: \"$KEY_FILE_PATH\""
        elif [[ "$CERT_MODE" == "http" ]]; then
            echo "Email: \"$CERT_EMAIL\""
        fi)
YML
    if [[ -n "$ANYTLS_FALLBACK" ]]; then
        log_info "AnyTLS 节点配置已添加 (${CERT_MODE} 证书, Fallback: ${ANYTLS_FALLBACK})"
    else
        log_info "AnyTLS 节点配置已添加 (${CERT_MODE} 证书, 无 Fallback)"
    fi
fi

log_info "主配置: ${XRAYR_INSTALL_DIR}/config.yml"

# DNS 配置
cat > "$XRAYR_INSTALL_DIR/config/dns.json" << 'JSON'
{
    "servers": [
        {
            "address": "https://1.1.1.1/dns-query",
            "domains": []
        },
        {
            "address": "https://8.8.8.8/dns-query",
            "domains": []
        },
        "localhost"
    ],
    "tag": "dns"
}
JSON
log_info "DNS 配置: ${XRAYR_INSTALL_DIR}/config/dns.json"

# 路由配置
cat > "$XRAYR_INSTALL_DIR/config/route.json" << 'JSON'
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
JSON
log_info "路由配置: ${XRAYR_INSTALL_DIR}/config/route.json"

# 出站配置
cat > "$XRAYR_INSTALL_DIR/config/custom_outbound.json" << 'JSON'
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
JSON
log_info "出站配置: ${XRAYR_INSTALL_DIR}/config/custom_outbound.json"

# 审计规则
cat > "$XRAYR_INSTALL_DIR/rulelist" << 'TXT'
# 本地审计规则 — 每行一个正则，匹配的域名/地址将被拒绝
# .*\.torrent\..*
# .*\.example\.blocked
TXT
pause 1

# ============================================================================
# 6. 设置文件权限
# ============================================================================
log_info "设置文件权限..."
chown -R root:root "$XRAYR_INSTALL_DIR"
chmod 755 "$XRAYR_INSTALL_DIR" "$XRAYR_INSTALL_DIR"/log "$XRAYR_INSTALL_DIR"/cert "$XRAYR_INSTALL_DIR"/cert/certificates "$XRAYR_INSTALL_DIR"/config 2>/dev/null || true
chmod 644 "$XRAYR_INSTALL_DIR/config.yml"
pause 1

# ============================================================================
# 7. 安装 systemd 服务
# ============================================================================
log_step "第九步：安装 systemd 服务"
pause 1

cat > /etc/systemd/system/xrayr.service << UNIT
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

ExecStart=/usr/local/bin/XrayR --config /etc/XrayR/config.yml
ExecStop=/bin/kill -s SIGINT \$MAINPID

Restart=on-failure
RestartSec=5s

LimitNOFILE=1048576
LimitNPROC=512

StandardOutput=journal
StandardError=journal
SyslogIdentifier=xrayr

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
log_info "systemd 服务已安装: /etc/systemd/system/xrayr.service"
pause 2

# ============================================================================
# 8. 系统内核优化
# ============================================================================
log_step "第十步：系统内核优化"
pause 1

if ! grep -q "tcp_congestion_control = bbr" /etc/sysctl.conf 2>/dev/null; then
    cat >> /etc/sysctl.conf << 'SYSCTL'

# === XrayR 优化参数 ===
net.core.default_qdisc = fq
net.ipv4.tcp_congestion_control = bbr
net.ipv4.tcp_rmem = 4096 87380 16777216
net.ipv4.tcp_wmem = 4096 65536 16777216
net.ipv4.tcp_slow_start_after_idle = 0
net.core.netdev_max_backlog = 16384
net.ipv4.tcp_fastopen = 3

# 文件描述符
fs.file-max = 1048576

# UDP 优化
net.core.rmem_default = 262144
net.core.wmem_default = 262144
net.core.rmem_max = 67108864
net.core.wmem_max = 67108864
SYSCTL
    sysctl -p >/dev/null 2>&1
    log_info "BBR 拥塞控制 + 内核参数已优化"
else
    log_info "内核优化参数已存在，跳过"
fi

# 文件描述符限制
if ! grep -q "root soft" /etc/security/limits.conf 2>/dev/null; then
    cat >> /etc/security/limits.conf << 'LIMITS'

# XrayR (runs as root)
root soft nofile 1048576
root hard nofile 1048576
root soft nproc 512
root hard nproc 512
LIMITS
    log_info "文件描述符限制已设置"
fi
pause 2

# ============================================================================
# 9. 防火墙配置
# ============================================================================
log_step "第十一步：防火墙配置"
pause 1

# 确定需要放行的端口
FIREWALL_PORTS=(443)              # VLESS REALITY / AnyTLS TLS
if $INSTALL_VLESS || [[ "$CERT_MODE" == "http" ]]; then
    FIREWALL_PORTS+=(80)          # REALITY fallback / Let's Encrypt HTTP
fi
# 去重
FIREWALL_PORTS=($(printf '%s\n' "${FIREWALL_PORTS[@]}" | sort -u))

log_info "需要放行的端口: ${FIREWALL_PORTS[*]}"

if command -v ufw &>/dev/null && ufw status 2>/dev/null | grep -q "active"; then
    log_info "检测到 UFW，添加规则..."
    for port in "${FIREWALL_PORTS[@]}"; do
        ufw allow $port/tcp comment "XrayR port $port" 2>/dev/null || true
    done
    ufw reload 2>/dev/null || true
    log_info "UFW 规则已添加"

elif command -v firewall-cmd &>/dev/null && firewall-cmd --state 2>/dev/null | grep -q "running"; then
    log_info "检测到 firewalld，添加规则..."
    for port in "${FIREWALL_PORTS[@]}"; do
        firewall-cmd --permanent --add-port=$port/tcp 2>/dev/null || true
    done
    firewall-cmd --reload 2>/dev/null || true
    log_info "firewalld 规则已添加"

elif command -v nft &>/dev/null && nft list ruleset 2>/dev/null | grep -q "input"; then
    log_info "检测到 nftables"
    echo -e "  ${YELLOW}请手动执行: nft add rule inet filter input tcp dport {443,80} accept${NC}"

elif command -v iptables &>/dev/null; then
    log_info "使用 iptables 添加规则..."
    for port in "${FIREWALL_PORTS[@]}"; do
        iptables -C INPUT -p tcp --dport $port -j ACCEPT 2>/dev/null || \
            iptables -A INPUT -p tcp --dport $port -j ACCEPT
    done

    # 持久化
    if command -v netfilter-persistent &>/dev/null; then
        netfilter-persistent save 2>/dev/null || true
    elif command -v iptables-save &>/dev/null; then
        mkdir -p /etc/iptables
        iptables-save > /etc/iptables/rules.v4 2>/dev/null || true
    fi
    log_info "iptables 规则已添加"

else
    log_warn "未检测到系统防火墙"
    echo ""
    echo -e "  ${RED}⚠️  请务必检查云控制台防火墙，放行 TCP 443 端口：${NC}"
    echo -e "  ${YELLOW}  Vultr:   Products → Firewall → Add Rule (TCP 443)${NC}"
    echo -e "  ${YELLOW}  AWS:     EC2 → Security Groups → Inbound (TCP 443)${NC}"
    echo -e "  ${YELLOW}  阿里云:  ECS → 安全组 → 入方向 (TCP 443)${NC}"
    echo -e "  ${YELLOW}  腾讯云:  CVM → 安全组 → 入站规则 (TCP 443)${NC}"
fi
pause 2

# ============================================================================
# 10. 启动服务
# ============================================================================
log_step "第十二步：启动 XrayR 服务"
pause 1

echo -e "  ${CYAN}正在启用并启动 xrayr 服务...${NC}"
systemctl enable xrayr
systemctl restart xrayr

echo -e "  ${CYAN}等待服务启动...${NC}"
sleep 3

if systemctl is-active --quiet xrayr; then
    log_info "✅ XrayR 服务已成功启动！"
else
    echo ""
    log_warn "❌ XrayR 启动失败，请运行以下命令排查:"
    echo -e "     ${YELLOW}journalctl -u xrayr -n 50${NC}"
fi
pause 2

# ============================================================================
# 11. 安装完成
# ============================================================================
clear
echo ""
echo -e "${GREEN}╔═══════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║                                                   ║${NC}"
echo -e "${GREEN}║        ✅  XrayR 部署完成！                        ║${NC}"
echo -e "${GREEN}║                                                   ║${NC}"
echo -e "${GREEN}╚═══════════════════════════════════════════════════╝${NC}"
echo ""
pause 1

echo -e "  ${BOLD}📋 部署摘要${NC}"
echo -e "  ┌─────────────────────────────────────────────┐"
echo -e "  │  面板地址:   ${GREEN}${V2BOARD_HOST}${NC}"
echo -e "  │  安装模式:   ${GREEN}${INSTALL_MODE}${NC}"
if $INSTALL_VLESS; then
echo -e "  │  🟢 VLESS:    面板 NodeID ${NODE_ID}, REALITY:443${NC}"
fi
if $INSTALL_ANYTLS; then
echo -e "  │  🔵 AnyTLS:   NodeID ${ANYTLS_NODE_ID}, 域名 ${GREEN}${DOMAIN_NAME}${NC}"
if [[ "$CERT_MODE" == "http" ]]; then
echo -e "  │     证书:     ${GREEN}HTTP 自动申请 (${CERT_EMAIL})${NC}"
else
echo -e "  │     证书:     ${GREEN}${CERT_FILE_PATH}${NC}"
fi
if [[ -n "$ANYTLS_FALLBACK" ]]; then
echo -e "  │     Fallback: ${GREEN}${ANYTLS_FALLBACK}${NC}"
else
echo -e "  │     Fallback: ${RED}未配置（有指纹风险）${NC}"
fi
fi
echo -e "  │  配置文件:   ${YELLOW}${XRAYR_INSTALL_DIR}/config.yml${NC}"
echo -e "  │  日志目录:   ${YELLOW}${XRAYR_INSTALL_DIR}/log/${NC}"
echo -e "  └─────────────────────────────────────────────┘"
echo ""
pause 1

echo -e "  ${BOLD}🔧 常用管理命令${NC}"
echo ""
echo -e "  ${GREEN}systemctl status xrayr${NC}      查看服务状态"
echo -e "  ${GREEN}journalctl -u xrayr -f${NC}      查看实时日志"
echo -e "  ${GREEN}ss -tlnp | grep XrayR${NC}       查看监听端口"
echo -e "  ${GREEN}systemctl restart xrayr${NC}     重启服务"
echo -e "  ${GREEN}vi ${XRAYR_INSTALL_DIR}/config.yml${NC}   编辑配置"
echo ""
pause 2

if $INSTALL_ANYTLS; then
echo -e "  ${YELLOW}💡 TLS 证书使用 acme.sh 等工具维护，脚本不管理续期${NC}"
echo -e "  ${YELLOW}   安装 acme.sh: curl https://get.acme.sh | sh${NC}"
if [[ "$CERT_MODE" == "file" ]]; then
echo -e "  ${YELLOW}   证书路径: ${CERT_FILE_PATH} / ${KEY_FILE_PATH}${NC}"
fi
if [[ -z "$ANYTLS_FALLBACK" ]]; then
echo ""
echo -e "  ${RED}⚠ 强烈建议配置 Fallback！${NC}"
echo -e "  ${YELLOW}   安装 nginx 并配置反向代理作为伪装站点：${NC}"
echo -e "  ${YELLOW}   apt install nginx -y${NC}"
echo -e "  ${YELLOW}   # nginx 监听 127.0.0.1:8443，返回正常网页${NC}"
echo -e "  ${YELLOW}   然后在 config.yml 中设置 AnyTLSFallback: \"127.0.0.1:8443\"${NC}"
fi
echo ""
fi

echo -e "  ${YELLOW}💡 端口由 V2Board 面板控制，XrayR 自动获取，无需手动配置${NC}"
echo -e "  在面板后台配置节点的监听端口后，XrayR 会自动拉取并监听"
echo ""
pause 1

echo -e "  ${RED}🔴 如果端口不通，请按顺序检查：${NC}"
echo -e "  1. 云控制台防火墙 (Vultr/AWS/阿里云 安全组) ← ${BOLD}最常见原因${NC}"
echo -e "  2. 系统 iptables/ufw:  ${GREEN}iptables -L -n${NC}"
echo -e "  3. XrayR 监听状态:     ${GREEN}ss -tlnp | grep XrayR${NC}"
echo -e "  4. 面板端口配置:       面板 → 节点 → port 字段"
if $INSTALL_ANYTLS; then
echo -e "  5. TLS 证书:           ${GREEN}journalctl -u xrayr | grep -i cert${NC}"
fi
echo ""
pause 1

echo -e "  ${YELLOW}📖 更多文档: https://github.com/HenZenKuriRIP/XrayR4u${NC}"
echo ""
