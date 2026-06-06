package main

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ParsedProxyResult 用于前端返回解析结果
type ParsedProxyResult struct {
	Name   string                 `json:"name"`
	Type   string                 `json:"type"`
	Server string                 `json:"server"`
	Port   int                    `json:"port"`
	Config map[string]interface{} `json:"config"`
}

// ParseProxyLink 解析代理链接为通用的 Clash 配置 Map
func ParseProxyLink(link string) (*ParsedProxyResult, error) {
	link = strings.TrimSpace(link)
	if link == "" {
		return nil, fmt.Errorf("link is empty")
	}

	// 规范化特殊的 socks5 链接格式 (ip:port:user:pass 转换成 user:pass@ip:port)
	link = normalizeSocksLink(link)

	u, err := url.Parse(link)
	if err != nil {
		return nil, fmt.Errorf("invalid URL format: %w", err)
	}

	scheme := strings.ToLower(u.Scheme)

	switch scheme {
	case "vless":
		return parseVLESS(u)
	case "hysteria2", "hy2":
		return parseHysteria2(u)
	case "anytls":
		return parseAnyTLS(u)
	case "ss":
		return parseSS(u)
	case "socks5", "socks":
		return parseSocks5(u)
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", scheme)
	}
}

func parseVLESS(u *url.URL) (*ParsedProxyResult, error) {
	// vless://uuid@server:port?query#name
	uuid := u.User.Username()
	host := u.Hostname()
	portStr := u.Port()
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid port: %s", portStr)
	}

	name := u.Fragment
	if name == "" {
		name = host
	}
	if unescaped, err := url.PathUnescape(name); err == nil {
		name = unescaped
	}

	query := u.Query()
	config := map[string]interface{}{
		"name":   name,
		"type":   "vless",
		"server": host,
		"port":   port,
		"uuid":   uuid,
		"tls":    false,
	}

	if security := query.Get("security"); security == "tls" || security == "reality" {
		config["tls"] = true
		if sni := query.Get("sni"); sni != "" {
			config["servername"] = sni
		}
		if alpn := query.Get("alpn"); alpn != "" {
			config["alpn"] = strings.Split(alpn, ",")
		}
		if fp := query.Get("fp"); fp != "" {
			config["client-fingerprint"] = fp
		}
	}

	if flow := query.Get("flow"); flow != "" {
		config["flow"] = flow
	}
	if encryption := query.Get("encryption"); encryption != "" {
		config["encryption"] = encryption
	}
	if truthyString(firstNonEmptyQuery(query, "allowInsecure", "insecure", "skip-cert-verify")) {
		config["skip-cert-verify"] = true
	}

	// Network
	switch net := query.Get("type"); net {
	case "ws":
		config["network"] = "ws"
		config["ws-opts"] = map[string]interface{}{
			"path": query.Get("path"),
			"headers": map[string]interface{}{
				"Host": query.Get("host"),
			},
		}
	case "grpc":
		config["network"] = "grpc"
		config["grpc-opts"] = map[string]interface{}{
			"grpc-service-name": query.Get("serviceName"),
		}
	case "tcp":
		config["network"] = "tcp"
	}

	if security := query.Get("security"); security == "reality" {
		config["reality-opts"] = map[string]interface{}{
			"public-key": query.Get("pbk"),
			"short-id":   query.Get("sid"),
		}
	}

	return &ParsedProxyResult{
		Name:   name,
		Type:   "vless",
		Server: host,
		Port:   port,
		Config: config,
	}, nil
}

func parseAnyTLS(u *url.URL) (*ParsedProxyResult, error) {
	password := u.User.Username()
	host := u.Hostname()
	portStr := u.Port()
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid port: %s", portStr)
	}

	name := u.Fragment
	if name == "" {
		name = host
	}
	if unescaped, err := url.PathUnescape(name); err == nil {
		name = unescaped
	}

	query := u.Query()
	config := map[string]interface{}{
		"name":     name,
		"type":     "anytls",
		"server":   host,
		"port":     port,
		"password": password,
	}

	if sni := firstNonEmptyQuery(query, "sni", "servername", "peer"); sni != "" {
		config["sni"] = sni
	}
	if truthyString(firstNonEmptyQuery(query, "insecure", "allowInsecure", "skip-cert-verify")) {
		config["skip-cert-verify"] = true
	}
	if alpn := query.Get("alpn"); alpn != "" {
		config["alpn"] = strings.Split(alpn, ",")
	}
	if fp := firstNonEmptyQuery(query, "fp", "client-fingerprint"); fp != "" {
		config["client-fingerprint"] = fp
	}
	if truthyString(query.Get("udp")) {
		config["udp"] = true
	}

	return &ParsedProxyResult{
		Name:   name,
		Type:   "anytls",
		Server: host,
		Port:   port,
		Config: config,
	}, nil
}

func parseHysteria2(u *url.URL) (*ParsedProxyResult, error) {
	// hysteria2://password@server:port?sni=xxx#name
	password := u.User.Username()
	host := u.Hostname()
	portStr := u.Port()
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid port: %s", portStr)
	}

	name := u.Fragment
	if name == "" {
		name = host
	}
	if unescaped, err := url.PathUnescape(name); err == nil {
		name = unescaped
	}

	query := u.Query()
	config := map[string]interface{}{
		"name":     name,
		"type":     "hysteria2",
		"server":   host,
		"port":     port,
		"password": password,
	}

	if sni := query.Get("sni"); sni != "" {
		config["sni"] = sni
	}
	if obfuscator := query.Get("obfs"); obfuscator != "" {
		config["obfs"] = obfuscator
		if obfsPassword := query.Get("obfs-password"); obfsPassword != "" {
			config["obfs-password"] = obfsPassword
		}
	}

	return &ParsedProxyResult{
		Name:   name,
		Type:   "hysteria2",
		Server: host,
		Port:   port,
		Config: config,
	}, nil
}

func parseSS(u *url.URL) (*ParsedProxyResult, error) {
	// ss://base64(method:password)@server:port#name or ss://method:password@server:port#name
	var method, password string

	host := u.Hostname()
	portStr := u.Port()
	if host == "" || portStr == "" {
		if legacyURL, ok := decodeLegacySSURL(u); ok {
			return parseSS(legacyURL)
		}
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid port: %s", portStr)
	}

	// 尝试解 base64 的 userinfo
	userInfo := u.User.String()
	if !strings.Contains(userInfo, ":") {
		decoded, err := decodeAdaptiveBase64(userInfo)
		if err == nil && strings.Contains(decoded, ":") {
			parts := strings.SplitN(decoded, ":", 2)
			method = parts[0]
			password = parts[1]
		}
	} else {
		method = u.User.Username()
		password, _ = u.User.Password()
	}

	if method == "" || password == "" {
		// 某些链接格式是 ss://[base64(method:password@server:port)]#name
		// 但这是较老的规范，现在主流的是基于 URI 的格式
		return nil, fmt.Errorf("failed to extract method and password")
	}

	name := u.Fragment
	if name == "" {
		name = host
	}
	if unescaped, err := url.PathUnescape(name); err == nil {
		name = unescaped
	}

	config := map[string]interface{}{
		"name":     name,
		"type":     "ss",
		"server":   host,
		"port":     port,
		"cipher":   method,
		"password": password,
	}
	if truthyString(u.Query().Get("udp")) {
		config["udp"] = true
	}

	return &ParsedProxyResult{
		Name:   name,
		Type:   "ss",
		Server: host,
		Port:   port,
		Config: config,
	}, nil
}

func decodeLegacySSURL(u *url.URL) (*url.URL, bool) {
	encoded := strings.TrimSpace(u.Host)
	if encoded == "" {
		encoded = strings.TrimPrefix(strings.TrimSpace(u.Opaque), "//")
	}
	if encoded == "" {
		return nil, false
	}

	decoded, err := decodeAdaptiveBase64(encoded)
	if err != nil || !strings.Contains(decoded, "@") {
		return nil, false
	}

	rebuilt := "ss://" + decoded
	if u.RawQuery != "" {
		rebuilt += "?" + u.RawQuery
	}
	if u.RawFragment != "" {
		rebuilt += "#" + u.RawFragment
	}
	legacyURL, err := url.Parse(rebuilt)
	return legacyURL, err == nil
}

func parseSocks5(u *url.URL) (*ParsedProxyResult, error) {
	host := u.Hostname()
	portStr := u.Port()
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid port: %s", portStr)
	}

	name := u.Fragment
	if name == "" {
		name = host
	}
	if unescaped, err := url.PathUnescape(name); err == nil {
		name = unescaped
	}

	config := map[string]interface{}{
		"name":   name,
		"type":   "socks5",
		"server": host,
		"port":   port,
	}

	if u.User != nil {
		username := u.User.Username()
		password, hasPassword := u.User.Password()

		// 尝试处理 base64 编码的 userInfo
		if !hasPassword && !strings.Contains(username, ":") {
			decoded, err := decodeAdaptiveBase64(username)
			if err == nil && strings.Contains(decoded, ":") {
				parts := strings.SplitN(decoded, ":", 2)
				username = parts[0]
				password = parts[1]
				hasPassword = true
			}
		}

		if username != "" {
			config["username"] = username
		}
		if hasPassword {
			config["password"] = password
		}
	}

	return &ParsedProxyResult{
		Name:   name,
		Type:   "socks5",
		Server: host,
		Port:   port,
		Config: config,
	}, nil
}

func firstNonEmptyQuery(values url.Values, keys ...string) string {
	for _, key := range keys {
		if value := values.Get(key); value != "" {
			return value
		}
	}
	return ""
}

func truthyString(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

// normalizeSocksLink 尝试修正没有 @ 分隔符的非标准 socks 链接，如 socks5://ip:port:user:pass
func normalizeSocksLink(link string) string {
	lower := strings.ToLower(link)
	prefix := ""
	if strings.HasPrefix(lower, "socks5://") {
		prefix = link[:9]
	} else if strings.HasPrefix(lower, "socks://") {
		prefix = link[:8]
	} else {
		return link
	}

	content := link[len(prefix):]

	// 暂存 fragment 和 query 以便后续拼回
	nameSuffix := ""
	if idx := strings.Index(content, "#"); idx != -1 {
		nameSuffix = content[idx:]
		content = content[:idx]
	}

	querySuffix := ""
	if idx := strings.Index(content, "?"); idx != -1 {
		querySuffix = content[idx:]
		content = content[:idx]
	}

	// 如果没有包含标准的 '@'，且含有2个以上的冒号，则很可能是 ip:port:user:pass 结构
	if !strings.Contains(content, "@") {
		parts := strings.Split(content, ":")
		if len(parts) >= 3 {
			ip := parts[0]
			port := parts[1]
			user := parts[2]
			pass := ""
			if len(parts) >= 4 {
				pass = strings.Join(parts[3:], ":") // 密码中可能含有冒号
			}

			if pass != "" {
				return prefix + user + ":" + pass + "@" + ip + ":" + port + querySuffix + nameSuffix
			}
			return prefix + user + "@" + ip + ":" + port + querySuffix + nameSuffix
		}
	}

	return link
}
