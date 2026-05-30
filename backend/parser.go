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
	case "ss":
		return parseSS(u)
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

	return &ParsedProxyResult{
		Name:   name,
		Type:   "ss",
		Server: host,
		Port:   port,
		Config: config,
	}, nil
}
