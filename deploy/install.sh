#!/usr/bin/env bash
# ============================================================================
# XrayR v0.9.19 VPS 一键部署脚本
# 支持: Debian 12/13, Ubuntu 20.04+/22.04+/24.04+
# 源码与本版本 Release 均内嵌 xray-core v26.11.9
# 协议: VLESS + REALITY / TLS+XHTTP+CDN / AnyTLS + TLS / Hysteria2 + TLS
# Hysteria2 见 deploy/HYSTERIA2.md。
# 选完协议后，每条节点单独输入自己的 API Key。
# [1][2][4] 只问该协议。[3] 分别问 VLESS 和 AnyTLS。[5] 分别问 VLESS 和 Hysteria2。
# v0.9.16 的 Release 仍是 xray-core v26.7.11，没有 Hysteria2，不要用那个二进制。
# 密钥生成: XrayR tools {x25519|mldsa65|vlessenc|mlkem768|uuid}
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
XRAYR_VERSION="v0.9.19"
# v0.9.19 的 Release 已含 Hysteria2，并会核对面板 server_name 与 CertDomain。为 false 时，选项 [4] 可以下载该 Release。
# 同目录若已有 ./XrayR，脚本仍优先使用本地文件。
HYSTERIA2_NEEDS_LOCAL_BINARY=false

# BEGIN_TESTABLE_HELPERS
# 纯函数，安装流程和 deploy 回归测试共用。不要在这里访问面板。

classify_address_set() {
    local state="$1"
    local records="$2"
    local local_ips="$3"
    local public_ips="$4"
    local rec hay
    case "$state" in
        nxdomain) printf '%s\n' nxdomain; return 0 ;;
        fail) printf '%s\n' fail; return 0 ;;
    esac
    if [[ -z "$records" ]]; then
        printf '%s\n' empty
        return 0
    fi
    hay=",${local_ips},${public_ips},"
    IFS=',' read -r -a _recs <<< "$records"
    for rec in "${_recs[@]}"; do
        [[ -z "$rec" ]] && continue
        if [[ "$hay" == *",${rec},"* ]]; then
            printf '%s\n' ok
            return 0
        fi
    done
    printf '%s\n' mismatch
}

sni_relation() {
    local panel cert
    panel=$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')
    cert=$(printf '%s' "$2" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')
    if [[ -z "$panel" || -z "$cert" ]]; then
        printf '%s\n' ok
        return 0
    fi
    if [[ "$panel" == "$cert" ]]; then
        printf '%s\n' ok
    else
        printf '%s\n' mismatch
    fi
}

config_has_existing_node() {
    local file="$1"
    [[ -f "$file" ]] || return 1
    grep -E -q 'NodeType:[[:space:]]*(Vless|vless|anytls|Anytls|AnyTLS|hysteria2|hysteria|hy2|Hysteria2|Hysteria|HY2)([[:space:]]|$)' "$file"
}

# 参数: vless端口 anytls端口 hy2端口 需要80 vless开 anytls开 hy2开 伪装开
# 输出一行: tcp=80,8443 udp=443
assemble_firewall_ports() {
    local vless_port="$1"
    local anytls_port="$2"
    local hy2_port="$3"
    local need80="$4"
    local vless_on="$5"
    local anytls_on="$6"
    local hy2_on="$7"
    local masq_on="$8"
    local tcp="" udp="" value list
    _fw_add() {
        list="$1"
        value="$2"
        [[ -z "$value" ]] && { printf '%s' "$list"; return 0; }
        case ",${list}," in
            *",${value},"*) printf '%s' "$list" ;;
            *)
                if [[ -z "$list" ]]; then
                    printf '%s' "$value"
                else
                    printf '%s' "${list},${value}"
                fi
                ;;
        esac
    }
    if [[ "$vless_on" == "1" ]]; then tcp=$(_fw_add "$tcp" "$vless_port"); fi
    if [[ "$anytls_on" == "1" ]]; then tcp=$(_fw_add "$tcp" "$anytls_port"); fi
    if [[ "$need80" == "1" || "$masq_on" == "1" ]]; then tcp=$(_fw_add "$tcp" 80); fi
    if [[ "$masq_on" == "1" ]]; then tcp=$(_fw_add "$tcp" 443); fi
    if [[ "$hy2_on" == "1" ]]; then udp=$(_fw_add "$udp" "$hy2_port"); fi
    printf 'tcp=%s udp=%s\n' "$tcp" "$udp"
}
# END_TESTABLE_HELPERS

# 把逗号分隔的端口放进数组。调用方先声明空数组。
csv_to_array() {
    local csv="$1"
    local dest="$2"
    local item
    eval "${dest}=()"
    [[ -z "$csv" ]] && return 0
    IFS=',' read -r -a _csv_items <<< "$csv"
    for item in "${_csv_items[@]}"; do
        [[ -z "$item" ]] && continue
        eval "${dest}+=(\"\$item\")"
    done
}

local_ip_csv() {
    local raw=""
    if command -v ip >/dev/null 2>&1; then
        raw=$(ip -o addr show 2>/dev/null | awk '{print $4}' | cut -d/ -f1 | sort -u | paste -sd, - || true)
    elif command -v ifconfig >/dev/null 2>&1; then
        raw=$(ifconfig 2>/dev/null | awk '/inet /{print $2} /inet6 /{print $2}' | sort -u | paste -sd, - || true)
    fi
    printf '%s\n' "$raw"
}

public_ip_csv() {
    local ip url
    for url in https://api.ipify.org https://checkip.amazonaws.com https://ifconfig.me/ip; do
        ip=$(curl -fsS --max-time 4 "$url" 2>/dev/null | tr -d '[:space:]' || true)
        if [[ "$ip" =~ ^[0-9A-Fa-f:.]+$ ]]; then
            printf '%s\n' "$ip"
            return 0
        fi
    done
    printf '\n'
}

# 打印问题文本。没有问题时不打印。不退出，由调用方决定。
domain_problem() {
    local domain="$1"
    local records kind local_ips public_ips
    records=$(getent ahosts "$domain" 2>/dev/null | awk '{print $1}' | sort -u | paste -sd, - || true)
    if [[ -z "$records" ]]; then
        printf '%s\n' "域名 ${domain} 无法解析，或没有 A/AAAA 记录。申请证书和启动之前已停止。"
        return 0
    fi
    local_ips=$(local_ip_csv)
    public_ips=$(public_ip_csv)
    kind=$(classify_address_set records "$records" "$local_ips" "$public_ips")
    if [[ "$kind" == "ok" ]]; then
        return 0
    fi
    printf '%s\n' "域名 ${domain} 解析到 ${records}，和本机地址（${local_ips:-无}）以及公网地址（${public_ips:-未取得}）都不一致。申请证书和启动之前已停止。"
}

require_domain_points_here() {
    local problem
    problem=$(domain_problem "$1")
    if [[ -n "$problem" ]]; then
        log_error "$problem"
    fi
}

ask_listen_port() {
    local label="$1"
    local dest="$2"
    local value=""
    while true; do
        echo ""
        echo -e "  ${YELLOW}没有从面板读到 ${label} 的 server_port。${NC}"
        echo -e "  ${BOLD}请输入该节点在面板上的端口（1-65535）。脚本不会改用 443。${NC}"
        read -r -p "  → " value
        if [[ "$value" =~ ^[0-9]+$ ]] && (( value >= 1 && value <= 65535 )); then
            printf -v "$dest" '%s' "$value"
            return 0
        fi
        log_warn "端口必须是 1 到 65535 的整数"
    done
}

# 成功时打印两行: server_port 和 server_name。临时文件含私钥，读完即删。
fetch_uniproxy_lines() {
    local node_id="$1"
    local api_key="$2"
    local tmp code
    tmp=$(mktemp)
    code=$(curl -sS -o "$tmp" -w '%{http_code}' \
        --connect-timeout 10 --max-time 20 \
        -A "XrayR/${XRAYR_VERSION#v}" \
        -H "Authorization: Bearer ${api_key}" \
        -H "X-Api-Key: ${api_key}" \
        "${V2BOARD_HOST%/}/api/v1/server/UniProxy/config?node_id=${node_id}" 2>/dev/null || true)
    if [[ "$code" != "200" ]]; then
        rm -f "$tmp"
        log_warn "节点 ${node_id} 的 /config 没有成功（HTTP ${code:-失败}）。不会改用 443。" >&2
        return 1
    fi
    if ! command -v python3 >/dev/null 2>&1; then
        rm -f "$tmp"
        log_warn "没有 python3，读不了 server_port。不会改用 443。" >&2
        return 1
    fi
    python3 - "$tmp" << 'PY'
import json, sys
path = sys.argv[1]
with open(path, encoding="utf-8") as fh:
    data = json.load(fh)
port = data.get("server_port", "")
sni = ""
ts = data.get("tls_settings") or {}
if isinstance(ts, dict):
    sni = ts.get("server_name") or ""
print(port)
print(sni)
PY
    local rc=$?
    rm -f "$tmp"
    return "$rc"
}

load_one_port() {
    local label="$1"
    local node_id="$2"
    local api_key="$3"
    local port_var="$4"
    local sni_var="${5:-}"
    local out port sni
    if out=$(fetch_uniproxy_lines "$node_id" "$api_key"); then
        port=$(printf '%s\n' "$out" | sed -n '1p' | tr -d '[:space:]')
        sni=$(printf '%s\n' "$out" | sed -n '2p')
        if [[ "$port" =~ ^[0-9]+$ ]] && (( port >= 1 && port <= 65535 )); then
            printf -v "$port_var" '%s' "$port"
            if [[ -n "$sni_var" ]]; then
                printf -v "$sni_var" '%s' "$sni"
            fi
            log_info "${label} 面板端口: ${port}"
            return 0
        fi
        log_warn "${label} 的 server_port 无效。" >&2
    fi
    ask_listen_port "$label" "$port_var"
}

load_panel_ports() {
    VLESS_LISTEN_PORT=""
    ANYTLS_LISTEN_PORT=""
    HY2_LISTEN_PORT=""
    HY2_PANEL_SNI=""
    if $INSTALL_VLESS; then
        load_one_port "VLESS" "$NODE_ID" "$VLESS_API_KEY" VLESS_LISTEN_PORT
    fi
    if $INSTALL_ANYTLS; then
        load_one_port "AnyTLS" "$ANYTLS_NODE_ID" "$ANYTLS_API_KEY" ANYTLS_LISTEN_PORT
    fi
    if $INSTALL_HY2; then
        load_one_port "Hysteria2" "$(hy2_panel_id)" "$HY2_API_KEY" HY2_LISTEN_PORT HY2_PANEL_SNI
        local rel
        rel=$(sni_relation "$HY2_PANEL_SNI" "$DOMAIN_NAME")
        if [[ "$rel" == "mismatch" ]]; then
            log_error "面板 tls_settings.server_name（${HY2_PANEL_SNI}）和证书域名（${DOMAIN_NAME}）不一致。已停止。空的 server_name 不会触发这道检查。"
        fi
        if [[ -z "$HY2_PANEL_SNI" ]]; then
            log_warn "面板 /config 没有 tls_settings.server_name。订阅里的 host 不在这个接口里，结束时会提醒你把 host 写成 ${DOMAIN_NAME}。"
        fi
    fi
}

apply_firewall_ports() {
    local need80=0 masq=0 vless=0 anytls=0 hy2=0 line tcp udp
    if [[ "${CERT_MODE:-}" == "http" ]]; then
        need80=1
    fi
    if [[ "${HY2_CAMOUFLAGE:-false}" == "true" ]]; then
        masq=1
    fi
    $INSTALL_VLESS && vless=1
    $INSTALL_ANYTLS && anytls=1
    $INSTALL_HY2 && hy2=1
    line=$(assemble_firewall_ports \
        "${VLESS_LISTEN_PORT:-}" "${ANYTLS_LISTEN_PORT:-}" "${HY2_LISTEN_PORT:-}" \
        "$need80" "$vless" "$anytls" "$hy2" "$masq")
    tcp=${line#tcp=}
    tcp=${tcp%% udp=*}
    udp=${line#*udp=}
    FIREWALL_PORTS=()
    FIREWALL_UDP_PORTS=()
    csv_to_array "$tcp" FIREWALL_PORTS
    csv_to_array "$udp" FIREWALL_UDP_PORTS
}

print_cloud_firewall_reminder() {
    local tcp_show udp_show
    tcp_show="${FIREWALL_PORTS[*]:-（无）}"
    udp_show="${FIREWALL_UDP_PORTS[*]:-（无）}"
    echo ""
    echo -e "  ${RED}云安全组必须放行和下面相同的端口。脚本只能改本机防火墙。${NC}"
    echo -e "  ${YELLOW}TCP: ${tcp_show}${NC}"
    echo -e "  ${YELLOW}UDP: ${udp_show}${NC}"
    if $INSTALL_HY2; then
        echo -e "  ${YELLOW}TCP 能连通，不能证明 UDP 可用。${NC}"
    fi
    echo -e "  ${YELLOW}Vultr / AWS / 阿里云 / 腾讯云的安全组都要按这一组另开。${NC}"
    echo ""
}

install_hy2_camouflage() {
    local email="$CERT_EMAIL"
    local webroot="/var/www/xrayr-masq"
    local site="/etc/nginx/sites-available/xrayr-masq.conf"
    if [[ "${HY2_CAMOUFLAGE:-false}" != "true" ]]; then
        return 0
    fi
    if $INSTALL_VLESS && [[ "$VLESS_LISTEN_PORT" == "443" ]]; then
        log_error "伪装站要占用 TCP 443，VLESS 的面板端口也是 443。请把 VLESS 改成其他 TCP 端口后再装。"
    fi
    require_domain_points_here "$DOMAIN_NAME"
    log_step "安装 Hysteria2 伪装站"
    if command -v apt-get >/dev/null 2>&1; then
        apt-get update
        DEBIAN_FRONTEND=noninteractive apt-get install -y nginx certbot
    elif command -v dnf >/dev/null 2>&1; then
        dnf install -y nginx certbot
    else
        log_error "找不到 apt-get 或 dnf，无法安装 nginx 和 certbot。"
    fi
    mkdir -p "$webroot" /etc/letsencrypt/renewal-hooks/deploy
    cat > "${webroot}/index.html" << 'HTML'
<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>Welcome</title></head>
<body><h1>Welcome</h1><p>This site is under construction.</p></body>
</html>
HTML
    # 先只听 80，让 certbot webroot 通过。不要另写 listen 127.0.0.1:80。
    cat > "$site" << NGINX
server {
    listen 80;
    listen [::]:80;
    server_name ${DOMAIN_NAME};
    root ${webroot};
    location ^~ /.well-known/acme-challenge/ {
        default_type "text/plain";
        root ${webroot};
    }
    location / {
        try_files /index.html =404;
    }
}
NGINX
    if [[ -d /etc/nginx/sites-enabled ]]; then
        ln -sf "$site" /etc/nginx/sites-enabled/xrayr-masq.conf
        rm -f /etc/nginx/sites-enabled/default
    elif ! grep -q "sites-available/xrayr-masq.conf" /etc/nginx/nginx.conf 2>/dev/null; then
        mkdir -p /etc/nginx/conf.d
        cp "$site" /etc/nginx/conf.d/xrayr-masq.conf
    fi
    nginx -t
    systemctl enable nginx
    systemctl restart nginx
    certbot certonly --webroot -w "$webroot" -d "$DOMAIN_NAME" \
        --agree-tos --non-interactive -m "$email" --keep-until-expiring
    install -d -m 755 "${XRAYR_INSTALL_DIR}/cert"
    cp "/etc/letsencrypt/live/${DOMAIN_NAME}/fullchain.pem" "${XRAYR_INSTALL_DIR}/cert/${DOMAIN_NAME}.crt"
    cp "/etc/letsencrypt/live/${DOMAIN_NAME}/privkey.pem" "${XRAYR_INSTALL_DIR}/cert/${DOMAIN_NAME}.key"
    chmod 600 "${XRAYR_INSTALL_DIR}/cert/${DOMAIN_NAME}.crt" "${XRAYR_INSTALL_DIR}/cert/${DOMAIN_NAME}.key"
    CERT_MODE="file"
    CERT_FILE_PATH="${XRAYR_INSTALL_DIR}/cert/${DOMAIN_NAME}.crt"
    KEY_FILE_PATH="${XRAYR_INSTALL_DIR}/cert/${DOMAIN_NAME}.key"
    cat > "/etc/letsencrypt/renewal-hooks/deploy/xrayr-${DOMAIN_NAME}.sh" << HOOK
#!/bin/bash
set -euo pipefail
lineage="\${RENEWED_LINEAGE:-/etc/letsencrypt/live/${DOMAIN_NAME}}"
domain=\$(basename "\$lineage")
install -d -m 755 ${XRAYR_INSTALL_DIR}/cert
cp "\$lineage/fullchain.pem" "${XRAYR_INSTALL_DIR}/cert/\${domain}.crt"
cp "\$lineage/privkey.pem" "${XRAYR_INSTALL_DIR}/cert/\${domain}.key"
chmod 600 "${XRAYR_INSTALL_DIR}/cert/\${domain}.crt" "${XRAYR_INSTALL_DIR}/cert/\${domain}.key"
systemctl try-restart xrayr || true
HOOK
    chmod 700 "/etc/letsencrypt/renewal-hooks/deploy/xrayr-${DOMAIN_NAME}.sh"
    # 证书到手后再换成：公网 80 返回 301，本机 127.0.0.1 仍走同一个 listen 80。
    cat > "$site" << NGINX
server {
    listen 443 ssl;
    listen [::]:443 ssl;
    server_name ${DOMAIN_NAME};
    ssl_certificate /etc/letsencrypt/live/${DOMAIN_NAME}/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/${DOMAIN_NAME}/privkey.pem;
    root ${webroot};
    index index.html;
}
server {
    listen 80;
    listen [::]:80;
    server_name ${DOMAIN_NAME};
    root ${webroot};

    location ^~ /.well-known/acme-challenge/ {
        default_type "text/plain";
        root ${webroot};
    }
    location / {
        error_page 418 = @local_masq;
        if (\$remote_addr = 127.0.0.1) { return 418; }
        if (\$remote_addr = ::1) { return 418; }
        return 301 https://\$host\$request_uri;
    }
    location @local_masq {
        root ${webroot};
        rewrite ^ /index.html break;
    }
}
NGINX
    if [[ -d /etc/nginx/conf.d ]] && [[ ! -d /etc/nginx/sites-enabled ]]; then
        cp "$site" /etc/nginx/conf.d/xrayr-masq.conf
    fi
    nginx -t
    systemctl reload nginx
    log_info "伪装站已就绪。公网 TCP 80 返回 301，本机 127.0.0.1:80 返回页面。Hysteria2 反代到这个地址。"
}

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
echo -e "${CYAN}║        ${BOLD}XrayR v0.9.19 — VPS 一键部署脚本${NC}${CYAN}             ║${NC}"
echo -e "${CYAN}║   VLESS+REALITY / TLS+XHTTP+CDN / AnyTLS+TLS      ║${NC}"
echo -e "${CYAN}║        适配 K2Board (UniProxy) 面板                 ║${NC}"
echo -e "${CYAN}║                                                   ║${NC}"
echo -e "${CYAN}╚═══════════════════════════════════════════════════╝${NC}"
echo ""
pause 2

# ---------------------------------------------------------------------------
# 询问面板配置
# ---------------------------------------------------------------------------
log_step "第一步：面板连接信息"

echo -e "  ${YELLOW}请准备好以下信息（在面板的节点页面可找到）：${NC}"
echo ""
echo -e "  1. 面板地址     — 例如: ${GREEN}https://panel.your-domain.com${NC}"
echo -e "  2. 节点 ID      — 每条节点自己的数字 ID"
echo -e "  3. 节点 API Key — 选完协议后，每条节点单独填写自己的 Key"
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

# 节点 ID。API Key 在选定协议后按节点分别询问。
while true; do
    echo ""
    echo -e "  ${BOLD}请输入第一条节点 ID：${NC}"
    echo -e "  ${YELLOW}单协议时这就是该节点的 ID。双协议时这一条给 VLESS，另一条稍后填写。${NC}"
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

# 收集 TLS 域名和证书。仅 Hysteria2 调用。AnyTLS 仍走下面原来的提示，避免两边互相影响。
collect_hysteria2_tls() {
    echo ""
    log_step "第二步续：Hysteria2 域名 & TLS 证书"

    while true; do
        echo ""
        echo -e "  ${BOLD}请输入 Hysteria2 节点域名：${NC}"
        echo -e "  ${YELLOW}例如: hy2.your-domain.com（需提前将 DNS A 记录指向本机 IP）${NC}"
        echo -e "  ${YELLOW}客户端 SNI 必须是这个域名。协议监听 UDP，不是 TCP。${NC}"
        read -r -p "  → " DOMAIN_NAME

        if [[ -z "$DOMAIN_NAME" ]]; then
            log_warn "域名不能为空"
            continue
        fi
        if [[ ! "$DOMAIN_NAME" =~ \. ]]; then
            log_warn "请输入完整域名（如 hy2.example.com）"
            continue
        fi
        break
    done
    echo ""
    echo -e "  ${GREEN}域名已记录: ${DOMAIN_NAME}${NC}"
    require_domain_points_here "$DOMAIN_NAME"

    HY2_CAMOUFLAGE=false
    echo ""
    echo -e "  ${BOLD}是否用 nginx 占用 TCP 80 和 TCP 443，给 Hysteria2 做伪装站？${NC}"
    echo -e "  ${YELLOW}默认否。选是之后，公网 80 会 301 到 HTTPS，本机 127.0.0.1:80 返回页面。${NC}"
    echo -e "  ${YELLOW}VLESS 的面板端口如果也是 443，安装会停止。UDP 仍给 Hysteria2。${NC}"
    echo -e "  ${YELLOW}输入 Y 安装，其他任意键保持现在的证书方式和 404 伪装。${NC}"
    read -r -p "  → " masq_answer
    if [[ "${masq_answer:0:1}" == "Y" || "${masq_answer:0:1}" == "y" ]]; then
        HY2_CAMOUFLAGE=true
        while true; do
            echo -e "  ${BOLD}请输入证书通知邮箱（Let's Encrypt 要求）：${NC}"
            read -r -p "  → " CERT_EMAIL
            if [[ -z "$CERT_EMAIL" || ! "$CERT_EMAIL" =~ @ ]]; then
                log_warn "请输入有效的邮箱地址"
                continue
            fi
            break
        done
        CERT_MODE="file"
        CERT_FILE_PATH="${XRAYR_INSTALL_DIR}/cert/${DOMAIN_NAME}.crt"
        KEY_FILE_PATH="${XRAYR_INSTALL_DIR}/cert/${DOMAIN_NAME}.key"
        echo -e "  ${GREEN}将安装 nginx + certbot，证书复制到 ${CERT_FILE_PATH}${NC}"
        echo -e "  ${GREEN}Hysteria2 伪装: MasqType proxy，MasqURL http://127.0.0.1:80${NC}"
        echo -e "  ${YELLOW}不写混淆密码，也不写带宽。${NC}"
        echo ""
        pause 2
        return 0
    fi

    echo ""
    echo -e "  ${BOLD}━━━ TLS 证书配置 ━━━${NC}"
    echo ""

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

    CERT_FILE_PATH=""
    KEY_FILE_PATH=""
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

    if [[ -z "$CERT_FILE_PATH" ]]; then
        if [[ -z "$FOUND_CERT" ]]; then
            echo -e "  ${YELLOW}未检测到域名 ${DOMAIN_NAME} 的已有证书${NC}"
        fi

        echo ""
        echo -e "  ${BOLD}请选择证书获取方式：${NC}"
        echo ""
        echo -e "  ${GREEN}[1]${NC} 自动申请 Let's Encrypt（HTTP 验证，需 80/TCP 对外开放）"
        echo -e "  ${GREEN}[2]${NC} 手动输入已有证书路径"
        echo ""

        while true; do
            read -r -n 1 -p "  → " cert_choice
            echo ""
            case "$cert_choice" in
                1)
                    CERT_MODE="http"
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
    fi
    echo ""
    echo -e "  ${YELLOW}Hysteria2 不使用 REALITY，也不使用 AnyTLS Fallback。${NC}"
    echo -e "  ${YELLOW}配置会写入 ALPN: [\"h3\"]。混淆和 Brutal 默认关闭。${NC}"
    echo ""
    pause 2
}

# VLESS + Hysteria2 时，第一个 NodeID 留给 VLESS，这里再要一个 Hysteria2 的。
collect_hysteria2_node_id() {
    echo ""
    echo -e "  ${YELLOW}VLESS 和 Hysteria2 在面板上是两条节点，NodeID 不能相同。${NC}"
    echo -e "  ${GREEN}已记录的 NodeID ${NODE_ID} 将用于 VLESS${NC}"
    echo ""
    while true; do
        echo -e "  ${BOLD}请输入 Hysteria2 节点 ID：${NC}"
        read -r -p "  → " HY2_NODE_ID
        if [[ -z "$HY2_NODE_ID" ]]; then
            log_warn "节点 ID 不能为空"
            continue
        fi
        if [[ ! "$HY2_NODE_ID" =~ ^[0-9]+$ ]]; then
            log_warn "节点 ID 必须是纯数字"
            continue
        fi
        if [[ "$HY2_NODE_ID" == "$NODE_ID" ]]; then
            log_warn "Hysteria2 节点 ID 不能和 VLESS 相同"
            continue
        fi
        break
    done
    echo -e "  ${GREEN}VLESS NodeID: ${NODE_ID}  |  Hysteria2 NodeID: ${HY2_NODE_ID}${NC}"
}

# 按协议写入各自的 ApiKey。other 非空且相同钥匙时，要明确输入 Y 才接受。
collect_node_api_key() {
    local label="$1"
    local dest="$2"
    local other="${3:-}"
    local value=""
    local confirm=""

    echo ""
    echo -e "  ${YELLOW}${label} 使用面板上该节点自己的 API Key。${NC}"
    while true; do
        echo ""
        echo -e "  ${BOLD}请输入 ${label} 节点的 API Key：${NC}"
        read -r -p "  → " value
        if [[ -z "$value" ]]; then
            log_warn "API Key 不能为空"
            continue
        fi
        if [[ ${#value} -lt 10 ]]; then
            log_warn "API Key 看起来太短（少于10位），请确认是否正确"
            echo -e "  ${YELLOW}输入 Y 继续使用，其他任意键重新输入：${NC}"
            read -r -p "  → " confirm
            if [[ "${confirm:0:1}" != "Y" && "${confirm:0:1}" != "y" ]]; then
                continue
            fi
        fi
        if [[ -n "$other" && "$value" == "$other" ]]; then
            log_warn "这把 Key 和上一条节点相同。面板若给每条节点单独发了 Key，请改用 ${label} 那把。"
            echo -e "  ${YELLOW}输入 Y 仍使用这一把，其他任意键重新输入：${NC}"
            read -r -p "  → " confirm
            if [[ "${confirm:0:1}" != "Y" && "${confirm:0:1}" != "y" ]]; then
                continue
            fi
        fi
        printf -v "$dest" '%s' "$value"
        break
    done
    echo -e "  ${GREEN}${label} API Key 已记录: ${value:0:8}****${NC}"
}

# force 为 force 时重问当前协议。否则只补尚未填写的 Key。
collect_selected_api_keys() {
    local force="${1:-}"
    if $INSTALL_VLESS && [[ "$force" == "force" || -z "${VLESS_API_KEY:-}" ]]; then
        collect_node_api_key "VLESS" VLESS_API_KEY
    fi
    if $INSTALL_ANYTLS && [[ "$force" == "force" || -z "${ANYTLS_API_KEY:-}" ]]; then
        if $INSTALL_VLESS; then
            collect_node_api_key "AnyTLS" ANYTLS_API_KEY "${VLESS_API_KEY:-}"
        else
            collect_node_api_key "AnyTLS" ANYTLS_API_KEY
        fi
    fi
    if $INSTALL_HY2 && [[ "$force" == "force" || -z "${HY2_API_KEY:-}" ]]; then
        if $INSTALL_VLESS; then
            collect_node_api_key "Hysteria2" HY2_API_KEY "${VLESS_API_KEY:-}"
        else
            collect_node_api_key "Hysteria2" HY2_API_KEY
        fi
    fi
}

mask_key() {
    local value="${1:-}"
    if [[ -z "$value" ]]; then
        printf '%s' "(未填写)"
    else
        printf '%s****' "${value:0:8}"
    fi
}

# [4] 只有 Hysteria2 时用第一个 NodeID。[5] 用单独收集的 HY2_NODE_ID。
hy2_panel_id() {
    if $INSTALL_VLESS && $INSTALL_HY2; then
        printf '%s' "$HY2_NODE_ID"
    else
        printf '%s' "$NODE_ID"
    fi
}

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
echo -e "  ${GREEN}[4]${NC} Hysteria2 + TLS      — 需要域名和证书，监听 UDP"
echo -e "  ${GREEN}[5]${NC} VLESS + Hysteria2    — 新机器同时写入两条节点"
echo -e "      ${YELLOW}已有 VLESS / AnyTLS 时不要选 [4] 或 [5]：本脚本会整份覆盖 config.yml。${NC}"
echo -e "      ${YELLOW}正在跑的机器保持现有二进制。新机器才装 Hysteria2，说明见 deploy/HYSTERIA2.md。${NC}"
echo ""

INSTALL_MODE=""
while true; do
    read -r -n 1 -p "  → " mode
    echo ""
    case "$mode" in
        1) INSTALL_MODE="vless"; break ;;
        2) INSTALL_MODE="anytls"; break ;;
        3) INSTALL_MODE="dual"; break ;;
        4) INSTALL_MODE="hysteria2"; break ;;
        5) INSTALL_MODE="vless-hysteria2"; break ;;
        *) log_warn "请输入 1、2、3、4 或 5" ;;
    esac
done

INSTALL_VLESS=false
INSTALL_ANYTLS=false
INSTALL_HY2=false
case "$INSTALL_MODE" in
    vless) INSTALL_VLESS=true ;;
    anytls) INSTALL_ANYTLS=true ;;
    dual) INSTALL_VLESS=true; INSTALL_ANYTLS=true ;;
    hysteria2) INSTALL_HY2=true ;;
    vless-hysteria2) INSTALL_VLESS=true; INSTALL_HY2=true ;;
esac

echo ""
echo -e "  ${GREEN}已选择: ${INSTALL_MODE}${NC}"
if $INSTALL_HY2; then
    echo ""
    echo -e "  ${YELLOW}Hysteria2 监听 UDP。云安全组和系统防火墙都要放行面板上的 UDP 端口。${NC}"
    echo -e "  ${YELLOW}本选项会下载 ${XRAYR_VERSION}。同目录已有 ./XrayR 时使用本地文件。${NC}"
    echo -e "  ${YELLOW}不要在已经运行 VLESS / AnyTLS 的机器上选择本选项。${NC}"
fi
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
HY2_NODE_ID=""           # 仅 [5] 使用；[4] 继续用 NODE_ID
VLESS_API_KEY=""
ANYTLS_API_KEY=""
HY2_API_KEY=""
HY2_CAMOUFLAGE=false
VLESS_LISTEN_PORT=""
ANYTLS_LISTEN_PORT=""
HY2_LISTEN_PORT=""
HY2_PANEL_SNI=""

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

if $INSTALL_VLESS && $INSTALL_HY2; then
    collect_hysteria2_node_id
fi

collect_selected_api_keys

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

if $INSTALL_HY2; then
    collect_hysteria2_tls
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

while true; do
echo ""
echo -e "  ${BOLD}请确认以下配置信息：${NC}"
echo ""
echo -e "  ┌─────────────────────────────────────────────┐"
echo -e "  │  面板地址:   ${GREEN}${V2BOARD_HOST}${NC}"
echo -e "  │  安装模式:   ${GREEN}${INSTALL_MODE}${NC}"
if $INSTALL_VLESS; then
echo -e "  │  🟢 VLESS:    NodeID ${NODE_ID}，Key $(mask_key "$VLESS_API_KEY")${NC}"
fi
if $INSTALL_ANYTLS; then
echo -e "  │  🔵 AnyTLS:   NodeID ${ANYTLS_NODE_ID}，Key $(mask_key "$ANYTLS_API_KEY")${NC}"
echo -e "  │     域名:     ${GREEN}${DOMAIN_NAME}${NC}"
if [[ "$CERT_MODE" == "http" ]]; then
echo -e "  │     证书:     ${GREEN}HTTP 自动申请 (${CERT_EMAIL})${NC}"
else
echo -e "  │     证书:     ${GREEN}${CERT_FILE_PATH}${NC}"
fi
fi
if $INSTALL_HY2; then
echo -e "  │  🟣 Hysteria2: NodeID $(hy2_panel_id)，Key $(mask_key "$HY2_API_KEY")${NC}"
echo -e "  │     域名:     ${GREEN}${DOMAIN_NAME}${NC}"
if [[ "${HY2_CAMOUFLAGE:-false}" == "true" ]]; then
echo -e "  │     证书:     ${GREEN}伪装站 certbot，复制到 ${CERT_FILE_PATH}${NC}"
elif [[ "$CERT_MODE" == "http" ]]; then
echo -e "  │     证书:     ${GREEN}HTTP 自动申请 (${CERT_EMAIL})${NC}"
else
echo -e "  │     证书:     ${GREEN}${CERT_FILE_PATH}${NC}"
fi
fi
echo -e "  └─────────────────────────────────────────────┘"
echo ""
    echo -e "  ${BOLD}以上信息是否正确？${NC}"
    echo -e "  ${GREEN}[Y]${NC} 确认安装   ${RED}[N]${NC} 取消   ${YELLOW}[R]${NC} 重新输入"
    read -r -n 1 -p "  → " confirm
    echo ""

    case "$confirm" in
        Y|y)
            if $INSTALL_VLESS && $INSTALL_HY2; then
                if [[ -z "$HY2_NODE_ID" || "$HY2_NODE_ID" == "$NODE_ID" ]]; then
                    log_warn "Hysteria2 需要一个和 VLESS 不同的节点 ID"
                    collect_hysteria2_node_id
                    continue
                fi
            fi
            if { $INSTALL_VLESS && [[ -z "$VLESS_API_KEY" ]]; } \
                || { $INSTALL_ANYTLS && [[ -z "$ANYTLS_API_KEY" ]]; } \
                || { $INSTALL_HY2 && [[ -z "$HY2_API_KEY" ]]; }; then
                log_warn "还有节点的 API Key 没填"
                collect_selected_api_keys
                continue
            fi
            break
            ;;
        N|n) echo ""; log_info "已取消安装"; exit 0 ;;
        R|r)
            echo ""
            echo -e "  ${YELLOW}需要重新输入哪个？${NC}"
            echo -e "  [1] 面板地址  [2] 各协议 API Key  [3] 节点 ID  [4] 协议选择"
            read -r -n 1 -p "  → " redo
            echo ""
            case "$redo" in
                1)
                    read -r -p "  新面板地址 → " V2BOARD_HOST
                    echo -e "  ${GREEN}已更新: ${V2BOARD_HOST}${NC}"
                    ;;
                2)
                    collect_selected_api_keys force
                    ;;
                3)
                    while true; do
                        read -r -p "  新节点 ID → " NODE_ID
                        if [[ "$NODE_ID" =~ ^[0-9]+$ ]]; then break; fi
                        log_warn "节点 ID 必须是纯数字"
                    done
                    echo -e "  ${GREEN}已更新: ${NODE_ID}${NC}"
                    if $INSTALL_VLESS && $INSTALL_HY2 && [[ "$HY2_NODE_ID" == "$NODE_ID" ]]; then
                        collect_hysteria2_node_id
                    fi
                    ;;
                4)
                    echo -e "  [1] VLESS+REALITY  [2] AnyTLS+TLS  [3] VLESS+AnyTLS  [4] Hysteria2  [5] VLESS+Hysteria2"
                    echo -e "  ${YELLOW}改到尚未填过的协议时，会补问该协议的 API Key、域名和证书。${NC}"
                    read -r -n 1 -p "  → " mode
                    echo ""
                    case "$mode" in
                        1) INSTALL_MODE="vless"; INSTALL_VLESS=true; INSTALL_ANYTLS=false; INSTALL_HY2=false ;;
                        2) INSTALL_MODE="anytls"; INSTALL_VLESS=false; INSTALL_ANYTLS=true; INSTALL_HY2=false ;;
                        3)
                            INSTALL_MODE="dual"
                            INSTALL_VLESS=true
                            INSTALL_ANYTLS=true
                            INSTALL_HY2=false
                            if [[ -z "$ANYTLS_NODE_ID" || "$ANYTLS_NODE_ID" == "$NODE_ID" ]]; then
                                while true; do
                                    read -r -p "  AnyTLS 节点 ID → " ANYTLS_NODE_ID
                                    if [[ "$ANYTLS_NODE_ID" =~ ^[0-9]+$ && "$ANYTLS_NODE_ID" != "$NODE_ID" ]]; then
                                        break
                                    fi
                                    log_warn "AnyTLS 节点 ID 必须是纯数字，且不能和 VLESS 相同"
                                done
                            fi
                            ;;
                        4)
                            INSTALL_MODE="hysteria2"
                            INSTALL_VLESS=false
                            INSTALL_ANYTLS=false
                            INSTALL_HY2=true
                            if [[ -z "${CERT_MODE}" ]]; then
                                collect_hysteria2_tls
                            fi
                            ;;
                        5)
                            INSTALL_MODE="vless-hysteria2"
                            INSTALL_VLESS=true
                            INSTALL_ANYTLS=false
                            INSTALL_HY2=true
                            if [[ -z "$HY2_NODE_ID" || "$HY2_NODE_ID" == "$NODE_ID" ]]; then
                                collect_hysteria2_node_id
                            fi
                            if [[ -z "${CERT_MODE}" ]]; then
                                collect_hysteria2_tls
                            fi
                            ;;
                    esac
                    collect_selected_api_keys
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

# 选项 [4] 和 [5] 会整份重写 config.yml。已经在跑 VLESS / AnyTLS 时直接停止。
if $INSTALL_HY2 && [[ -f "$XRAYR_INSTALL_DIR/config.yml" ]]; then
    if config_has_existing_node "$XRAYR_INSTALL_DIR/config.yml"; then
        log_error "检测到 ${XRAYR_INSTALL_DIR}/config.yml 里已有 VLESS、AnyTLS 或 Hysteria2。安装脚本会覆盖整份配置，已停止。正在跑的 VLESS 保持 v0.9.16，不要为了这次更新换成 v0.9.19。新机器请看 deploy/HYSTERIA2.md。"
    fi
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
    if $INSTALL_HY2 && $HYSTERIA2_NEEDS_LOCAL_BINARY && [[ ! -f "./XrayR" ]]; then
        echo ""
        echo -e "  ${RED}Hysteria2 安装需要脚本同目录中的本地二进制 ./XrayR${NC}"
        echo -e "  ${YELLOW}GitHub Release ${XRAYR_VERSION} 还没有 Hysteria2。下载那个版本后把 NodeType 写成 hysteria2，旧程序会把它当成 VLESS。${NC}"
        echo ""
        echo -e "  ${BOLD}在开发机编译并上传：${NC}"
        echo -e "  ${GREEN}CGO_ENABLED=0 GOOS=linux GOARCH=${PLATFORM} go build -mod=vendor -trimpath -ldflags \"-s -w -buildid=\" -o XrayR ./main${NC}"
        echo -e "  ${GREEN}scp XrayR root@<VPS_IP>:/root/${NC}"
        echo ""
        echo -e "  ${YELLOW}上传后在该目录重新运行本脚本，并选择 [4]。${NC}"
        return 1
    fi

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
log_step "第八步：读取面板端口并生成配置文件"
pause 1

load_panel_ports
if $INSTALL_HY2; then
    require_domain_points_here "$DOMAIN_NAME"
fi
install_hy2_camouflage

# 主配置 — 根据安装模式动态生成 Nodes 列表
cat > "$XRAYR_INSTALL_DIR/config.yml" << YML
# ============================================================================
# XrayR 配置文件 — 安装模式: ${INSTALL_MODE}
# 内置 xray-core v26.11.9 | XrayR4u v0.9.19
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
      ApiKey: "$VLESS_API_KEY"
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
      # REALITY: 兼容 Mihomo 等客户端（xray-core ≥26.7.11 勿留空）
      RealityMinClientVer: "1.8.0"
      # 可选后量子 / CDN 覆盖见官方文档 deploy/K2BOARD_INTEGRATION.md
      # RealityMldsa65Seed: ""
      # VlessDecryption: ""
      # Transport: "xhttp"
      # Security: "tls"
      # XHTTP: { Host: "cdn.example.com", Path: "/vless", Mode: "auto" }
      CertConfig:
        CertMode: none
YML
    log_info "VLESS 节点配置已添加 (REALITY + minClientVer=1.8.0)"
fi

# --- AnyTLS 节点 ---
if $INSTALL_ANYTLS; then
    cat >> "$XRAYR_INSTALL_DIR/config.yml" << YML
  - PanelType: "V2board"
    ApiConfig:
      ApiHost: "$V2BOARD_HOST"
      ApiKey: "$ANYTLS_API_KEY"
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

# --- Hysteria2 节点（独立条目；不改上面的 VLESS / AnyTLS 块） ---
if $INSTALL_HY2; then
    cat >> "$XRAYR_INSTALL_DIR/config.yml" << YML
  - PanelType: "V2board"
    ApiConfig:
      ApiHost: "$V2BOARD_HOST"
      ApiKey: "$HY2_API_KEY"
      NodeID: $(hy2_panel_id)
      NodeType: hysteria2
      Timeout: 30
      EnableReality: false
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
      # 密码是面板用户 UUID。传输固定为 hysteria / UDP / ALPN h3。
      # 不读取 Transport、XHTTP、REALITY。不写混淆和带宽。
      $(if [[ "${HY2_CAMOUFLAGE:-false}" == "true" ]]; then
          echo "Hysteria2:"
          echo "        MasqType: \"proxy\""
          echo "        MasqURL: \"http://127.0.0.1:80\""
      else
          echo "# Hysteria2:"
          echo "      #   MasqType: \"404\""
      fi)
      CertConfig:
        CertMode: $CERT_MODE
        CertDomain: "$DOMAIN_NAME"
        ALPN: ["h3"]
        $(if [[ "$CERT_MODE" == "file" ]]; then
            echo "CertFile: \"$CERT_FILE_PATH\""
            echo "        KeyFile: \"$KEY_FILE_PATH\""
        elif [[ "$CERT_MODE" == "http" ]]; then
            echo "Email: \"$CERT_EMAIL\""
        fi)
YML
    log_info "Hysteria2 节点配置已添加 (${CERT_MODE} 证书, ALPN h3, UDP)"
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
chmod 600 "$XRAYR_INSTALL_DIR/config.yml"
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
WorkingDirectory=/etc/XrayR
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

# 端口来自面板 server_port。HTTP 证书或伪装站才额外放行 TCP 80。
# 没有读到端口时前面已经询问，这里不再回退到 443。
apply_firewall_ports

if [[ ${#FIREWALL_PORTS[@]} -gt 0 ]]; then
    log_info "需要放行的 TCP 端口: ${FIREWALL_PORTS[*]}"
else
    log_info "需要放行的 TCP 端口: （无）"
fi
if [[ ${#FIREWALL_UDP_PORTS[@]} -gt 0 ]]; then
    log_info "需要放行的 UDP 端口: ${FIREWALL_UDP_PORTS[*]}"
else
    log_info "需要放行的 UDP 端口: （无）"
fi

if command -v ufw &>/dev/null && ufw status 2>/dev/null | grep -q "active"; then
    log_info "检测到 UFW，添加规则..."
    for port in "${FIREWALL_PORTS[@]}"; do
        ufw allow $port/tcp comment "XrayR port $port" 2>/dev/null || true
    done
    for port in "${FIREWALL_UDP_PORTS[@]}"; do
        ufw allow $port/udp comment "XrayR UDP port $port" 2>/dev/null || true
    done
    ufw reload 2>/dev/null || true
    log_info "UFW 规则已添加"

elif command -v firewall-cmd &>/dev/null && firewall-cmd --state 2>/dev/null | grep -q "running"; then
    log_info "检测到 firewalld，添加规则..."
    for port in "${FIREWALL_PORTS[@]}"; do
        firewall-cmd --permanent --add-port=$port/tcp 2>/dev/null || true
    done
    for port in "${FIREWALL_UDP_PORTS[@]}"; do
        firewall-cmd --permanent --add-port=$port/udp 2>/dev/null || true
    done
    firewall-cmd --reload 2>/dev/null || true
    log_info "firewalld 规则已添加"

elif command -v nft &>/dev/null && nft list ruleset 2>/dev/null | grep -q "input"; then
    log_info "检测到 nftables，请按实际端口手动添加："
    if [[ ${#FIREWALL_PORTS[@]} -gt 0 ]]; then
        echo -e "  ${YELLOW}nft add rule inet filter input tcp dport {$(IFS=,; echo "${FIREWALL_PORTS[*]}")} accept${NC}"
    fi
    if [[ ${#FIREWALL_UDP_PORTS[@]} -gt 0 ]]; then
        echo -e "  ${YELLOW}nft add rule inet filter input udp dport {$(IFS=,; echo "${FIREWALL_UDP_PORTS[*]}")} accept${NC}"
    fi

elif command -v iptables &>/dev/null; then
    log_info "使用 iptables 添加规则..."
    for port in "${FIREWALL_PORTS[@]}"; do
        iptables -C INPUT -p tcp --dport $port -j ACCEPT 2>/dev/null || \
            iptables -A INPUT -p tcp --dport $port -j ACCEPT
    done
    for port in "${FIREWALL_UDP_PORTS[@]}"; do
        iptables -C INPUT -p udp --dport $port -j ACCEPT 2>/dev/null || \
            iptables -A INPUT -p udp --dport $port -j ACCEPT
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
    log_warn "未检测到本机防火墙工具，只打印要放行的端口。"
fi
print_cloud_firewall_reminder
pause 2

# ============================================================================
# 9b. 可选：本机生成 REALITY / PQ 密钥材料（XrayR tools，同版本 core）
# ============================================================================
log_step "密钥材料生成（可选 · XrayR tools）"
pause 1

KEYS_DIR="${XRAYR_INSTALL_DIR}/keys"
XRAYR_BIN="${XRAYR_BIN_DIR}/${XRAYR_BIN_NAME}"

echo -e "  ${BOLD}是否在本机生成密钥 / 后量子材料？${NC}"
echo -e "  ${YELLOW}（使用节点自带 ${XRAYR_BIN} tools，与内嵌 xray-core 同版本）${NC}"
echo ""
echo -e "  ${GREEN}[1]${NC} REALITY X25519 密钥对          → x25519"
echo -e "  ${GREEN}[2]${NC} ML-DSA-65（Seed+Verify）       → mldsa65"
echo -e "  ${GREEN}[3]${NC} VLESS Encryption 成对串        → vlessenc（推荐 PQ 载荷）"
echo -e "  ${GREEN}[4]${NC} 全部生成并保存到 ${KEYS_DIR}/"
echo -e "  ${GREEN}[0]${NC} 跳过（密钥由面板一键生成亦可）"
echo ""

GEN_CHOICE=""
while true; do
    read -r -n 1 -p "  → " GEN_CHOICE
    echo ""
    case "$GEN_CHOICE" in
        0|1|2|3|4) break ;;
        *) log_warn "请输入 0–4" ;;
    esac
done

run_xrayr_tools() {
    local subcmd=$1
    local outfile=$2
    if [[ ! -x "$XRAYR_BIN" ]]; then
        log_warn "找不到可执行文件: $XRAYR_BIN"
        return 1
    fi
    # tools 模式不需要 config.yml
    if [[ -n "$outfile" ]]; then
        if "$XRAYR_BIN" tools "$subcmd" >"$outfile" 2>&1; then
            log_info "已写入: $outfile"
            # 同时打印摘要（前 20 行）
            head -n 20 "$outfile" | sed 's/^/    /'
            return 0
        fi
        log_warn "生成失败: $subcmd"
        cat "$outfile" 2>/dev/null | head -n 5 | sed 's/^/    /' || true
        return 1
    fi
    "$XRAYR_BIN" tools "$subcmd"
}

if [[ "$GEN_CHOICE" != "0" ]]; then
    mkdir -p "$KEYS_DIR"
    chmod 700 "$KEYS_DIR"
    echo ""
    case "$GEN_CHOICE" in
        1)
            run_xrayr_tools x25519 "${KEYS_DIR}/reality-x25519.txt"
            echo -e "  ${YELLOW}→ 将 PrivateKey 填面板 private_key，Password/PublicKey 填 public_key / 订阅 pbk${NC}"
            ;;
        2)
            run_xrayr_tools mldsa65 "${KEYS_DIR}/reality-mldsa65.txt"
            echo -e "  ${YELLOW}→ Seed 填服务端 mldsa65_seed / RealityMldsa65Seed；Verify 填订阅 mldsa65Verify${NC}"
            ;;
        3)
            run_xrayr_tools vlessenc "${KEYS_DIR}/vless-encryption.txt"
            echo -e "  ${YELLOW}→ decryption 填面板/节点服务端；encryption 填客户端订阅（成对使用，勿混用两套）${NC}"
            ;;
        4)
            run_xrayr_tools x25519 "${KEYS_DIR}/reality-x25519.txt"
            echo ""
            run_xrayr_tools mldsa65 "${KEYS_DIR}/reality-mldsa65.txt"
            echo ""
            run_xrayr_tools vlessenc "${KEYS_DIR}/vless-encryption.txt"
            echo ""
            log_info "全部材料已保存到 ${KEYS_DIR}/ （权限 700）"
            echo -e "  ${YELLOW}REALITY 密钥 / ML-DSA / Encryption 请按文件说明填入面板或 config.yml${NC}"
            ;;
    esac
    echo ""
    echo -e "  ${CYAN}以后可随时手动生成：${NC}"
    echo -e "  ${GREEN}${XRAYR_BIN} tools x25519${NC}"
    echo -e "  ${GREEN}${XRAYR_BIN} tools mldsa65${NC}"
    echo -e "  ${GREEN}${XRAYR_BIN} tools vlessenc${NC}"
    echo -e "  ${GREEN}${XRAYR_BIN} tools help${NC}"
else
    log_info "已跳过本机密钥生成（可使用面板一键生成，或稍后 XrayR tools）"
fi
pause 2

# ============================================================================
# 10. 启动服务
# ============================================================================
log_step "第十二步：启动 XrayR 服务"
pause 1

echo -e "  ${CYAN}正在启用并启动 xrayr 服务...${NC}"
if $INSTALL_HY2; then
    require_domain_points_here "$DOMAIN_NAME"
fi
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

echo ""
echo -e "  ${BOLD}当前监听（ss）${NC}"
ss -tulnp 2>/dev/null | grep -E 'XrayR|nginx' || true
echo ""
if $INSTALL_HY2 || $INSTALL_ANYTLS; then
    cert_show=""
    if [[ -n "${CERT_FILE_PATH:-}" && -f "$CERT_FILE_PATH" ]]; then
        cert_show="$CERT_FILE_PATH"
    elif [[ -n "${DOMAIN_NAME:-}" && -f "${XRAYR_INSTALL_DIR}/cert/certificates/${DOMAIN_NAME}.crt" ]]; then
        cert_show="${XRAYR_INSTALL_DIR}/cert/certificates/${DOMAIN_NAME}.crt"
    fi
    if [[ -n "$cert_show" ]] && command -v openssl >/dev/null 2>&1; then
        echo -e "  ${BOLD}证书 ${cert_show}${NC}"
        openssl x509 -in "$cert_show" -noout -subject -enddate 2>/dev/null || true
        echo ""
    fi
fi
echo -e "  ${BOLD}用户加载日志${NC}"
journalctl -u xrayr --no-pager -n 400 2>/dev/null | grep -E 'Added [0-9]+ (new|hysteria2) users|Updated [0-9]+ anytls users|start failed' || true
echo ""
if $INSTALL_HY2; then
    echo -e "  ${YELLOW}上面的 TCP 监听正常，不能证明 Hysteria2 的 UDP 已经可用。${NC}"
    echo -e "  ${YELLOW}面板里 Hysteria2 的 host 必须等于 ${DOMAIN_NAME}。/config 不返回订阅主机名，节点核对不了这一项。${NC}"
    echo -e "  ${YELLOW}证书有效期内，这个名字都要解析到本机。${NC}"
    echo ""
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
echo -e "  │  🟢 VLESS:    NodeID ${NODE_ID}，Key $(mask_key "$VLESS_API_KEY")，TCP ${VLESS_LISTEN_PORT:-未取得}${NC}"
fi
if $INSTALL_ANYTLS; then
echo -e "  │  🔵 AnyTLS:   NodeID ${ANYTLS_NODE_ID}，Key $(mask_key "$ANYTLS_API_KEY")，域名 ${GREEN}${DOMAIN_NAME}${NC}"
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
if $INSTALL_HY2; then
echo -e "  │  🟣 Hysteria2: NodeID $(hy2_panel_id)，Key $(mask_key "$HY2_API_KEY")，UDP ${HY2_LISTEN_PORT:-未取得}，域名 ${GREEN}${DOMAIN_NAME}${NC}"
echo -e "  │     ALPN:      ${GREEN}h3${NC}    REALITY: ${GREEN}关闭${NC}"
if [[ "${HY2_CAMOUFLAGE:-false}" == "true" ]]; then
echo -e "  │     伪装:     ${GREEN}proxy → http://127.0.0.1:80${NC}"
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
echo -e "  ${GREEN}ss -tlnp | grep XrayR${NC}       查看 TCP 监听"
if $INSTALL_HY2; then
echo -e "  ${GREEN}ss -ulnp | grep XrayR${NC}       查看 Hysteria2 的 UDP 监听"
fi
echo -e "  ${GREEN}systemctl restart xrayr${NC}     重启服务"
echo -e "  ${GREEN}vi ${XRAYR_INSTALL_DIR}/config.yml${NC}   编辑配置"
echo ""
echo -e "  ${BOLD}🔑 密钥 / 后量子材料（节点自带，同版本 core）${NC}"
echo -e "  ${GREEN}XrayR tools x25519${NC}           REALITY 密钥对"
echo -e "  ${GREEN}XrayR tools mldsa65${NC}          ML-DSA Seed + Verify"
echo -e "  ${GREEN}XrayR tools vlessenc${NC}         VLESS Encryption 成对串"
echo -e "  ${GREEN}XrayR tools help${NC}             全部 tools 说明"
if [[ -d "${XRAYR_INSTALL_DIR}/keys" ]]; then
echo -e "  ${YELLOW}安装时生成的材料: ${XRAYR_INSTALL_DIR}/keys/${NC}"
fi
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

if $INSTALL_HY2; then
echo -e "  ${YELLOW}💡 Hysteria2 监听 UDP ${HY2_LISTEN_PORT:-未取得}。云安全组要放行这个 UDP 端口，不要只放行 TCP。${NC}"
echo -e "  ${YELLOW}   面板 host 必须写成 ${DOMAIN_NAME}。/config 里没有订阅主机名。${NC}"
if [[ "${CERT_MODE:-}" == "http" ]]; then
echo -e "  ${YELLOW}   证书是 HTTP 验证，还要开放 TCP 80。${NC}"
fi
echo -e "  ${YELLOW}   客户端: hysteria2://<用户UUID>@${DOMAIN_NAME}:${HY2_LISTEN_PORT:-端口}?sni=${DOMAIN_NAME}&alpn=h3${NC}"
echo -e "  ${YELLOW}   面板若已下发 hysteria2 订阅，用订阅。否则先用上面这条手工链接。${NC}"
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
if $INSTALL_HY2; then
echo -e "     Hysteria2 UDP:      ${GREEN}ss -ulnp | grep XrayR${NC}"
fi
echo -e "  4. 面板端口配置:       面板 → 节点 → port 字段"
if $INSTALL_ANYTLS || $INSTALL_HY2; then
echo -e "  5. TLS 证书:           ${GREEN}journalctl -u xrayr | grep -i cert${NC}"
fi
echo ""
pause 1

echo -e "  ${YELLOW}📖 更多文档: https://github.com/HenZenKuriRIP/XrayR4u${NC}"
echo ""
