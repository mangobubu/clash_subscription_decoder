package main

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

// DecodeRequest 定义解码请求的参数结构
type DecodeRequest struct {
	URL string `json:"url" binding:"required"`
}

// DecodeResponse 定义解码成功的响应结构
type DecodeResponse struct {
	URL       string `json:"url"`
	RawBase64 string `json:"raw_base64"`
	Decoded   string `json:"decoded"`
}

func main() {
	// 初始化数据库
	initDB()

	// 设置为发布模式或开发模式，这里使用默认的运行模式
	r := gin.Default()

	// 注册跨域中间件
	r.Use(CORSMiddleware())

	// 注册解码接口
	r.POST("/api/decode", handleDecode)
	
	// 注册解析节点链接接口
	r.POST("/api/parse-link", handleParseLink)

	// 注册自定义节点 CRUD 接口
	r.GET("/api/custom-nodes", handleGetCustomNodes)
	r.POST("/api/custom-nodes", handleCreateCustomNode)
	r.DELETE("/api/custom-nodes/:id", handleDeleteCustomNode)

	// 注册自定义组 CRUD 接口
	r.GET("/api/custom-groups", handleGetCustomGroups)
	r.POST("/api/custom-groups", handleCreateCustomGroup)

	// 启动服务，默认监听 8080 端口
	log.Println("Starting Clash Proxy Decoder backend on :8080...")
	if err := r.Run(":8080"); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}

// CORSMiddleware 跨域中间件
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// handleDecode 处理获取并解码请求
func handleDecode(c *gin.Context) {
	var req DecodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "参数格式错误，请提供有效的 url 字段",
			"error":   err.Error(),
		})
		return
	}

	targetURL := strings.TrimSpace(req.URL)
	
	// 极致清洗 URL，只保留可见的合法 ASCII 字符，彻底消灭零宽空格 \u200b 等隐形杀手！
	var cleanURL strings.Builder
	for _, r := range targetURL {
		if r > 32 && r < 127 {
			cleanURL.WriteRune(r)
		}
	}
	targetURL = cleanURL.String()

	if targetURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "地址不能为空",
		})
		return
	}

	// 自动补齐协议头（如果没有输入的话）
	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		targetURL = "https://" + targetURL
	}

	// 1. 发起请求获取远端 Base64 内容
	rawBase64, err := fetchURLContent(targetURL)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"code":    502,
			"message": "获取目标地址内容失败，请检查网络或地址是否正确",
			"error":   err.Error(),
		})
		return
	}

	// 2. 尝试对内容进行健壮的 Base64 解码，如果是纯文本 YAML 则自动回退接纳
	var decodedContent string
	decoded, err := decodeAdaptiveBase64(rawBase64)
	if err != nil {
		// 也许它本身就是明文的配置文件 (如 YAML)? 我们判断一下有没有常见的配置特征 (KISS 原则: 灵活兼容)
		if strings.Contains(rawBase64, "proxies:") || strings.Contains(rawBase64, "outbounds:") || strings.Contains(rawBase64, "servers:") {
			decodedContent = rawBase64
		} else {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"code":    422,
				"message": "解析 Base64 失败，且不包含常见配置文件特征，目标地址返回的内容格式不支持",
				"error":   err.Error(),
				"preview": truncateString(rawBase64, 200), // 提供前200个字符用于排错
			})
			return
		}
	} else {
		decodedContent = decoded
	}

	// 智能明文转换：由于很多节点配置（如 vmess://, hysteria2://）为了防乱码会把节点名称进行 URL 编码（如 %E5%89...），
	// 这里我们对每一行进行 URL 解析，让前端拿到最纯净、最直观的明文。
	lines := strings.Split(decodedContent, "\n")
	for i, line := range lines {
		// 为了防止把原本合法的 + 错误转为空格，这里使用更稳妥的局部替换策略。
		// 大多数节点编码都集中在节点名 # 后面，也可以直接整体 unquote
		if unescaped, err := url.PathUnescape(strings.TrimSpace(line)); err == nil {
			lines[i] = unescaped
		}
	}
	decodedContent = strings.Join(lines, "\n")
	
	// 🌟 AST 无损注入逻辑：自动将数据库中的自定义节点注入到 proxies 中
	decodedContent = injectCustomNodes(decodedContent)

	// 🌟 AST 无损注入逻辑：自动将数据库中的自定义组注入到配置的 proxy-groups 中
	decodedContent = injectCustomGroups(decodedContent)

	// 3. 成功返回
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "success",
		"data": DecodeResponse{
			URL:       targetURL,
			RawBase64: truncateString(rawBase64, 1000), // 限制原始 Base64 返回长度，避免传输体过大
			Decoded:   decodedContent,
		},
	})
}

// handleGetCustomGroups 获取所有自定义策略组
func handleGetCustomGroups(c *gin.Context) {
	groups, err := GetCustomProxyGroups()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "获取失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": groups})
}

// handleCreateCustomGroup 创建新策略组
func handleCreateCustomGroup(c *gin.Context) {
	var req struct {
		Name    string   `json:"name"`
		Type    string   `json:"type"`
		Proxies []string `json:"proxies"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}

	proxiesBytes, _ := json.Marshal(req.Proxies)
	group := CustomProxyGroup{
		Name:    req.Name,
		Type:    req.Type,
		Proxies: string(proxiesBytes),
	}
	if err := DB.Create(&group).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "保存失败", "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "保存成功", "data": group})
}

// handleParseLink 处理节点链接解析
func handleParseLink(c *gin.Context) {
	var req struct {
		Link string `json:"link" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "缺少 link 参数"})
		return
	}

	result, err := ParseProxyLink(req.Link)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "解析失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "解析成功", "data": result})
}

// handleGetCustomNodes 获取所有自定义节点
func handleGetCustomNodes(c *gin.Context) {
	nodes, err := GetCustomNodes()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "获取失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": nodes})
}

// handleCreateCustomNode 创建或保存自定义节点
func handleCreateCustomNode(c *gin.Context) {
	var req struct {
		Name   string                 `json:"name"`
		Type   string                 `json:"type"`
		Server string                 `json:"server"`
		Port   int                    `json:"port"`
		Config map[string]interface{} `json:"config"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}

	configBytes, _ := json.Marshal(req.Config)
	node := CustomNode{
		Name:   req.Name,
		Type:   req.Type,
		Server: req.Server,
		Port:   req.Port,
		Config: string(configBytes),
	}
	if err := DB.Create(&node).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "保存失败", "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "保存成功", "data": node})
}

// handleDeleteCustomNode 删除自定义节点
func handleDeleteCustomNode(c *gin.Context) {
	id := c.Param("id")
	if err := DB.Delete(&CustomNode{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "删除失败", "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "删除成功"})
}

// injectCustomNodes 基于 yaml.v3 的 Node 树完成自定义节点的注入
func injectCustomNodes(yamlContent string) string {
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(yamlContent), &root); err != nil {
		log.Printf("YAML unmarshal warning in injectCustomNodes: %v", err)
		return yamlContent
	}

	if len(root.Content) == 0 {
		return yamlContent
	}
	docNode := root.Content[0]
	if docNode.Kind != yaml.MappingNode {
		return yamlContent
	}

	// 寻找 proxies 节点
	var proxiesNode *yaml.Node
	for i := 0; i < len(docNode.Content); i += 2 {
		if docNode.Content[i].Value == "proxies" {
			proxiesNode = docNode.Content[i+1]
			break
		}
	}

	// 如果没有 proxies 节点，创建一个新的并加入 docNode
	if proxiesNode == nil {
		proxiesNode = &yaml.Node{Kind: yaml.SequenceNode}
		docNode.Content = append(docNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "proxies"},
			proxiesNode,
		)
	} else if proxiesNode.Kind != yaml.SequenceNode {
		return yamlContent
	}

	// 获取数据库里的自定义节点并无损注入
	customNodes, _ := GetCustomNodes()
	for _, cn := range customNodes {
		var configMap map[string]interface{}
		if err := json.Unmarshal([]byte(cn.Config), &configMap); err != nil {
			log.Printf("Failed to unmarshal custom node config: %v", err)
			continue
		}

		// 将 configMap 转换回 YAML 节点
		configYAML, err := yaml.Marshal(configMap)
		if err != nil {
			log.Printf("Failed to marshal config back to yaml: %v", err)
			continue
		}

		var tempRoot yaml.Node
		if err := yaml.Unmarshal(configYAML, &tempRoot); err != nil {
			log.Printf("Failed to unmarshal temp config back to node: %v", err)
			continue
		}
		
		if len(tempRoot.Content) > 0 && len(tempRoot.Content[0].Content) > 0 {
			// 将生成的节点对象注入到 proxiesNode 的最开头，使其显示在最前面
			proxiesNode.Content = append([]*yaml.Node{tempRoot.Content[0]}, proxiesNode.Content...)
		}
	}

	// 转回 YAML 字符串
	out, err := yaml.Marshal(&root)
	if err != nil {
		log.Printf("YAML marshal warning in injectCustomNodes: %v", err)
		return yamlContent
	}
	return string(out)
}

// injectCustomGroups 基于 yaml.v3 的 Node 树完成零注释丢失的无损注入
func injectCustomGroups(yamlContent string) string {
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(yamlContent), &root); err != nil {
		log.Printf("YAML unmarshal warning: %v", err)
		return yamlContent
	}

	if len(root.Content) == 0 {
		return yamlContent
	}
	docNode := root.Content[0]
	if docNode.Kind != yaml.MappingNode {
		return yamlContent
	}

	// 提取所有的 proxies 名称以备 "ALL_NODES" 使用
	var allNodeNames []string
	for i := 0; i < len(docNode.Content); i += 2 {
		if docNode.Content[i].Value == "proxies" {
			seq := docNode.Content[i+1]
			if seq.Kind == yaml.SequenceNode {
				for _, proxyNode := range seq.Content {
					if proxyNode.Kind == yaml.MappingNode {
						for j := 0; j < len(proxyNode.Content); j += 2 {
							if proxyNode.Content[j].Value == "name" {
								allNodeNames = append(allNodeNames, proxyNode.Content[j+1].Value)
								break
							}
						}
					}
				}
			}
			break
		}
	}

	// 寻找 proxy-groups 节点
	var proxyGroupsNode *yaml.Node
	for i := 0; i < len(docNode.Content); i += 2 {
		if docNode.Content[i].Value == "proxy-groups" {
			proxyGroupsNode = docNode.Content[i+1]
			break
		}
	}

	if proxyGroupsNode == nil || proxyGroupsNode.Kind != yaml.SequenceNode {
		return yamlContent
	}

	// 获取数据库里的自定义组并无损注入
	customGroups, _ := GetCustomProxyGroups()
	for _, cg := range customGroups {
		proxiesList := cg.GetProxiesList()

		var finalProxies []string
		for _, p := range proxiesList {
			if p == "[ALL_NODES]" {
				finalProxies = append(finalProxies, allNodeNames...)
			} else {
				finalProxies = append(finalProxies, p)
			}
		}

		proxiesSeq := &yaml.Node{Kind: yaml.SequenceNode}
		for _, p := range finalProxies {
			proxiesSeq.Content = append(proxiesSeq.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: p})
		}

		groupMap := &yaml.Node{Kind: yaml.MappingNode}
		groupMap.Content = append(groupMap.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "name"},
			&yaml.Node{Kind: yaml.ScalarNode, Value: cg.Name},
			&yaml.Node{Kind: yaml.ScalarNode, Value: "type"},
			&yaml.Node{Kind: yaml.ScalarNode, Value: cg.Type},
			&yaml.Node{Kind: yaml.ScalarNode, Value: "proxies"},
			proxiesSeq,
		)

		proxyGroupsNode.Content = append(proxyGroupsNode.Content, groupMap)
	}

	// 转回 YAML 字符串（保持原格式和注释）
	out, err := yaml.Marshal(&root)
	if err != nil {
		log.Printf("YAML marshal warning: %v", err)
		return yamlContent
	}
	return string(out)
}

// fetchURLContent 获取指定 URL 的文本内容，支持内置测试 mock 地址拦截与多重高仿真抓取策略
func fetchURLContent(targetURL string) (string, error) {
	// 针对本地 Mock URL 实施拦截，提供零成本快速体验
	if strings.Contains(targetURL, "mock.clash.local/sub") {
		mockYAML := `port: 7890
socks-port: 7891
allow-lan: true
mode: Rule
log-level: info
proxies:
  - name: "香港 01 [IPL]"
    type: ss
    server: 43.156.22.44
    port: 10086
    cipher: aes-256-gcm
    password: "mock-hk-password-strong"
  - name: "新加坡 02 [BGP]"
    type: vmess
    server: 128.199.204.11
    port: 443
    uuid: "88888888-4444-4444-4444-121212121212"
    alterId: 0
    cipher: auto
    tls: true
  - name: "东京 03 [CN2]"
    type: trojan
    server: 210.140.10.99
    port: 443
    password: "mock-trojan-pass-key"
    sni: "jp-tokyo.cn2-service.net"
  - name: "美国 04 [GIA]"
    type: ss
    server: 198.51.100.5
    port: 8888
    cipher: chacha20-ietf-poly1305
    password: "mock-us-password-strong"
rules:
  - DOMAIN-SUFFIX,google.com,Proxy
  - DOMAIN-SUFFIX,github.com,Proxy
  - DOMAIN-KEYWORD,clash,Proxy
  - GEOIP,CN,DIRECT
  - MATCH,Proxy`
		return base64.StdEncoding.EncodeToString([]byte(mockYAML)), nil
	}

	// 极其关键：继承并读取系统全局代理，并忽略自签名证书与 IP 证书的 TLS 校验限制，保证与 Apifox 使用同一网络通道！
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment, // 自动读取并遵循全局系统代理！
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
			Renegotiation:      tls.RenegotiateFreelyAsClient, // 关键破局：允许客户端进行 TLS 重新协商，完美契合服务端双向 TLS 握手！
		},
	}
	client := &http.Client{
		Timeout:   15 * time.Second,
		Transport: tr,
	}

	// 定义三种高拟真自适应策略，攻破各种防火墙和反代限制 (KISS + DRY 原则)
	// 策略 1: 纯浏览器 UA + 默认 Host (带端口)
	// 策略 2: Clash UA + 默认 Host (带端口)
	// 策略 3: 纯浏览器 UA + 剥离端口号的 Host (防 Nginx/Laravel 反代 Host 匹配错误)
	type Strategy struct {
		Name        string
		UA          string
		UseBareHost bool
		Minimal     bool
	}

	browserUA := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	
	strategies := []Strategy{
		// 策略 0: 标准 Clash 原生请求头（优先级最高！因为这会诱导订阅转换面板直接吐出完整的 YAML 配置文件，而不是 Base64 节点列表）
		{Name: "Clash原生", UA: "clash", UseBareHost: false, Minimal: true},
		// 策略 1: 极其精简的 curl 模拟（如果 clash 被墙或面板强制下发 Base64，我们退而求其次）
		{Name: "Curl原生", UA: "curl/7.88.1", UseBareHost: false, Minimal: true},
		// 策略 2: v2rayN 原生请求头
		{Name: "v2rayN原生", UA: "v2rayN/6.23", UseBareHost: false, Minimal: true},
		// 策略 3: 浏览器高拟真伪装
		{Name: "Chrome高拟真", UA: browserUA, UseBareHost: false, Minimal: false},
		// 策略 4: 浏览器伪装 + 剥离端口（针对特定 Nginx 反代）
		{Name: "Chrome剥离端口", UA: browserUA, UseBareHost: true, Minimal: false},
	}

	var lastErr error
	for i, strategy := range strategies {
		req, err := http.NewRequest("GET", targetURL, nil)
		if err != nil {
			return "", fmt.Errorf("创建请求失败: %w", err)
		}

		if strategy.Minimal {
			// 极简模式：只发送 UA 和 Accept，完美模拟 CLI 工具
			req.Header.Set("User-Agent", strategy.UA)
			req.Header.Set("Accept", "*/*")
		} else {
			// 注入全套高仿真 Chrome 浏览器 Header 与 Sec-Metadata，完美破解 Laravel/Nginx 防爬虫的 404/403 路由屏蔽机制
			req.Header.Set("Connection", "keep-alive")
			req.Header.Set("Upgrade-Insecure-Requests", "1")
			req.Header.Set("User-Agent", strategy.UA)
			req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
			req.Header.Set("Sec-Fetch-Site", "none")
			req.Header.Set("Sec-Fetch-Mode", "navigate")
			req.Header.Set("Sec-Fetch-User", "?1")
			req.Header.Set("Sec-Fetch-Dest", "document")
			req.Header.Set("Sec-Ch-Ua", `"Not_A Brand";v="8", "Chromium";v="120", "Google Chrome";v="120"`)
			req.Header.Set("Sec-Ch-Ua-Mobile", "?0")
			req.Header.Set("Sec-Ch-Ua-Platform", `"Windows"`)
			req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
			req.Header.Set("Cache-Control", "no-cache")
		}

		// 如果开启了 UseBareHost，将 Host 请求头重设为剥离了非标准端口（如 :1000）的主机名，防止 Web 服务因端口名匹配不到虚拟主机而 404
		if strategy.UseBareHost {
			host := req.URL.Host
			if pos := strings.Index(host, ":"); pos != -1 {
				req.Host = host[:pos]
			} else {
				req.Host = host
			}
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("[策略: %s] 请求网络失败: %w", strategy.Name, err)
			log.Printf("[Clash-Proxy 后端诊断] 尝试 %d (%s) 失败，请求网络出错: %v", i+1, strategy.Name, err)
			continue
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		// 极其关键的控制台实时排错日志 (AIR 中直观展示)
		log.Printf("=================== [Clash-Proxy 诊断请求 %d - %s] ===================", i+1, strategy.Name)
		log.Printf("请求 URL: %s", targetURL)
		log.Printf("请求 UA  : %s", strategy.UA)
		log.Printf("请求 Host: %s (原 URL.Host: %s)", req.Host, req.URL.Host)
		log.Printf("响应状态码: %d", resp.StatusCode)
		log.Printf("响应 Headers: %v", resp.Header)
		log.Printf("响应 Body 预览 (前300字): %s", truncateString(string(bodyBytes), 300))
		log.Printf("==========================================================================")

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("[策略: %s] 服务器返回状态码 %d (内容: %s)", 
				strategy.Name, resp.StatusCode, truncateString(string(bodyBytes), 80))
			continue
		}

		if err != nil {
			lastErr = fmt.Errorf("[策略: %s] 读取响应体失败: %w", strategy.Name, err)
			continue
		}

		// 成功直接返回
		return string(bodyBytes), nil
	}

	return "", fmt.Errorf("多重自适应策略抓取均告失败。最后报错: %w", lastErr)
}

// 辅助函数，用于截断 UA 输出便于排错
func truncateUA(ua string, limit int) string {
	if len(ua) <= limit {
		return ua
	}
	return ua[:limit] + "..."
}

// decodeAdaptiveBase64 强健的自适应 Base64 解码函数
func decodeAdaptiveBase64(input string) (string, error) {
	// 1. 去除所有空白字符（空格、换行符 \r \n、制表符等）
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, input)

	if len(cleaned) == 0 {
		return "", fmt.Errorf("输入内容为空")
	}

	// 2. 自适应补齐 '=' 填充字符以满足 4 的整数倍长度要求
	if len(cleaned)%4 != 0 {
		cleaned += strings.Repeat("=", 4-(len(cleaned)%4))
	}

	// 3. 尝试标准 Base64 解码
	if decodedBytes, err := base64.StdEncoding.DecodeString(cleaned); err == nil {
		return string(decodedBytes), nil
	}

	// 4. 尝试 URL Safe Base64 解码
	if decodedBytes, err := base64.URLEncoding.DecodeString(cleaned); err == nil {
		return string(decodedBytes), nil
	}

	// 5. 尝试 Raw Standard Base64 解码 (无填充)
	if decodedBytes, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(cleaned, "=")); err == nil {
		return string(decodedBytes), nil
	}

	// 6. 尝试 Raw URL Safe Base64 解码 (无填充)
	if decodedBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(cleaned, "=")); err == nil {
		return string(decodedBytes), nil
	}

	return "", fmt.Errorf("无法使用任何标准 Base64 编码方式解码该文本")
}

// truncateString 截断字符串，防止返回大文件时撑爆网络通道
func truncateString(str string, length int) string {
	if len(str) <= length {
		return str
	}
	return str[:length] + "...(已截断)"
}
