//go:generate go run ../build.go

package main

import (
	"crypto/tls"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/mojocn/base64Captcha"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

//go:embed dist
var frontendFS embed.FS

// DecodeRequest 定义解码请求的参数结构
type DecodeRequest struct {
	URL       string `json:"url" binding:"required"`
	ProfileID uint   `json:"profile_id"`
}

// DecodeResponse 定义解码成功的响应结构
type DecodeResponse struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	SourceType  string `json:"source_type"`
	URL         string `json:"url"`
	RawResponse string `json:"raw_response"`
	Decoded     string `json:"decoded"`
	UpdatedAt   int64  `json:"updated_at"`
}

func main() {
	// 初始化数据库
	initDB()

	// 设置为发布模式或开发模式，这里使用默认的运行模式
	r := gin.Default()

	// 注册跨域中间件
	r.Use(CORSMiddleware())

	// 注册公开接口
	r.GET("/api/check-init", handleCheckInit)
	r.POST("/api/init", handleInit)
	r.GET("/api/captcha", handleCaptcha)
	r.POST("/api/login", handleLogin)
	r.GET("/sub", handleFinalSubscription)                      // 最终订阅地址
	r.GET("/shadowrocket.conf", handleShadowrocketSubscription) // Shadowrocket 配置文件地址
	r.GET("/surge.conf", handleSurgeSubscription)               // Surge 最新版配置文件地址
	r.GET("/surge-5.7.6.conf", handleSurge576Subscription)      // Surge 5.7.6 兼容配置文件地址
	r.GET("/shadowrocket/config/:tokenFile", handleShadowrocketPathSubscription)
	r.GET("/shadowrocket/install", handleShadowrocketInstall) // Shadowrocket 一键安装桥接页

	// 需要认证的接口组
	api := r.Group("/api")
	api.Use(AuthMiddleware())

	// 注册验证接口
	api.GET("/verify", handleVerify)
	api.POST("/logout", handleLogout)
	api.POST("/change-password", handleChangePassword)
	api.GET("/sub-token", handleGetSubToken)
	api.POST("/generate-sub-token", handleGenerateSubToken)

	// 数据备份与导入恢复
	api.GET("/backup", handleBackup)
	api.POST("/import", handleImport)

	// 注册解码接口
	api.POST("/decode", handleDecode)

	// 注册多配置与获取订阅接口
	api.GET("/profiles", handleListProfiles)
	api.POST("/profiles", handleCreateProfile)
	api.PUT("/profiles/:id", handleUpdateProfile)
	api.DELETE("/profiles/:id", handleDeleteProfile)
	api.POST("/profiles/:id/refresh", handleRefreshProfile)
	api.GET("/profiles/:id/subscription", handleGetProfileSubscription)
	api.GET("/profiles/:id/sub-token", handleGetProfileSubToken)
	api.POST("/profiles/:id/generate-sub-token", handleGenerateProfileSubToken)
	api.POST("/profiles/:id/copy-rules", handleCopyProfileRules)
	api.POST("/profiles/:id/copy-groups", handleCopyProfileGroups)
	api.POST("/profiles/:id/localize-rules", handleLocalizeProfileRules)
	api.GET("/subscription", handleGetSubscription)
	api.PUT("/resource-orders", handleUpdateResourceOrder)
	api.POST("/subscription-resources/takeover", handleTakeoverSubscriptionResource)
	api.POST("/subscription-resources/delete", handleDeleteSubscriptionResource)

	// 注册解析节点链接接口
	api.POST("/parse-link", handleParseLink)

	// 注册自定义节点 CRUD 接口
	api.GET("/custom-nodes", handleGetCustomNodes)
	api.POST("/custom-nodes", handleCreateCustomNode)
	api.PUT("/custom-nodes/:id", handleUpdateCustomNode)
	api.DELETE("/custom-nodes/:id", handleDeleteCustomNode)

	// 注册自定义组 CRUD 接口
	api.GET("/custom-groups", handleGetCustomGroups)
	api.POST("/custom-groups", handleCreateCustomGroup)
	api.PUT("/custom-groups/:id", handleUpdateCustomGroup)
	api.DELETE("/custom-groups/:id", handleDeleteCustomGroup)

	// 注册自定义分流规则 CRUD 接口
	api.GET("/custom-rules", handleGetCustomRules)
	api.POST("/custom-rules", handleCreateCustomRule)
	api.POST("/custom-rules/batch", handleBatchSaveCustomRules)
	api.POST("/custom-rules/batch/stream", handleBatchSaveCustomRulesStream)
	api.POST("/custom-rules/batch-delete", handleBatchDeleteCustomRules)
	api.PUT("/custom-rules/:id", handleUpdateCustomRule)
	api.DELETE("/custom-rules/:id", handleDeleteCustomRule)

	// 提取嵌入的 dist 目录
	subFS, err := fs.Sub(frontendFS, "dist")
	if err != nil {
		log.Fatalf("Failed to sub-embed frontend files: %v", err)
	}

	// 注册 SPA 静态资源路由与 Fallback 处理
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path

		// 如果是后端 API 或配置订阅请求但没匹配到，走 404 处理，不要 fallback 返回 index.html
		if isBackendOnlyPath(path) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Not Found"})
			return
		}

		// 检查 embed 中是否存在该请求的文件
		filePath := strings.TrimPrefix(path, "/")
		if filePath == "" {
			filePath = "index.html"
		}

		_, err := subFS.Open(filePath)
		if err == nil {
			// 文件存在，直接交给 http.FileServer 处理
			http.FileServer(http.FS(subFS)).ServeHTTP(c.Writer, c.Request)
			return
		}

		// 如果文件不存在，执行 SPA 路由的 Fallback (返回 index.html)
		indexFile, err := subFS.Open("index.html")
		if err != nil {
			c.String(http.StatusNotFound, "Embedded frontend assets not found or index.html missing")
			return
		}
		defer indexFile.Close()

		content, err := io.ReadAll(indexFile)
		if err != nil {
			c.String(http.StatusInternalServerError, "Internal Server Error")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", content)
	})

	// 启动服务，端口由 config.toml 的 [server].port 或 SERVER_PORT 环境变量控制
	serverAddress := fmt.Sprintf(":%d", AppConfig.Server.Port)
	log.Printf("Starting Clash Proxy Decoder backend on %s...", serverAddress)
	if err := r.Run(serverAddress); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}

func isBackendOnlyPath(path string) bool {
	return strings.HasPrefix(path, "/api") ||
		strings.HasPrefix(path, "/sub") ||
		strings.HasPrefix(path, "/surge.conf") ||
		strings.HasPrefix(path, "/surge-5.7.6.conf") ||
		strings.HasPrefix(path, "/shadowrocket.conf") ||
		strings.HasPrefix(path, "/shadowrocket")
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

// ---- Auth 相关逻辑 ----
var SessionTokens sync.Map
var captchaStore = base64Captcha.DefaultMemStore

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "未授权的访问"})
			return
		}
		token := strings.TrimPrefix(authHeader, "Bearer ")
		usernameVal, ok := SessionTokens.Load(token)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "无效或已过期的凭证"})
			return
		}
		// 将当前登录的用户名保存至上下文，方便后续接口获取
		c.Set("username", usernameVal.(string))
		c.Next()
	}
}

func handleVerify(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "Token valid"})
}

func handleCheckInit(c *gin.Context) {
	var count int64
	DB.Model(&User{}).Count(&count)
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"need_init": count == 0,
		},
	})
}

type InitRequest struct {
	Username     string `json:"username" binding:"required"`
	Password     string `json:"password" binding:"required"`
	CaptchaId    string `json:"captcha_id"`
	CaptchaValue string `json:"captcha_value"`
}

func handleInit(c *gin.Context) {
	var count int64
	DB.Model(&User{}).Count(&count)
	if count > 0 {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "系统已初始化，禁止重复操作"})
		return
	}

	var req InitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}

	if AppConfig.Auth.CaptchaEnabled {
		if req.CaptchaId == "" || req.CaptchaValue == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "请输入验证码"})
			return
		}
		if !captchaStore.Verify(req.CaptchaId, req.CaptchaValue, true) {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "验证码错误"})
			return
		}
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "密码加密失败"})
		return
	}

	user := User{
		Username: req.Username,
		Password: string(hashedPassword),
	}
	if err := DB.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "创建管理员失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "管理员创建成功"})
}

func handleCaptcha(c *gin.Context) {
	if !AppConfig.Auth.CaptchaEnabled {
		c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{"enabled": false}})
		return
	}

	cp := base64Captcha.NewCaptcha(newConfiguredCaptchaDriver(), captchaStore)
	id, b64s, _, err := cp.Generate()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "生成验证码失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"enabled":    true,
			"captcha_id": id,
			"b64s":       b64s,
			"text_color": AppConfig.Auth.CaptchaTextColor,
		},
	})
}

type LoginRequest struct {
	Username     string `json:"username" binding:"required"`
	Password     string `json:"password" binding:"required"`
	CaptchaId    string `json:"captcha_id"`
	CaptchaValue string `json:"captcha_value"`
}

func handleLogin(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}

	if AppConfig.Auth.CaptchaEnabled {
		if req.CaptchaId == "" || req.CaptchaValue == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "请输入验证码"})
			return
		}
		if !captchaStore.Verify(req.CaptchaId, req.CaptchaValue, true) {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "验证码错误"})
			return
		}
	}

	var user User
	if err := DB.Where("username = ?", req.Username).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "用户名或密码错误"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "用户名或密码错误"})
		return
	}

	token := uuid.New().String()
	SessionTokens.Store(token, user.Username)

	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "登录成功", "token": token})
}

func handleLogout(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")
	if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		SessionTokens.Delete(token)
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "已成功退出登录"})
}

type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required"`
}

func handleChangePassword(c *gin.Context) {
	username, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "未登录"})
		return
	}

	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}

	var user User
	if err := DB.Where("username = ?", username.(string)).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "用户不存在"})
		return
	}

	// 验证旧密码是否正确
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.OldPassword)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "原密码不正确"})
		return
	}

	// 加密并保存新密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "密码加密失败"})
		return
	}

	user.Password = string(hashedPassword)
	if err := DB.Save(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "保存密码失败"})
		return
	}

	// 清理该用户的所有活跃 Session 使得所有设备强制重新登录
	SessionTokens.Range(func(key, value interface{}) bool {
		if valStr, ok := value.(string); ok && valStr == user.Username {
			SessionTokens.Delete(key)
		}
		return true
	})

	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "密码修改成功，请使用新密码重新登录"})
}

type profileWriteRequest struct {
	Name         string `json:"name" binding:"required"`
	SourceType   string `json:"source_type" binding:"required"`
	URL          string `json:"url"`
	LocalContent string `json:"local_content"`
}

type copyProfileRulesRequest struct {
	SourceProfileID uint `json:"source_profile_id" binding:"required"`
}

func normalizeProfileSourceType(sourceType string) string {
	sourceType = strings.ToLower(strings.TrimSpace(sourceType))
	if sourceType == profileSourceLocal {
		return profileSourceLocal
	}
	return profileSourceRemote
}

func sanitizeVisibleASCII(input string) string {
	var clean strings.Builder
	for _, r := range strings.TrimSpace(input) {
		if r > 32 && r < 127 {
			clean.WriteRune(r)
		}
	}
	return clean.String()
}

func normalizeSubscriptionURL(input string) (string, error) {
	targetURL := sanitizeVisibleASCII(input)
	if targetURL == "" {
		return "", fmt.Errorf("地址不能为空")
	}
	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		targetURL = "https://" + targetURL
	}
	return targetURL, nil
}

func getDefaultProfile() (SubscriptionProfile, error) {
	var profile SubscriptionProfile
	err := DB.Order("id asc").First(&profile).Error
	return profile, err
}

func getProfileFromParam(c *gin.Context) (SubscriptionProfile, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "配置 ID 不合法"})
		return SubscriptionProfile{}, false
	}

	var profile SubscriptionProfile
	if err := DB.First(&profile, uint(id)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "配置不存在"})
		return SubscriptionProfile{}, false
	}
	return profile, true
}

func resolveRequestProfileID(c *gin.Context, bodyProfileID uint) (uint, bool) {
	if bodyProfileID > 0 {
		return bodyProfileID, true
	}
	if queryID := strings.TrimSpace(c.Query("profile_id")); queryID != "" {
		id, err := strconv.ParseUint(queryID, 10, 64)
		if err != nil || id == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "配置 ID 不合法"})
			return 0, false
		}
		return uint(id), true
	}

	profile, err := getDefaultProfile()
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "暂无可用配置，请先创建配置"})
		return 0, false
	}
	return profile.ID, true
}

func decodeResponseFromProfile(profile SubscriptionProfile) DecodeResponse {
	return DecodeResponse{
		ID:          profile.ID,
		Name:        profile.Name,
		SourceType:  profile.SourceType,
		URL:         profile.URL,
		RawResponse: truncateString(profile.RawResponse, 1000),
		Decoded:     profile.Decoded,
		UpdatedAt:   profile.UpdatedAt,
	}
}

func profileListItem(profile SubscriptionProfile) gin.H {
	localContent := profile.LocalContent
	if normalizeProfileSourceType(profile.SourceType) == profileSourceLocal {
		localContent = ""
	}
	return gin.H{
		"id":            profile.ID,
		"name":          profile.Name,
		"source_type":   profile.SourceType,
		"url":           profile.URL,
		"local_content": localContent,
		"has_token":     profile.SubToken != "",
		"created_at":    profile.CreatedAt,
		"updated_at":    profile.UpdatedAt,
	}
}

func validateProfileWriteRequest(req profileWriteRequest) (profileWriteRequest, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.SourceType = normalizeProfileSourceType(req.SourceType)
	req.URL = strings.TrimSpace(req.URL)
	if req.Name == "" {
		return req, fmt.Errorf("配置名称不能为空")
	}
	if req.SourceType == profileSourceRemote {
		targetURL, err := normalizeSubscriptionURL(req.URL)
		if err != nil {
			return req, err
		}
		req.URL = targetURL
		req.LocalContent = ""
		return req, nil
	}
	req.URL = ""
	req.LocalContent = ""
	return req, nil
}

type profileContentFetcher func(string) (string, error)

func loadProfileRawContent(profile SubscriptionProfile, fetcher profileContentFetcher) (string, bool, error) {
	if normalizeProfileSourceType(profile.SourceType) == profileSourceLocal {
		return "", false, fmt.Errorf("本地手动配置不读取 YAML 内容，请通过手动节点、策略组和规则生成")
	}

	targetURL := strings.TrimSpace(profile.URL)
	if targetURL == "" {
		return "", true, fmt.Errorf("订阅地址为空")
	}
	rawResponse, err := fetcher(targetURL)
	return rawResponse, true, err
}

func looksLikePlainSubscriptionConfig(content string) bool {
	return strings.Contains(content, "proxies:") ||
		strings.Contains(content, "proxy-groups:") ||
		strings.Contains(content, "proxy-providers:") ||
		strings.Contains(content, "rules:") ||
		strings.Contains(content, "outbounds:") ||
		strings.Contains(content, "servers:")
}

type manualProfileConfig struct {
	MixedPort   int                      `yaml:"mixed-port"`
	AllowLan    bool                     `yaml:"allow-lan"`
	Mode        string                   `yaml:"mode"`
	LogLevel    string                   `yaml:"log-level"`
	GeodataMode bool                     `yaml:"geodata-mode"`
	Proxies     []map[string]interface{} `yaml:"proxies"`
	ProxyGroups []manualProxyGroupConfig `yaml:"proxy-groups"`
	Rules       []string                 `yaml:"rules"`
}

type manualProxyGroupConfig struct {
	Name    string   `yaml:"name"`
	Type    string   `yaml:"type"`
	Proxies []string `yaml:"proxies"`
	Extra   string   `yaml:"-"`
}

func (g manualProxyGroupConfig) MarshalYAML() (interface{}, error) {
	groupMap := map[string]interface{}{
		"name":    g.Name,
		"type":    g.Type,
		"proxies": g.Proxies,
	}
	mergeProxyGroupExtraMap(groupMap, g.Extra)
	return groupMap, nil
}

// BuildManualProfileYAML 根据本地配置下手动维护的节点、策略组和规则生成 Clash/Mihomo YAML。
func BuildManualProfileYAML(profileID uint) (string, error) {
	nodes, err := GetCustomNodes(profileID)
	if err != nil {
		return "", fmt.Errorf("读取本地节点失败: %w", err)
	}
	nodes = applyCustomNodeOrder(nodes, loadProfileResourceOrderNames(profileID, resourceOrderTypeNodes))
	groups, err := GetCustomProxyGroups(profileID)
	if err != nil {
		return "", fmt.Errorf("读取本地策略组失败: %w", err)
	}
	groups = applyCustomGroupOrder(groups, loadProfileResourceOrderNames(profileID, resourceOrderTypeGroups))
	rules, err := GetCustomRules(profileID)
	if err != nil {
		return "", fmt.Errorf("读取本地规则失败: %w", err)
	}

	return buildManualProfileYAMLFromResources(nodes, groups, rules)
}

func buildManualProfileYAMLFromResources(nodes []CustomNode, groups []CustomProxyGroup, rules []CustomRule) (string, error) {
	if len(nodes) == 0 {
		return "", fmt.Errorf("本地手动配置至少需要先添加一个节点")
	}

	proxies := make([]map[string]interface{}, 0, len(nodes))
	nodeNames := make([]string, 0, len(nodes))
	for _, node := range nodes {
		var configMap map[string]interface{}
		if err := json.Unmarshal([]byte(node.Config), &configMap); err != nil {
			return "", fmt.Errorf("解析节点 %s 配置失败: %w", node.Name, err)
		}
		if strings.TrimSpace(valueAsString(configMap["name"])) == "" {
			configMap["name"] = node.Name
		}
		if strings.TrimSpace(valueAsString(configMap["type"])) == "" {
			configMap["type"] = node.Type
		}
		if strings.TrimSpace(valueAsString(configMap["server"])) == "" {
			configMap["server"] = node.Server
		}
		if strings.TrimSpace(valueAsString(configMap["port"])) == "" {
			configMap["port"] = node.Port
		}
		proxies = append(proxies, configMap)
		nodeNames = append(nodeNames, valueAsString(configMap["name"]))
	}

	proxyGroups := buildManualProxyGroups(groups, nodeNames, len(rules) == 0)
	ruleLines := buildManualRuleLines(rules)
	cfg := manualProfileConfig{
		MixedPort:   manualDefaultMixedPort,
		AllowLan:    true,
		Mode:        "rule",
		LogLevel:    "info",
		GeodataMode: true,
		Proxies:     proxies,
		ProxyGroups: proxyGroups,
		Rules:       ruleLines,
	}

	out, err := yaml.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("生成本地 YAML 失败: %w", err)
	}
	return string(out), nil
}

func loadProfileResourceOrderNames(profileID uint, resourceType string) []string {
	names, err := GetProfileResourceOrderNames(profileID, resourceType)
	if err != nil {
		log.Printf("load resource order warning: profile=%d type=%s err=%v", profileID, resourceType, err)
		return nil
	}
	return names
}

func applyCustomNodeOrder(nodes []CustomNode, orderNames []string) []CustomNode {
	cleanedOrder := cleanResourceOrderNames(orderNames)
	if len(cleanedOrder) == 0 || len(nodes) == 0 {
		return nodes
	}

	byName := make(map[string][]CustomNode, len(nodes))
	for _, node := range nodes {
		byName[node.Name] = append(byName[node.Name], node)
	}

	ordered := make([]CustomNode, 0, len(nodes))
	usedNames := make(map[string]bool, len(cleanedOrder))
	for _, name := range cleanedOrder {
		if matched, ok := byName[name]; ok {
			ordered = append(ordered, matched...)
			usedNames[name] = true
		}
	}
	for _, node := range nodes {
		if !usedNames[node.Name] {
			ordered = append(ordered, node)
		}
	}
	return ordered
}

func applyCustomGroupOrder(groups []CustomProxyGroup, orderNames []string) []CustomProxyGroup {
	cleanedOrder := cleanResourceOrderNames(orderNames)
	if len(cleanedOrder) == 0 || len(groups) == 0 {
		return groups
	}

	byName := make(map[string][]CustomProxyGroup, len(groups))
	for _, group := range groups {
		byName[group.Name] = append(byName[group.Name], group)
	}

	ordered := make([]CustomProxyGroup, 0, len(groups))
	usedNames := make(map[string]bool, len(cleanedOrder))
	for _, name := range cleanedOrder {
		if matched, ok := byName[name]; ok {
			ordered = append(ordered, matched...)
			usedNames[name] = true
		}
	}
	for _, group := range groups {
		if !usedNames[group.Name] {
			ordered = append(ordered, group)
		}
	}
	return ordered
}

func buildManualProxyGroups(groups []CustomProxyGroup, nodeNames []string, needsDefaultProxyGroup bool) []manualProxyGroupConfig {
	var proxyGroups []manualProxyGroupConfig
	hasDefaultProxyGroup := false
	for _, group := range groups {
		groupConfig := manualProxyGroupConfig{
			Name:    group.Name,
			Type:    group.Type,
			Proxies: expandManualGroupProxies(group, nodeNames),
			Extra:   group.Extra,
		}
		if groupConfig.Name == manualDefaultProxyGroupName {
			hasDefaultProxyGroup = true
		}
		proxyGroups = append(proxyGroups, groupConfig)
	}

	if len(proxyGroups) == 0 || (needsDefaultProxyGroup && !hasDefaultProxyGroup) {
		proxyGroups = append([]manualProxyGroupConfig{defaultManualProxyGroup(nodeNames)}, proxyGroups...)
	}
	return proxyGroups
}

func expandManualGroupProxies(group CustomProxyGroup, nodeNames []string) []string {
	proxiesList := group.GetProxiesList()
	var expanded []string
	var excludeRegex *regexp.Regexp
	if group.Exclude != "" {
		excludeRegex, _ = regexp.Compile(group.Exclude)
	}

	seen := make(map[string]bool)
	appendProxy := func(proxy string) {
		if proxy == "" {
			return
		}
		if excludeRegex != nil && excludeRegex.MatchString(proxy) {
			return
		}
		if !seen[proxy] {
			seen[proxy] = true
			expanded = append(expanded, proxy)
		}
	}

	for _, proxy := range proxiesList {
		if proxy == "[ALL_NODES]" {
			for _, nodeName := range nodeNames {
				appendProxy(nodeName)
			}
			continue
		}
		appendProxy(proxy)
	}
	return expanded
}

func defaultManualProxyGroup(nodeNames []string) manualProxyGroupConfig {
	proxies := append([]string{}, nodeNames...)
	proxies = append(proxies, manualDirectPolicyName)
	return manualProxyGroupConfig{
		Name:    manualDefaultProxyGroupName,
		Type:    "select",
		Proxies: proxies,
	}
}

func mergeProxyGroupExtraMap(groupMap map[string]interface{}, extraJSON string) {
	if strings.TrimSpace(extraJSON) == "" {
		return
	}
	var extra map[string]interface{}
	if err := json.Unmarshal([]byte(extraJSON), &extra); err != nil {
		return
	}
	for key, value := range extra {
		key = strings.TrimSpace(key)
		if key == "" || copiedGroupProviderScopedExtraKeys[key] {
			continue
		}
		switch key {
		case "name", "type", "proxies":
			continue
		default:
			groupMap[key] = value
		}
	}
}

func buildManualRuleLines(rules []CustomRule) []string {
	if len(rules) == 0 {
		return []string{
			defaultGeositeDirectRule,
			defaultGeoIPDirectRule,
			defaultProxyMatchRule,
		}
	}

	lines := make([]string, 0, len(rules))
	terminalLines := make([]string, 0, 1)
	for _, rule := range rules {
		if rule.Target == deletedCustomRuleTarget {
			continue
		}
		ruleLine, _ := customRuleLineAndFingerprint(rule)
		if isTerminalRuleType(rule.Type) {
			terminalLines = append(terminalLines, ruleLine)
			continue
		}
		lines = append(lines, ruleLine)
	}
	return append(lines, terminalLines...)
}

func refreshProfileCache(profile *SubscriptionProfile) error {
	if normalizeProfileSourceType(profile.SourceType) == profileSourceLocal {
		manualYAML, err := BuildManualProfileYAML(profile.ID)
		if err != nil {
			return err
		}
		profile.RawResponse = manualYAML
		profile.Decoded = manualYAML
		return DB.Save(profile).Error
	}

	rawContent, _, err := loadProfileRawContent(*profile, fetchURLContent)
	if err != nil {
		return err
	}

	decodedContent, err := ProcessSubscriptionRawData(rawContent, profile.ID)
	if err != nil {
		return err
	}

	profile.RawResponse = rawContent
	profile.Decoded = decodedContent
	return DB.Save(profile).Error
}

func generateProfileSubToken(profile SubscriptionProfile) string {
	rawToken := fmt.Sprintf("profile:%d|%d|%s", profile.ID, time.Now().Unix(), uuid.New().String())
	return base64.URLEncoding.EncodeToString([]byte(rawToken))
}

func handleListProfiles(c *gin.Context) {
	var profiles []SubscriptionProfile
	if err := DB.Order("updated_at desc, id asc").Find(&profiles).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "获取配置列表失败"})
		return
	}

	items := make([]gin.H, 0, len(profiles))
	for _, profile := range profiles {
		items = append(items, profileListItem(profile))
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": items})
}

func handleCreateProfile(c *gin.Context) {
	var req profileWriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}

	normalizedReq, err := validateProfileWriteRequest(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}

	profile := SubscriptionProfile{
		Name:         normalizedReq.Name,
		SourceType:   normalizedReq.SourceType,
		URL:          normalizedReq.URL,
		LocalContent: normalizedReq.LocalContent,
	}
	if err := DB.Create(&profile).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "创建配置失败", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "配置创建成功", "data": profileListItem(profile)})
}

func handleUpdateProfile(c *gin.Context) {
	profile, ok := getProfileFromParam(c)
	if !ok {
		return
	}

	var req profileWriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}

	normalizedReq, err := validateProfileWriteRequest(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}

	profile.Name = normalizedReq.Name
	profile.SourceType = normalizedReq.SourceType
	profile.URL = normalizedReq.URL
	profile.LocalContent = normalizedReq.LocalContent
	if profile.SourceType == profileSourceRemote {
		profile.RawResponse = ""
		profile.Decoded = ""
	}

	if err := DB.Save(&profile).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "更新配置失败", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "配置更新成功", "data": profileListItem(profile)})
}

func handleDeleteProfile(c *gin.Context) {
	profile, ok := getProfileFromParam(c)
	if !ok {
		return
	}

	var count int64
	DB.Model(&SubscriptionProfile{}).Count(&count)
	if count <= 1 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "至少需要保留一个配置"})
		return
	}

	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("profile_id = ?", profile.ID).Delete(&CustomProxyGroup{}).Error; err != nil {
			return err
		}
		if err := tx.Where("profile_id = ?", profile.ID).Delete(&CustomNode{}).Error; err != nil {
			return err
		}
		if err := tx.Where("profile_id = ?", profile.ID).Delete(&CustomRule{}).Error; err != nil {
			return err
		}
		if err := tx.Where("profile_id = ?", profile.ID).Delete(&ProfileResourceOrder{}).Error; err != nil {
			return err
		}
		if err := tx.Where("profile_id = ?", profile.ID).Delete(&HiddenSubscriptionResource{}).Error; err != nil {
			return err
		}
		return tx.Delete(&profile).Error
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "删除配置失败", "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "配置已删除"})
}

func handleRefreshProfile(c *gin.Context) {
	profile, ok := getProfileFromParam(c)
	if !ok {
		return
	}

	if err := refreshProfileCache(&profile); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": "刷新配置失败", "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "配置刷新成功", "data": decodeResponseFromProfile(profile)})
}

func handleGetProfileSubscription(c *gin.Context) {
	profile, ok := getProfileFromParam(c)
	if !ok {
		return
	}

	if profile.Decoded == "" && profile.SourceType == profileSourceLocal {
		_ = refreshProfileCache(&profile)
	}
	if profile.Decoded == "" {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "当前配置暂无缓存，请先刷新配置"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": decodeResponseFromProfile(profile)})
}

func handleGetProfileSubToken(c *gin.Context) {
	profile, ok := getProfileFromParam(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "获取成功",
		"data": gin.H{
			"token":     profile.SubToken,
			"has_token": profile.SubToken != "",
			"profile":   profileListItem(profile),
		},
	})
}

func handleGenerateProfileSubToken(c *gin.Context) {
	profile, ok := getProfileFromParam(c)
	if !ok {
		return
	}

	token := generateProfileSubToken(profile)
	profile.SubToken = token
	if err := DB.Save(&profile).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "保存 Token 失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "Token 生成成功", "data": gin.H{"token": token, "profile": profileListItem(profile)}})
}

func mergeProfileRules(targetRules, sourceRules []CustomRule, targetProfileID uint) []CustomRule {
	merged := make(map[string]CustomRule, len(targetRules)+len(sourceRules))
	order := make([]string, 0, len(targetRules)+len(sourceRules))
	addRule := func(rule CustomRule) {
		key := rule.Type + "\x00" + rule.Payload
		if _, exists := merged[key]; !exists {
			order = append(order, key)
		}
		rule.ID = 0
		rule.ProfileID = targetProfileID
		merged[key] = rule
	}
	for _, rule := range targetRules {
		addRule(rule)
	}
	for _, rule := range sourceRules {
		addRule(rule)
	}

	result := make([]CustomRule, 0, len(order))
	for _, key := range order {
		result = append(result, merged[key])
	}
	return result
}

var copiedGroupProviderScopedExtraKeys = map[string]bool{
	"use":            true,
	"filter":         true,
	"exclude-filter": true,
	"exclude-type":   true,
}

func ensureProfileDecodedContent(profile *SubscriptionProfile) (string, error) {
	if strings.TrimSpace(profile.Decoded) != "" {
		return profile.Decoded, nil
	}
	if err := refreshProfileCache(profile); err != nil {
		return "", err
	}
	if strings.TrimSpace(profile.Decoded) == "" {
		return "", fmt.Errorf("来源配置暂无可复制的最终 YAML")
	}
	return profile.Decoded, nil
}

func extractCopyableProxyGroupsFromYAML(yamlContent string, targetProfileID uint) ([]CustomProxyGroup, []string, error) {
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(yamlContent), &root); err != nil {
		return nil, nil, fmt.Errorf("解析来源 YAML 失败: %w", err)
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("来源 YAML 格式无效")
	}

	docNode := root.Content[0]
	proxyNames := yamlSequenceNamesFromNode(findTopLevelSequenceNode(docNode, "proxies"))
	proxyNameSet := make(map[string]bool, len(proxyNames))
	for _, name := range proxyNames {
		proxyNameSet[name] = true
	}

	groupsNode := findTopLevelSequenceNode(docNode, "proxy-groups")
	if groupsNode == nil {
		return nil, nil, fmt.Errorf("来源配置没有 proxy-groups")
	}

	groupNames := yamlSequenceNamesFromNode(groupsNode)
	groupNameSet := make(map[string]bool, len(groupNames))
	for _, name := range groupNames {
		groupNameSet[name] = true
	}

	groups := make([]CustomProxyGroup, 0, len(groupsNode.Content))
	orderNames := make([]string, 0, len(groupsNode.Content))
	seenNames := make(map[string]bool, len(groupsNode.Content))
	for _, groupNode := range groupsNode.Content {
		group, ok, err := copyableProxyGroupFromYAMLNode(groupNode, targetProfileID, proxyNameSet, groupNameSet)
		if err != nil {
			return nil, nil, err
		}
		if !ok || seenNames[group.Name] {
			continue
		}
		seenNames[group.Name] = true
		groups = append(groups, group)
		orderNames = append(orderNames, group.Name)
	}
	if len(groups) == 0 {
		return nil, nil, fmt.Errorf("来源配置没有可复制的代理组")
	}
	return groups, orderNames, nil
}

func copyableProxyGroupFromYAMLNode(node *yaml.Node, targetProfileID uint, proxyNameSet, groupNameSet map[string]bool) (CustomProxyGroup, bool, error) {
	if node == nil || node.Kind != yaml.MappingNode {
		return CustomProxyGroup{}, false, nil
	}

	var name, groupType string
	var proxiesNode *yaml.Node
	extra := make(map[string]interface{})
	for i := 0; i < len(node.Content)-1; i += 2 {
		key := strings.TrimSpace(node.Content[i].Value)
		valueNode := node.Content[i+1]
		switch key {
		case "name":
			name = strings.TrimSpace(valueNode.Value)
		case "type":
			groupType = strings.TrimSpace(valueNode.Value)
		case "proxies":
			proxiesNode = valueNode
		default:
			if copiedGroupProviderScopedExtraKeys[key] {
				continue
			}
			var value interface{}
			if err := valueNode.Decode(&value); err == nil {
				extra[key] = value
			}
		}
	}
	if name == "" {
		return CustomProxyGroup{}, false, nil
	}
	if groupType == "" {
		groupType = "select"
	}

	proxies := copyableProxyGroupMembers(proxiesNode, proxyNameSet, groupNameSet)
	if len(proxies) == 0 {
		proxies = []string{"[ALL_NODES]"}
	}

	proxiesBytes, err := json.Marshal(proxies)
	if err != nil {
		return CustomProxyGroup{}, false, err
	}
	extraBytes, err := json.Marshal(extra)
	if err != nil {
		return CustomProxyGroup{}, false, err
	}

	return CustomProxyGroup{
		ProfileID: targetProfileID,
		Name:      name,
		Type:      groupType,
		Proxies:   string(proxiesBytes),
		Extra:     string(extraBytes),
	}, true, nil
}

func copyableProxyGroupMembers(proxiesNode *yaml.Node, proxyNameSet, groupNameSet map[string]bool) []string {
	if proxiesNode == nil || proxiesNode.Kind != yaml.SequenceNode {
		return []string{"[ALL_NODES]"}
	}

	members := []string{}
	seen := map[string]bool{}
	hasAllNodes := false
	appendMember := func(member string) {
		member = strings.TrimSpace(member)
		if member == "" || seen[member] {
			return
		}
		seen[member] = true
		members = append(members, member)
	}

	for _, item := range proxiesNode.Content {
		member := strings.TrimSpace(item.Value)
		if member == "" {
			continue
		}
		if proxyNameSet[member] {
			hasAllNodes = true
			continue
		}
		if groupNameSet[member] || isBuiltInProxyPolicy(member) || member == "[ALL_NODES]" {
			appendMember(member)
		}
	}

	if hasAllNodes && !seen["[ALL_NODES]"] {
		members = append([]string{"[ALL_NODES]"}, members...)
	}
	return members
}

func isBuiltInProxyPolicy(policy string) bool {
	switch strings.ToUpper(strings.TrimSpace(policy)) {
	case "DIRECT", "REJECT", "REJECT-DROP", "PASS":
		return true
	default:
		return false
	}
}

func yamlSequenceNamesFromNode(seq *yaml.Node) []string {
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return nil
	}
	names := make([]string, 0, len(seq.Content))
	for _, item := range seq.Content {
		if name := yamlMappingName(item); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func handleCopyProfileRules(c *gin.Context) {
	targetProfile, ok := getProfileFromParam(c)
	if !ok {
		return
	}

	var req copyProfileRulesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}
	if req.SourceProfileID == targetProfile.ID {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "不能从当前配置复制规则"})
		return
	}

	var sourceProfile SubscriptionProfile
	if err := DB.First(&sourceProfile, req.SourceProfileID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "来源配置不存在"})
		return
	}

	var sourceRules []CustomRule
	var targetRules []CustomRule
	if err := DB.Where("profile_id = ?", sourceProfile.ID).Find(&sourceRules).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "读取来源规则失败"})
		return
	}
	if err := DB.Where("profile_id = ?", targetProfile.ID).Find(&targetRules).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "读取当前规则失败"})
		return
	}

	mergedRules := mergeProfileRules(targetRules, sourceRules, targetProfile.ID)
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("profile_id = ?", targetProfile.ID).Delete(&CustomRule{}).Error; err != nil {
			return err
		}
		if len(mergedRules) > 0 {
			return tx.Create(&mergedRules).Error
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "复制规则失败", "error": err.Error()})
		return
	}

	ReapplyRulesToProfile(targetProfile.ID)
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "规则复制成功", "data": gin.H{"copied": len(sourceRules), "total": len(mergedRules)}})
}

func handleCopyProfileGroups(c *gin.Context) {
	targetProfile, ok := getProfileFromParam(c)
	if !ok {
		return
	}

	var req copyProfileRulesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}
	if req.SourceProfileID == targetProfile.ID {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "不能从当前配置复制代理组"})
		return
	}

	var sourceProfile SubscriptionProfile
	if err := DB.First(&sourceProfile, req.SourceProfileID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "来源配置不存在"})
		return
	}

	sourceYAML, err := ensureProfileDecodedContent(&sourceProfile)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": "读取来源代理组失败", "error": err.Error()})
		return
	}

	sourceGroups, orderNames, err := extractCopyableProxyGroupsFromYAML(sourceYAML, targetProfile.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("profile_id = ?", targetProfile.ID).Delete(&CustomProxyGroup{}).Error; err != nil {
			return err
		}
		if len(sourceGroups) > 0 {
			if err := tx.Create(&sourceGroups).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("profile_id = ? AND resource_type = ?", targetProfile.ID, resourceOrderTypeGroups).Delete(&ProfileResourceOrder{}).Error; err != nil {
			return err
		}
		cleanedOrder := cleanResourceOrderNames(orderNames)
		if len(cleanedOrder) > 0 {
			orders := make([]ProfileResourceOrder, 0, len(cleanedOrder))
			for idx, name := range cleanedOrder {
				orders = append(orders, ProfileResourceOrder{
					ProfileID:    targetProfile.ID,
					ResourceType: resourceOrderTypeGroups,
					Name:         name,
					SortOrder:    idx,
				})
			}
			if err := tx.Create(&orders).Error; err != nil {
				return err
			}
		}
		return tx.Model(&SubscriptionProfile{}).
			Where("id = ?", targetProfile.ID).
			Update("groups_mode", profileGroupsModeOverride).Error
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "复制代理组失败", "error": err.Error()})
		return
	}

	ReapplyRulesToProfile(targetProfile.ID)
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "代理组复制成功",
		"data": gin.H{
			"copied": len(sourceGroups),
			"total":  len(sourceGroups),
		},
	})
}

func parseSubscriptionRuleLine(ruleLine string, profileID uint) (CustomRule, bool) {
	parts := strings.Split(strings.TrimSpace(ruleLine), ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	if len(parts) < 2 || parts[0] == "" {
		return CustomRule{}, false
	}

	ruleType := strings.ToUpper(parts[0])
	if ruleType == "MATCH" || ruleType == "FINAL" {
		target := strings.Join(parts[1:], ",")
		if strings.TrimSpace(target) == "" {
			return CustomRule{}, false
		}
		return CustomRule{
			ProfileID: profileID,
			Type:      ruleType,
			Payload:   "-",
			Target:    target,
		}, true
	}

	if len(parts) < 3 {
		return CustomRule{}, false
	}
	targetStart := len(parts) - 1
	if isRuleOptionSuffix(parts[len(parts)-1]) && len(parts) >= 4 {
		targetStart = len(parts) - 2
	}
	payload := strings.Join(parts[1:targetStart], ",")
	target := strings.Join(parts[targetStart:], ",")
	if strings.TrimSpace(payload) == "" {
		return CustomRule{}, false
	}
	if strings.TrimSpace(target) == "" {
		return CustomRule{}, false
	}
	return CustomRule{
		ProfileID: profileID,
		Type:      ruleType,
		Payload:   payload,
		Target:    target,
	}, true
}

func isRuleOptionSuffix(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "no-resolve":
		return true
	default:
		return false
	}
}

func extractProfileRulesFromSubscriptionContent(content string, profileID uint) ([]CustomRule, error) {
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(content), &root); err != nil {
		return nil, fmt.Errorf("解析远程订阅 YAML 失败: %w", err)
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("远程订阅不是标准 YAML 映射结构")
	}

	docNode := root.Content[0]
	var rulesNode *yaml.Node
	for i := 0; i < len(docNode.Content); i += 2 {
		if docNode.Content[i].Value == "rules" {
			rulesNode = docNode.Content[i+1]
			break
		}
	}
	if rulesNode == nil || rulesNode.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("远程订阅中没有可本地化的 rules")
	}

	rules := make([]CustomRule, 0, len(rulesNode.Content))
	seen := make(map[string]bool)
	for _, ruleNode := range rulesNode.Content {
		if ruleNode.Kind != yaml.ScalarNode {
			continue
		}
		rule, ok := parseSubscriptionRuleLine(ruleNode.Value, profileID)
		if !ok {
			continue
		}
		key := rule.Type + "\x00" + rule.Payload
		if seen[key] {
			continue
		}
		seen[key] = true
		rules = append(rules, rule)
	}
	if len(rules) == 0 {
		return nil, fmt.Errorf("远程订阅中没有解析到可本地化的规则")
	}
	return rules, nil
}

func filterNewLocalizedRules(existingRules, candidateRules []CustomRule, profileID uint) ([]CustomRule, int) {
	existingKeys := make(map[string]bool, len(existingRules))
	for _, rule := range existingRules {
		existingKeys[rule.Type+"\x00"+rule.Payload] = true
	}

	var newRules []CustomRule
	skippedExisting := 0
	for _, rule := range candidateRules {
		key := rule.Type + "\x00" + rule.Payload
		if existingKeys[key] {
			skippedExisting++
			continue
		}
		rule.ID = 0
		rule.ProfileID = profileID
		newRules = append(newRules, rule)
		existingKeys[key] = true
	}
	return newRules, skippedExisting
}

func handleLocalizeProfileRules(c *gin.Context) {
	profile, ok := getProfileFromParam(c)
	if !ok {
		return
	}
	if normalizeProfileSourceType(profile.SourceType) == profileSourceLocal {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "本地手动配置没有远程原始规则可本地化"})
		return
	}

	if strings.TrimSpace(profile.RawResponse) == "" {
		if err := refreshProfileCache(&profile); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"code": 502, "message": "刷新远程订阅失败", "error": err.Error()})
			return
		}
	}

	plainContent, err := decodeSubscriptionPlainContent(profile.RawResponse)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": 422, "message": "远程订阅解析失败", "error": err.Error()})
		return
	}

	candidateRules, err := extractProfileRulesFromSubscriptionContent(plainContent, profile.ID)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": 422, "message": "远程规则解析失败", "error": err.Error()})
		return
	}

	var existingRules []CustomRule
	if err := DB.Where("profile_id = ?", profile.ID).Find(&existingRules).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "读取当前规则失败"})
		return
	}

	newRules, skippedExisting := filterNewLocalizedRules(existingRules, candidateRules, profile.ID)
	if len(newRules) > 0 {
		if err := DB.Create(&newRules).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "写入本地化规则失败", "error": err.Error()})
			return
		}
		ReapplyRulesToProfile(profile.ID)
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "远程规则本地化完成",
		"data": gin.H{
			"parsed":           len(candidateRules),
			"imported":         len(newRules),
			"skipped_existing": skippedExisting,
			"total":            len(existingRules) + len(newRules),
		},
	})
}

type BackupData struct {
	Profiles        []SubscriptionProfile        `json:"profiles"`
	Groups          []CustomProxyGroup           `json:"groups"`
	Nodes           []CustomNode                 `json:"nodes"`
	Rules           []CustomRule                 `json:"rules"`
	HiddenResources []HiddenSubscriptionResource `json:"hidden_resources"`
}

func handleBackup(c *gin.Context) {
	var data BackupData
	if err := DB.Find(&data.Profiles).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "获取配置失败"})
		return
	}
	if err := DB.Find(&data.Groups).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "获取策略组失败"})
		return
	}
	if err := DB.Find(&data.Nodes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "获取节点失败"})
		return
	}
	if err := DB.Find(&data.Rules).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "获取规则失败"})
		return
	}
	if err := DB.Find(&data.HiddenResources).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "获取隐藏资源失败"})
		return
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "JSON 序列化失败"})
		return
	}

	c.Header("Content-Disposition", "attachment; filename=\"clash_proxy_backup.json\"")
	c.Data(http.StatusOK, "application/json", jsonData)
}

func handleImport(c *gin.Context) {
	var data BackupData
	if err := c.ShouldBindJSON(&data); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "上传的 JSON 格式解析失败"})
		return
	}

	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&CustomProxyGroup{}).Error; err != nil {
			return err
		}
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&CustomNode{}).Error; err != nil {
			return err
		}
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&CustomRule{}).Error; err != nil {
			return err
		}
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&HiddenSubscriptionResource{}).Error; err != nil {
			return err
		}
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&SubscriptionProfile{}).Error; err != nil {
			return err
		}

		defaultProfileID := uint(0)
		profileIDMap := map[uint]uint{}
		if len(data.Profiles) > 0 {
			oldProfileIDs := make([]uint, len(data.Profiles))
			for i := range data.Profiles {
				oldProfileID := data.Profiles[i].ID
				oldProfileIDs[i] = oldProfileID
				data.Profiles[i].ID = 0
				data.Profiles[i].SourceType = normalizeProfileSourceType(data.Profiles[i].SourceType)
				data.Profiles[i].SubToken = strings.TrimSpace(data.Profiles[i].SubToken)
				if data.Profiles[i].Name == "" {
					data.Profiles[i].Name = fmt.Sprintf("导入配置 %d", i+1)
				}
			}
			if err := tx.Create(&data.Profiles).Error; err != nil {
				return err
			}
			defaultProfileID = data.Profiles[0].ID
			for i := range data.Profiles {
				profileIDMap[oldProfileIDs[i]] = data.Profiles[i].ID
				profileIDMap[data.Profiles[i].ID] = data.Profiles[i].ID
			}
		} else {
			profile := SubscriptionProfile{Name: "导入配置", SourceType: profileSourceLocal}
			if err := tx.Create(&profile).Error; err != nil {
				return err
			}
			defaultProfileID = profile.ID
		}

		for i := range data.Groups {
			data.Groups[i].ID = 0
			if mappedProfileID, ok := profileIDMap[data.Groups[i].ProfileID]; ok && mappedProfileID > 0 {
				data.Groups[i].ProfileID = mappedProfileID
			}
			if data.Groups[i].ProfileID == 0 {
				data.Groups[i].ProfileID = defaultProfileID
			}
		}
		for i := range data.Nodes {
			data.Nodes[i].ID = 0
			if mappedProfileID, ok := profileIDMap[data.Nodes[i].ProfileID]; ok && mappedProfileID > 0 {
				data.Nodes[i].ProfileID = mappedProfileID
			}
			if data.Nodes[i].ProfileID == 0 {
				data.Nodes[i].ProfileID = defaultProfileID
			}
		}
		for i := range data.Rules {
			data.Rules[i].ID = 0
			if mappedProfileID, ok := profileIDMap[data.Rules[i].ProfileID]; ok && mappedProfileID > 0 {
				data.Rules[i].ProfileID = mappedProfileID
			}
			if data.Rules[i].ProfileID == 0 {
				data.Rules[i].ProfileID = defaultProfileID
			}
		}
		for i := range data.HiddenResources {
			data.HiddenResources[i].ID = 0
			if mappedProfileID, ok := profileIDMap[data.HiddenResources[i].ProfileID]; ok && mappedProfileID > 0 {
				data.HiddenResources[i].ProfileID = mappedProfileID
			}
			if data.HiddenResources[i].ProfileID == 0 {
				data.HiddenResources[i].ProfileID = defaultProfileID
			}
			data.HiddenResources[i].ResourceType = strings.TrimSpace(data.HiddenResources[i].ResourceType)
			data.HiddenResources[i].Name = strings.TrimSpace(data.HiddenResources[i].Name)
		}

		if len(data.Groups) > 0 {
			if err := tx.Create(&data.Groups).Error; err != nil {
				return err
			}
		}
		if len(data.Nodes) > 0 {
			if err := tx.Create(&data.Nodes).Error; err != nil {
				return err
			}
		}
		if len(data.Rules) > 0 {
			if err := tx.Create(&data.Rules).Error; err != nil {
				return err
			}
		}
		for _, hiddenResource := range data.HiddenResources {
			if hiddenResource.Name == "" || !isValidSubscriptionResourceType(hiddenResource.ResourceType) {
				continue
			}
			if err := HideSubscriptionResourceTx(tx, hiddenResource.ProfileID, hiddenResource.ResourceType, hiddenResource.Name); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "导入失败，数据库事务已回滚: " + err.Error()})
		return
	}

	ReapplyRulesToAllProfiles()
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "备份导入成功，全部数据已覆盖"})
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

	profileID, ok := resolveRequestProfileID(c, req.ProfileID)
	if !ok {
		return
	}

	targetURL, err := normalizeSubscriptionURL(req.URL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}

	var profile SubscriptionProfile
	if err := DB.First(&profile, profileID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "配置不存在"})
		return
	}

	profile.SourceType = profileSourceRemote
	profile.URL = targetURL
	profile.LocalContent = ""
	if err := refreshProfileCache(&profile); err != nil {
		statusCode := http.StatusBadGateway
		if strings.Contains(err.Error(), "格式不支持") {
			statusCode = http.StatusUnprocessableEntity
		}
		c.JSON(statusCode, gin.H{
			"code":    statusCode,
			"message": "获取或解析目标地址内容失败",
			"error":   err.Error(),
		})
		return
	}

	var sub Subscription
	if err := DB.Where("url = ?", targetURL).First(&sub).Error; err != nil {
		sub = Subscription{URL: targetURL}
	}
	sub.RawResponse = profile.RawResponse
	sub.Decoded = profile.Decoded
	DB.Save(&sub)

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "success",
		"data":    decodeResponseFromProfile(profile),
	})
}

// handleGetSubscription 获取当前配置保存的订阅配置
func handleGetSubscription(c *gin.Context) {
	profileID, ok := resolveRequestProfileID(c, 0)
	if !ok {
		return
	}

	var profile SubscriptionProfile
	if err := DB.First(&profile, profileID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "配置不存在"})
		return
	}
	if profile.Decoded == "" {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "当前配置暂无缓存，请先刷新配置"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": decodeResponseFromProfile(profile),
	})
}

// handleGetCustomGroups 获取所有自定义策略组
func handleGetCustomGroups(c *gin.Context) {
	profileID, ok := resolveRequestProfileID(c, 0)
	if !ok {
		return
	}
	groups, err := GetCustomProxyGroups(profileID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "获取失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": groups})
}

func handleUpdateResourceOrder(c *gin.Context) {
	var req struct {
		ProfileID    uint     `json:"profile_id"`
		ResourceType string   `json:"resource_type" binding:"required"`
		Names        []string `json:"names"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}

	req.ResourceType = strings.TrimSpace(req.ResourceType)
	if !isValidResourceOrderType(req.ResourceType) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "资源类型不支持"})
		return
	}

	profileID, ok := resolveRequestProfileID(c, req.ProfileID)
	if !ok {
		return
	}

	cleanedNames := cleanResourceOrderNames(req.Names)
	if err := SaveProfileResourceOrder(profileID, req.ResourceType, cleanedNames); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "保存排序失败", "error": err.Error()})
		return
	}

	ReapplyRulesToProfile(profileID)
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "排序保存成功",
		"data": gin.H{
			"resource_type": req.ResourceType,
			"names":         cleanedNames,
		},
	})
}

type subscriptionResourceWriteRequest struct {
	ProfileID    uint                   `json:"profile_id"`
	ResourceType string                 `json:"resource_type" binding:"required"`
	Name         string                 `json:"name" binding:"required"`
	Data         map[string]interface{} `json:"data"`
}

func normalizeSubscriptionResourceWriteRequest(req subscriptionResourceWriteRequest) (subscriptionResourceWriteRequest, error) {
	req.ResourceType = strings.TrimSpace(req.ResourceType)
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return req, fmt.Errorf("资源名称不能为空")
	}
	if !isValidSubscriptionResourceType(req.ResourceType) {
		return req, fmt.Errorf("资源类型不支持")
	}
	if req.Data == nil {
		req.Data = map[string]interface{}{}
	}
	return req, nil
}

func handleTakeoverSubscriptionResource(c *gin.Context) {
	var req subscriptionResourceWriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}
	req, err := normalizeSubscriptionResourceWriteRequest(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}
	profileID, ok := resolveRequestProfileID(c, req.ProfileID)
	if !ok {
		return
	}

	var data interface{}
	err = DB.Transaction(func(tx *gorm.DB) error {
		switch req.ResourceType {
		case resourceOrderTypeNodes:
			node, err := upsertTakenOverNodeTx(tx, profileID, req.Name, req.Data)
			if err != nil {
				return err
			}
			data = node
		case resourceOrderTypeGroups:
			group, err := upsertTakenOverGroupTx(tx, profileID, req.Name, req.Data)
			if err != nil {
				return err
			}
			data = group
		default:
			return fmt.Errorf("资源类型不支持")
		}
		if err := UnhideSubscriptionResourceTx(tx, profileID, req.ResourceType, req.Name); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "接管资源失败", "error": err.Error()})
		return
	}
	ReapplyRulesToProfile(profileID)
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "资源已接管为自定义配置", "data": data})
}

func handleDeleteSubscriptionResource(c *gin.Context) {
	var req subscriptionResourceWriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}
	req, err := normalizeSubscriptionResourceWriteRequest(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}
	profileID, ok := resolveRequestProfileID(c, req.ProfileID)
	if !ok {
		return
	}

	if err := DB.Transaction(func(tx *gorm.DB) error {
		if err := HideSubscriptionResourceTx(tx, profileID, req.ResourceType, req.Name); err != nil {
			return err
		}
		switch req.ResourceType {
		case resourceOrderTypeNodes:
			if err := tx.Where("profile_id = ? AND name = ?", profileID, req.Name).Delete(&CustomNode{}).Error; err != nil {
				return err
			}
		case resourceOrderTypeGroups:
			if err := tx.Where("profile_id = ? AND name = ?", profileID, req.Name).Delete(&CustomProxyGroup{}).Error; err != nil {
				return err
			}
			if err := tx.Model(&CustomRule{}).
				Where("profile_id = ? AND target = ?", profileID, req.Name).
				Update("target", deletedCustomRuleTarget).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "删除资源失败", "error": err.Error()})
		return
	}

	ReapplyRulesToProfile(profileID)
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "资源已从当前配置隐藏"})
}

func upsertTakenOverNodeTx(tx *gorm.DB, profileID uint, fallbackName string, data map[string]interface{}) (CustomNode, error) {
	configMap := cloneStringAnyMap(data)
	name := strings.TrimSpace(valueAsString(configMap["name"]))
	if name == "" {
		name = fallbackName
		configMap["name"] = name
	}
	nodeType := strings.TrimSpace(valueAsString(configMap["type"]))
	if nodeType == "" {
		nodeType = "unknown"
		configMap["type"] = nodeType
	}
	server := strings.TrimSpace(valueAsString(configMap["server"]))
	if server == "" {
		server = "-"
		configMap["server"] = server
	}
	port := valueAsInt(configMap["port"])
	if port <= 0 {
		port = 0
		configMap["port"] = port
	}

	configBytes, err := json.Marshal(configMap)
	if err != nil {
		return CustomNode{}, err
	}
	node := CustomNode{
		ProfileID: profileID,
		Name:      name,
		Type:      nodeType,
		Server:    server,
		Port:      port,
		Config:    string(configBytes),
	}
	err = tx.Where("profile_id = ? AND name = ?", profileID, name).
		Assign(node).
		FirstOrCreate(&node).Error
	return node, err
}

func upsertTakenOverGroupTx(tx *gorm.DB, profileID uint, fallbackName string, data map[string]interface{}) (CustomProxyGroup, error) {
	groupMap := cloneStringAnyMap(data)
	name := strings.TrimSpace(valueAsString(groupMap["name"]))
	if name == "" {
		name = fallbackName
	}
	groupType := strings.TrimSpace(valueAsString(groupMap["type"]))
	if groupType == "" {
		groupType = "select"
	}
	proxies := stringSliceFromAny(groupMap["proxies"])
	if len(proxies) == 0 {
		proxies = []string{"[ALL_NODES]"}
	}
	proxiesBytes, err := json.Marshal(proxies)
	if err != nil {
		return CustomProxyGroup{}, err
	}
	extra := map[string]interface{}{}
	for key, value := range groupMap {
		key = strings.TrimSpace(key)
		if key == "" || copiedGroupProviderScopedExtraKeys[key] {
			continue
		}
		switch key {
		case "name", "type", "proxies":
			continue
		default:
			extra[key] = value
		}
	}
	extraBytes, err := json.Marshal(extra)
	if err != nil {
		return CustomProxyGroup{}, err
	}
	group := CustomProxyGroup{
		ProfileID: profileID,
		Name:      name,
		Type:      groupType,
		Proxies:   string(proxiesBytes),
		Extra:     string(extraBytes),
	}
	err = tx.Where("profile_id = ? AND name = ?", profileID, name).
		Assign(group).
		FirstOrCreate(&group).Error
	if err != nil {
		return CustomProxyGroup{}, err
	}
	if err := tx.Model(&SubscriptionProfile{}).Where("id = ?", profileID).Update("groups_mode", profileGroupsModeMerge).Error; err != nil {
		return CustomProxyGroup{}, err
	}
	return group, nil
}

func cloneStringAnyMap(input map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func valueAsInt(value interface{}) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case uint:
		return int(v)
	case uint64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	default:
		n, _ := strconv.Atoi(valueAsString(v))
		return n
	}
}

func stringSliceFromAny(value interface{}) []string {
	switch v := value.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if itemValue := valueAsString(item); itemValue != "" {
				out = append(out, itemValue)
			}
		}
		return out
	default:
		if itemValue := valueAsString(value); itemValue != "" {
			return []string{itemValue}
		}
		return nil
	}
}

// handleCreateCustomGroup 创建新策略组
func handleCreateCustomGroup(c *gin.Context) {
	var req struct {
		ProfileID uint     `json:"profile_id"`
		Name      string   `json:"name"`
		Type      string   `json:"type"`
		Proxies   []string `json:"proxies"`
		Exclude   string   `json:"exclude"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}
	profileID, ok := resolveRequestProfileID(c, req.ProfileID)
	if !ok {
		return
	}

	proxiesBytes, _ := json.Marshal(req.Proxies)
	group := CustomProxyGroup{
		ProfileID: profileID,
		Name:      req.Name,
		Type:      req.Type,
		Proxies:   string(proxiesBytes),
		Exclude:   req.Exclude,
	}
	if err := DB.Transaction(func(tx *gorm.DB) error {
		if err := UnhideSubscriptionResourceTx(tx, profileID, resourceOrderTypeGroups, group.Name); err != nil {
			return err
		}
		return tx.Create(&group).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "保存失败", "error": err.Error()})
		return
	}
	DB.Model(&SubscriptionProfile{}).Where("id = ?", profileID).Update("groups_mode", profileGroupsModeMerge)
	ReapplyRulesToProfile(profileID)
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "保存成功", "data": group})
}

// handleUpdateCustomGroup 更新策略组
func handleUpdateCustomGroup(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Name    string   `json:"name"`
		Type    string   `json:"type"`
		Proxies []string `json:"proxies"`
		Exclude string   `json:"exclude"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}

	proxiesBytes, _ := json.Marshal(req.Proxies)
	var group CustomProxyGroup
	if err := DB.First(&group, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "代理组不存在"})
		return
	}

	oldName := group.Name
	group.Name = req.Name
	group.Type = req.Type
	group.Proxies = string(proxiesBytes)
	group.Exclude = req.Exclude

	if err := DB.Transaction(func(tx *gorm.DB) error {
		if strings.TrimSpace(oldName) != strings.TrimSpace(group.Name) {
			if err := HideSubscriptionResourceTx(tx, group.ProfileID, resourceOrderTypeGroups, oldName); err != nil {
				return err
			}
		}
		if err := UnhideSubscriptionResourceTx(tx, group.ProfileID, resourceOrderTypeGroups, group.Name); err != nil {
			return err
		}
		return tx.Save(&group).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "更新失败", "error": err.Error()})
		return
	}
	ReapplyRulesToProfile(group.ProfileID)
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "更新成功", "data": group})
}

// handleDeleteCustomGroup 删除自定义组
func handleDeleteCustomGroup(c *gin.Context) {
	id := c.Param("id")
	var group CustomProxyGroup
	if err := DB.First(&group, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "代理组不存在"})
		return
	}
	if err := DB.Transaction(func(tx *gorm.DB) error {
		if err := HideSubscriptionResourceTx(tx, group.ProfileID, resourceOrderTypeGroups, group.Name); err != nil {
			return err
		}
		if err := tx.Model(&CustomRule{}).
			Where("profile_id = ? AND target = ?", group.ProfileID, group.Name).
			Update("target", deletedCustomRuleTarget).Error; err != nil {
			return err
		}
		return tx.Delete(&group).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "删除失败", "error": err.Error()})
		return
	}
	var remainingCount int64
	DB.Model(&CustomProxyGroup{}).Where("profile_id = ?", group.ProfileID).Count(&remainingCount)
	if remainingCount == 0 {
		DB.Model(&SubscriptionProfile{}).Where("id = ?", group.ProfileID).Update("groups_mode", profileGroupsModeMerge)
	}
	ReapplyRulesToProfile(group.ProfileID)
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "删除成功"})
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
	profileID, ok := resolveRequestProfileID(c, 0)
	if !ok {
		return
	}
	nodes, err := GetCustomNodes(profileID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "获取失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": nodes})
}

// handleCreateCustomNode 创建或保存自定义节点
func handleCreateCustomNode(c *gin.Context) {
	var req struct {
		ProfileID uint                   `json:"profile_id"`
		Name      string                 `json:"name"`
		Type      string                 `json:"type"`
		Server    string                 `json:"server"`
		Port      int                    `json:"port"`
		Config    map[string]interface{} `json:"config"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}
	profileID, ok := resolveRequestProfileID(c, req.ProfileID)
	if !ok {
		return
	}

	configBytes, _ := json.Marshal(req.Config)
	node := CustomNode{
		ProfileID: profileID,
		Name:      req.Name,
		Type:      req.Type,
		Server:    req.Server,
		Port:      req.Port,
		Config:    string(configBytes),
	}
	if err := DB.Transaction(func(tx *gorm.DB) error {
		if err := UnhideSubscriptionResourceTx(tx, profileID, resourceOrderTypeNodes, node.Name); err != nil {
			return err
		}
		return tx.Create(&node).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "保存失败", "error": err.Error()})
		return
	}
	ReapplyRulesToProfile(profileID)
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "保存成功", "data": node})
}

// handleDeleteCustomNode 删除自定义节点
func handleDeleteCustomNode(c *gin.Context) {
	id := c.Param("id")
	var node CustomNode
	if err := DB.First(&node, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "节点不存在"})
		return
	}
	if err := DB.Transaction(func(tx *gorm.DB) error {
		if err := HideSubscriptionResourceTx(tx, node.ProfileID, resourceOrderTypeNodes, node.Name); err != nil {
			return err
		}
		return tx.Delete(&node).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "删除失败", "error": err.Error()})
		return
	}
	ReapplyRulesToProfile(node.ProfileID)
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "删除成功"})
}

// handleUpdateCustomNode 更新自定义节点
func handleUpdateCustomNode(c *gin.Context) {
	id := c.Param("id")
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
	var node CustomNode
	if err := DB.First(&node, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "节点不存在"})
		return
	}

	oldName := node.Name
	node.Name = req.Name
	node.Type = req.Type
	node.Server = req.Server
	node.Port = req.Port
	node.Config = string(configBytes)

	if err := DB.Transaction(func(tx *gorm.DB) error {
		if strings.TrimSpace(oldName) != strings.TrimSpace(node.Name) {
			if err := HideSubscriptionResourceTx(tx, node.ProfileID, resourceOrderTypeNodes, oldName); err != nil {
				return err
			}
		}
		if err := UnhideSubscriptionResourceTx(tx, node.ProfileID, resourceOrderTypeNodes, node.Name); err != nil {
			return err
		}
		return tx.Save(&node).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "更新失败", "error": err.Error()})
		return
	}
	ReapplyRulesToProfile(node.ProfileID)
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "更新成功", "data": node})
}

// handleGetCustomRules 获取所有自定义规则
func handleGetCustomRules(c *gin.Context) {
	profileID, ok := resolveRequestProfileID(c, 0)
	if !ok {
		return
	}
	rules, err := GetCustomRules(profileID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "获取失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": rules})
}

type customRuleWritePayload struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
	Target  string `json:"target"`
}

type batchCustomRulesRequest struct {
	ProfileID uint                     `json:"profile_id"`
	Rules     []customRuleWritePayload `json:"rules"`
}

type customRulesBatchProgress struct {
	Stage   string `json:"stage"`
	Current int    `json:"current"`
	Total   int    `json:"total"`
	Saved   int    `json:"saved,omitempty"`
	Message string `json:"message"`
}

const customRulesStreamBatchSize = 100
const deletedCustomRuleTarget = "__DELETE__"

func normalizeCustomRuleWritePayload(input customRuleWritePayload) (customRuleWritePayload, error) {
	rule := customRuleWritePayload{
		Type:    strings.TrimSpace(input.Type),
		Payload: strings.TrimSpace(input.Payload),
		Target:  strings.TrimSpace(input.Target),
	}
	if rule.Type == "" {
		return customRuleWritePayload{}, fmt.Errorf("规则类型不能为空")
	}
	if rule.Payload == "" {
		rule.Payload = "-"
	}
	if rule.Target == "" {
		return customRuleWritePayload{}, fmt.Errorf("目标策略不能为空")
	}
	return rule, nil
}

func normalizeBatchCustomRules(profileID uint, inputs []customRuleWritePayload) ([]CustomRule, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("没有需要保存的规则")
	}

	rules := make([]CustomRule, 0, len(inputs))
	ruleIndexes := make(map[string]int, len(inputs))
	for idx, input := range inputs {
		rule, err := normalizeCustomRuleWritePayload(input)
		if err != nil {
			return nil, fmt.Errorf("第 %d 条规则无效: %w", idx+1, err)
		}

		key := rule.Type + "\x00" + rule.Payload
		if existingIndex, ok := ruleIndexes[key]; ok {
			rules[existingIndex].Target = rule.Target
			continue
		}

		ruleIndexes[key] = len(rules)
		rules = append(rules, CustomRule{
			ProfileID: profileID,
			Type:      rule.Type,
			Payload:   rule.Payload,
			Target:    rule.Target,
		})
	}
	return rules, nil
}

func normalizeBatchDeletedCustomRules(profileID uint, inputs []customRuleWritePayload) ([]CustomRule, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("没有需要删除的规则")
	}

	deleteInputs := make([]customRuleWritePayload, 0, len(inputs))
	for _, input := range inputs {
		deleteInputs = append(deleteInputs, customRuleWritePayload{
			Type:    input.Type,
			Payload: input.Payload,
			Target:  deletedCustomRuleTarget,
		})
	}
	return normalizeBatchCustomRules(profileID, deleteInputs)
}

func customRuleBatchProgressSteps(total int, batchSize int) []int {
	if total <= 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = total
	}

	steps := make([]int, 0, (total+batchSize-1)/batchSize)
	for current := batchSize; current < total; current += batchSize {
		steps = append(steps, current)
	}
	return append(steps, total)
}

func upsertCustomRulesBatchWithProgress(rules []CustomRule, batchSize int, onProgress func(current, total int) error) error {
	if len(rules) == 0 {
		return nil
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		steps := customRuleBatchProgressSteps(len(rules), batchSize)
		previous := 0
		for _, current := range steps {
			batch := rules[previous:current]
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{
					{Name: "profile_id"},
					{Name: "type"},
					{Name: "payload"},
				},
				DoUpdates: clause.AssignmentColumns([]string{"target"}),
			}).Create(&batch).Error; err != nil {
				return err
			}
			if onProgress != nil {
				if err := onProgress(current, len(rules)); err != nil {
					return err
				}
			}
			previous = current
		}
		return nil
	})
}

func upsertCustomRulesBatch(rules []CustomRule) error {
	return upsertCustomRulesBatchWithProgress(rules, len(rules), nil)
}

func writeSSEEvent(c *gin.Context, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return err
	}
	if flusher, ok := c.Writer.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

func handleBatchSaveCustomRules(c *gin.Context) {
	var req batchCustomRulesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}
	profileID, ok := resolveRequestProfileID(c, req.ProfileID)
	if !ok {
		return
	}
	if err := DB.First(&SubscriptionProfile{}, profileID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "配置不存在"})
		return
	}

	rules, err := normalizeBatchCustomRules(profileID, req.Rules)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}
	if err := upsertCustomRulesBatch(rules); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "批量保存规则失败", "error": err.Error()})
		return
	}

	ReapplyRulesToProfile(profileID)
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "规则批量保存成功",
		"data": gin.H{
			"saved": len(rules),
		},
	})
}

func handleBatchDeleteCustomRules(c *gin.Context) {
	var req batchCustomRulesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}
	profileID, ok := resolveRequestProfileID(c, req.ProfileID)
	if !ok {
		return
	}
	if err := DB.First(&SubscriptionProfile{}, profileID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "配置不存在"})
		return
	}

	rules, err := normalizeBatchDeletedCustomRules(profileID, req.Rules)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}
	if err := upsertCustomRulesBatch(rules); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "批量删除规则失败", "error": err.Error()})
		return
	}

	ReapplyRulesToProfile(profileID)
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "规则批量删除成功",
		"data": gin.H{
			"deleted": len(rules),
		},
	})
}

func handleBatchSaveCustomRulesStream(c *gin.Context) {
	var req batchCustomRulesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}
	profileID, ok := resolveRequestProfileID(c, req.ProfileID)
	if !ok {
		return
	}
	if err := DB.First(&SubscriptionProfile{}, profileID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "配置不存在"})
		return
	}

	rules, err := normalizeBatchCustomRules(profileID, req.Rules)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")

	total := len(rules)
	if err := writeSSEEvent(c, "progress", customRulesBatchProgress{
		Stage:   "saving",
		Current: 0,
		Total:   total,
		Message: fmt.Sprintf("准备保存 0/%d 条规则", total),
	}); err != nil {
		return
	}

	err = upsertCustomRulesBatchWithProgress(rules, customRulesStreamBatchSize, func(current, total int) error {
		return writeSSEEvent(c, "progress", customRulesBatchProgress{
			Stage:   "saving",
			Current: current,
			Total:   total,
			Message: fmt.Sprintf("已保存 %d/%d 条规则", current, total),
		})
	})
	if err != nil {
		_ = writeSSEEvent(c, "error", gin.H{"message": "批量保存规则失败: " + err.Error()})
		return
	}

	if err := writeSSEEvent(c, "progress", customRulesBatchProgress{
		Stage:   "reapplying",
		Current: total,
		Total:   total,
		Message: "规则已保存，正在重新应用订阅配置",
	}); err != nil {
		return
	}

	ReapplyRulesToProfile(profileID)
	_ = writeSSEEvent(c, "complete", customRulesBatchProgress{
		Stage:   "complete",
		Current: total,
		Total:   total,
		Saved:   total,
		Message: fmt.Sprintf("规则批量保存完成，共保存 %d 条规则", total),
	})
}

// handleCreateCustomRule 创建或保存自定义规则 (支持按 Type 和 Payload 进行 Upsert 智能覆盖)
func handleCreateCustomRule(c *gin.Context) {
	var req struct {
		ProfileID uint   `json:"profile_id"`
		Type      string `json:"type" binding:"required"`
		Payload   string `json:"payload" binding:"required"`
		Target    string `json:"target" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}
	profileID, ok := resolveRequestProfileID(c, req.ProfileID)
	if !ok {
		return
	}

	var existingRule CustomRule
	if err := DB.Where("profile_id = ? AND type = ? AND payload = ?", profileID, req.Type, req.Payload).First(&existingRule).Error; err == nil {
		// 已存在相同的拦截条件，执行覆盖更新
		existingRule.Target = req.Target
		if err := DB.Save(&existingRule).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "覆盖更新失败", "error": err.Error()})
			return
		}
		ReapplyRulesToProfile(profileID)
		c.JSON(http.StatusOK, gin.H{"code": 200, "message": "规则已覆盖更新", "data": existingRule})
		return
	}

	// 不存在，创建新规则
	rule := CustomRule{
		ProfileID: profileID,
		Type:      req.Type,
		Payload:   req.Payload,
		Target:    req.Target,
	}
	if err := DB.Create(&rule).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "保存失败", "error": err.Error()})
		return
	}
	ReapplyRulesToProfile(profileID)
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "保存成功", "data": rule})
}

// handleUpdateCustomRule 更新自定义规则
func handleUpdateCustomRule(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Type    string `json:"type" binding:"required"`
		Payload string `json:"payload" binding:"required"`
		Target  string `json:"target" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}

	var rule CustomRule
	if err := DB.First(&rule, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "规则不存在"})
		return
	}

	rule.Type = req.Type
	rule.Payload = req.Payload
	rule.Target = req.Target

	if err := DB.Save(&rule).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "更新失败", "error": err.Error()})
		return
	}
	ReapplyRulesToProfile(rule.ProfileID)
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "更新成功", "data": rule})
}

// handleDeleteCustomRule 删除自定义规则
func handleDeleteCustomRule(c *gin.Context) {
	id := c.Param("id")
	var rule CustomRule
	if err := DB.First(&rule, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "规则不存在"})
		return
	}
	if err := DB.Delete(&rule).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "删除失败", "error": err.Error()})
		return
	}
	ReapplyRulesToProfile(rule.ProfileID)
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "删除成功"})
}

// injectCustomNodes 基于 yaml.v3 的 Node 树完成自定义节点的注入
func injectCustomNodes(yamlContent string, profileID uint) string {
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

	// 寻找 proxies 节点与 proxy-groups 节点
	var proxiesNode *yaml.Node
	var proxyGroupsNode *yaml.Node
	for i := 0; i < len(docNode.Content); i += 2 {
		switch docNode.Content[i].Value {
		case "proxies":
			proxiesNode = docNode.Content[i+1]
		case "proxy-groups":
			proxyGroupsNode = docNode.Content[i+1]
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
	customNodes, _ := GetCustomNodes(profileID)
	hiddenNodes := loadHiddenSubscriptionResourceNames(profileID, resourceOrderTypeNodes)
	filterYAMLSequenceByName(proxiesNode, hiddenNodes, customNodeNameSet(customNodes))

	var customProxyNodes []*yaml.Node
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
			customProxyNodes = append(customProxyNodes, tempRoot.Content[0])
		}

		// 智能感知：自动将该自定义节点注入到订阅原有的各个代理组的 proxies 列表中
		// （自定义策略组由于会在后续阶段独立注入，因此此处天然排除了自定义组，符合 KISS 和业务原则）
		if proxyGroupsNode != nil && proxyGroupsNode.Kind == yaml.SequenceNode {
			for _, groupNode := range proxyGroupsNode.Content {
				if groupNode.Kind != yaml.MappingNode {
					continue
				}
				// 寻找组内的 proxies 序列节点
				for i := 0; i < len(groupNode.Content); i += 2 {
					if groupNode.Content[i].Value == "proxies" {
						groupProxiesNode := groupNode.Content[i+1]
						if groupProxiesNode.Kind == yaml.SequenceNode {
							// 将自定义节点名优雅地插入到该组代理列表的最前方
							groupProxiesNode.Content = append([]*yaml.Node{{Kind: yaml.ScalarNode, Value: cn.Name}}, groupProxiesNode.Content...)
						}
						break
					}
				}
			}
		}
	}

	if len(customProxyNodes) > 0 {
		// 自定义节点保持创建顺序前置，显式排序记录会在后续统一覆盖。
		proxiesNode.Content = append(customProxyNodes, proxiesNode.Content...)
	}

	if proxyGroupsNode != nil && proxyGroupsNode.Kind == yaml.SequenceNode {
		availableNames := yamlSequenceNameSet(proxiesNode)
		for _, groupNode := range proxyGroupsNode.Content {
			if name := yamlMappingName(groupNode); name != "" {
				availableNames[name] = true
			}
		}
		pruneProxyGroupMembers(proxyGroupsNode, availableNames)
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
func injectCustomGroups(yamlContent string, profileID uint) string {
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

	// 提取所有的 proxies 节点以备使用
	var proxiesNode *yaml.Node
	var allNodeNames []string
	for i := 0; i < len(docNode.Content); i += 2 {
		if docNode.Content[i].Value == "proxies" {
			proxiesNode = docNode.Content[i+1]
			seq := proxiesNode
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

	customGroups, _ := GetCustomProxyGroups(profileID)
	if proxyGroupsNode == nil || proxyGroupsNode.Kind != yaml.SequenceNode {
		if len(customGroups) == 0 {
			return yamlContent
		}
		proxyGroupsNode = &yaml.Node{Kind: yaml.SequenceNode}
		docNode.Content = append(docNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "proxy-groups"},
			proxyGroupsNode,
		)
	}

	if shouldOverrideProfileProxyGroups(profileID, len(customGroups)) {
		proxyGroupsNode.Content = nil
	} else {
		hiddenGroups := loadHiddenSubscriptionResourceNames(profileID, resourceOrderTypeGroups)
		filterYAMLSequenceByName(proxyGroupsNode, hiddenGroups, customGroupNameSet(customGroups))
	}

	// 获取数据库里的自定义组并无损注入
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

		var filteredProxies []string
		var excludeRegex *regexp.Regexp
		if cg.Exclude != "" {
			excludeRegex, _ = regexp.Compile(cg.Exclude)
		}

		seen := make(map[string]bool)
		for _, p := range finalProxies {
			if excludeRegex != nil && excludeRegex.MatchString(p) {
				continue // 被正则表达式匹配排除
			}
			if !seen[p] {
				seen[p] = true
				filteredProxies = append(filteredProxies, p)
			}
		}

		proxiesSeq := &yaml.Node{Kind: yaml.SequenceNode}
		for _, p := range filteredProxies {
			proxiesSeq.Content = append(proxiesSeq.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: p})
		}

		groupMap := proxyGroupYAMLNode(cg, proxiesSeq)

		proxyGroupsNode.Content = append(proxyGroupsNode.Content, groupMap)
	}

	// 自动清理因 dialer-proxy 引用自身代理组而产生的闭环
	availableNames := yamlSequenceNameSet(proxiesNode)
	for _, groupNode := range proxyGroupsNode.Content {
		if name := yamlMappingName(groupNode); name != "" {
			availableNames[name] = true
		}
	}
	pruneProxyGroupMembers(proxyGroupsNode, availableNames)
	autoPruneDialerProxyLoops(proxiesNode, proxyGroupsNode)

	// 转回 YAML 字符串（保持原格式和注释）
	out, err := yaml.Marshal(&root)
	if err != nil {
		log.Printf("YAML marshal warning: %v", err)
		return yamlContent
	}
	return string(out)
}

func shouldOverrideProfileProxyGroups(profileID uint, customGroupCount int) bool {
	if customGroupCount == 0 {
		return false
	}
	var profile SubscriptionProfile
	if err := DB.Select("id", "groups_mode").First(&profile, profileID).Error; err != nil {
		return false
	}
	return profile.GroupsMode == profileGroupsModeOverride
}

func proxyGroupYAMLNode(group CustomProxyGroup, proxiesSeq *yaml.Node) *yaml.Node {
	groupMap := &yaml.Node{Kind: yaml.MappingNode}
	appendScalarField := func(key, value string) {
		groupMap.Content = append(groupMap.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: key},
			&yaml.Node{Kind: yaml.ScalarNode, Value: value},
		)
	}

	appendScalarField("name", group.Name)
	appendScalarField("type", group.Type)
	groupMap.Content = append(groupMap.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "proxies"},
		proxiesSeq,
	)

	if strings.TrimSpace(group.Extra) == "" {
		return groupMap
	}
	var extra map[string]interface{}
	if err := json.Unmarshal([]byte(group.Extra), &extra); err != nil {
		return groupMap
	}
	for key, value := range extra {
		key = strings.TrimSpace(key)
		if key == "" || copiedGroupProviderScopedExtraKeys[key] {
			continue
		}
		switch key {
		case "name", "type", "proxies":
			continue
		}
		valueNode := &yaml.Node{}
		if err := valueNode.Encode(value); err != nil {
			continue
		}
		groupMap.Content = append(groupMap.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: key},
			valueNode,
		)
	}
	return groupMap
}

func applyProfileResourceOrderToYAML(yamlContent string, profileID uint, resourceType string) string {
	orderNames := loadProfileResourceOrderNames(profileID, resourceType)
	return applyResourceOrderToYAMLContent(yamlContent, resourceType, orderNames)
}

func applyResourceOrderToYAMLContent(yamlContent string, resourceType string, orderNames []string) string {
	cleanedOrder := cleanResourceOrderNames(orderNames)
	if len(cleanedOrder) == 0 {
		return yamlContent
	}

	yamlKey, ok := resourceOrderYAMLKey(resourceType)
	if !ok {
		return yamlContent
	}

	var root yaml.Node
	if err := yaml.Unmarshal([]byte(yamlContent), &root); err != nil {
		log.Printf("YAML unmarshal warning in applyResourceOrderToYAMLContent: %v", err)
		return yamlContent
	}
	if len(root.Content) == 0 {
		return yamlContent
	}

	docNode := root.Content[0]
	if docNode.Kind != yaml.MappingNode {
		return yamlContent
	}

	seq := findTopLevelSequenceNode(docNode, yamlKey)
	if seq == nil {
		return yamlContent
	}

	applyYAMLSequenceOrderByName(seq, cleanedOrder)
	out, err := yaml.Marshal(&root)
	if err != nil {
		log.Printf("YAML marshal warning in applyResourceOrderToYAMLContent: %v", err)
		return yamlContent
	}
	return string(out)
}

func resourceOrderYAMLKey(resourceType string) (string, bool) {
	switch resourceType {
	case resourceOrderTypeNodes:
		return "proxies", true
	case resourceOrderTypeGroups:
		return "proxy-groups", true
	default:
		return "", false
	}
}

func findTopLevelSequenceNode(docNode *yaml.Node, key string) *yaml.Node {
	for i := 0; i < len(docNode.Content)-1; i += 2 {
		if docNode.Content[i].Value == key && docNode.Content[i+1].Kind == yaml.SequenceNode {
			return docNode.Content[i+1]
		}
	}
	return nil
}

func applyYAMLSequenceOrderByName(seq *yaml.Node, orderNames []string) {
	byName := make(map[string][]*yaml.Node, len(seq.Content))
	for _, item := range seq.Content {
		if name := yamlMappingName(item); name != "" {
			byName[name] = append(byName[name], item)
		}
	}

	ordered := make([]*yaml.Node, 0, len(seq.Content))
	usedNames := make(map[string]bool, len(orderNames))
	for _, name := range orderNames {
		if matched, ok := byName[name]; ok {
			ordered = append(ordered, matched...)
			usedNames[name] = true
		}
	}

	for _, item := range seq.Content {
		name := yamlMappingName(item)
		if name == "" || !usedNames[name] {
			ordered = append(ordered, item)
		}
	}
	seq.Content = ordered
}

func filterYAMLSequenceByName(seq *yaml.Node, hiddenNames map[string]bool, overrideNames map[string]bool) {
	if seq == nil || seq.Kind != yaml.SequenceNode || (len(hiddenNames) == 0 && len(overrideNames) == 0) {
		return
	}
	filtered := make([]*yaml.Node, 0, len(seq.Content))
	for _, item := range seq.Content {
		name := yamlMappingName(item)
		if name != "" && (hiddenNames[name] || overrideNames[name]) {
			continue
		}
		filtered = append(filtered, item)
	}
	seq.Content = filtered
}

func yamlMappingName(node *yaml.Node) string {
	if node == nil || node.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i < len(node.Content)-1; i += 2 {
		if node.Content[i].Value == "name" {
			return strings.TrimSpace(node.Content[i+1].Value)
		}
	}
	return ""
}

func yamlSequenceNameSet(seq *yaml.Node) map[string]bool {
	names := map[string]bool{}
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return names
	}
	for _, item := range seq.Content {
		if name := yamlMappingName(item); name != "" {
			names[name] = true
		}
	}
	return names
}

func customNodeNameSet(nodes []CustomNode) map[string]bool {
	names := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		if name := strings.TrimSpace(node.Name); name != "" {
			names[name] = true
		}
	}
	return names
}

func customGroupNameSet(groups []CustomProxyGroup) map[string]bool {
	names := make(map[string]bool, len(groups))
	for _, group := range groups {
		if name := strings.TrimSpace(group.Name); name != "" {
			names[name] = true
		}
	}
	return names
}

func loadHiddenSubscriptionResourceNames(profileID uint, resourceType string) map[string]bool {
	names, err := GetHiddenSubscriptionResourceNames(profileID, resourceType)
	if err != nil {
		log.Printf("load hidden resource warning: profile=%d type=%s err=%v", profileID, resourceType, err)
		return map[string]bool{}
	}
	return names
}

func pruneProxyGroupMembers(proxyGroupsNode *yaml.Node, availableNames map[string]bool) {
	if proxyGroupsNode == nil || proxyGroupsNode.Kind != yaml.SequenceNode {
		return
	}
	for _, groupNode := range proxyGroupsNode.Content {
		if groupNode.Kind != yaml.MappingNode {
			continue
		}
		var proxiesSeq *yaml.Node
		for i := 0; i < len(groupNode.Content)-1; i += 2 {
			if groupNode.Content[i].Value == "proxies" {
				proxiesSeq = groupNode.Content[i+1]
				break
			}
		}
		if proxiesSeq == nil || proxiesSeq.Kind != yaml.SequenceNode {
			continue
		}
		filtered := make([]*yaml.Node, 0, len(proxiesSeq.Content))
		for _, item := range proxiesSeq.Content {
			member := strings.TrimSpace(item.Value)
			if member == "" || availableNames[member] || isBuiltInProxyPolicy(member) {
				filtered = append(filtered, item)
			}
		}
		proxiesSeq.Content = filtered
	}
}

// injectCustomRules 基于 yaml.v3 Node 树完成分流规则的强力接管、前置注入与原生去重
func injectCustomRules(yamlContent string, profileID uint) string {
	customRules, _ := GetCustomRules(profileID)
	return injectCustomRulesWithRules(yamlContent, customRules)
}

func isTerminalRuleType(ruleType string) bool {
	switch strings.ToUpper(strings.TrimSpace(ruleType)) {
	case "MATCH", "FINAL":
		return true
	default:
		return false
	}
}

func splitRuleParts(ruleLine string) []string {
	parts := strings.Split(strings.TrimSpace(ruleLine), ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func customRuleLineAndFingerprint(rule CustomRule) (string, string) {
	ruleType := strings.TrimSpace(rule.Type)
	payload := strings.TrimSpace(rule.Payload)
	target := strings.TrimSpace(rule.Target)
	normalizedType := strings.ToUpper(ruleType)
	if payload == "-" || payload == "" {
		return fmt.Sprintf("%s,%s", ruleType, target), normalizedType
	}
	return fmt.Sprintf("%s,%s,%s", ruleType, payload, target), fmt.Sprintf("%s,%s", normalizedType, payload)
}

func ruleFingerprintFromParts(parts []string) string {
	if len(parts) == 0 || parts[0] == "" {
		return ""
	}
	ruleType := strings.ToUpper(parts[0])
	if isTerminalRuleType(ruleType) {
		return ruleType
	}
	if len(parts) >= 3 {
		return fmt.Sprintf("%s,%s", ruleType, parts[1])
	}
	if len(parts) == 2 {
		return ruleType
	}
	return strings.Join(parts, ",")
}

func ruleTargetPolicyFromParts(parts []string) string {
	if len(parts) < 2 || parts[0] == "" {
		return ""
	}
	if isTerminalRuleType(parts[0]) {
		return strings.TrimSpace(strings.Join(parts[1:], ","))
	}
	if len(parts) < 3 {
		return ""
	}
	targetIndex := len(parts) - 1
	if isRuleOptionSuffix(parts[len(parts)-1]) && len(parts) >= 4 {
		targetIndex = len(parts) - 2
	}
	return strings.TrimSpace(parts[targetIndex])
}

func yamlDocumentPolicyTargetNames(docNode *yaml.Node) map[string]bool {
	names := yamlSequenceNameSet(findTopLevelSequenceNode(docNode, "proxies"))
	for name := range yamlSequenceNameSet(findTopLevelSequenceNode(docNode, "proxy-groups")) {
		names[name] = true
	}
	return names
}

func ruleTargetPolicyAvailable(policy string, availableNames map[string]bool) bool {
	policy = strings.TrimSpace(policy)
	return policy == "" || isBuiltInProxyPolicy(policy) || availableNames[policy]
}

func injectCustomRulesWithRules(yamlContent string, customRules []CustomRule) string {
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(yamlContent), &root); err != nil {
		log.Printf("YAML unmarshal warning in injectCustomRules: %v", err)
		return yamlContent
	}

	if len(root.Content) == 0 {
		return yamlContent
	}
	docNode := root.Content[0]
	if docNode.Kind != yaml.MappingNode {
		return yamlContent
	}

	// 寻找 rules 节点
	var rulesNode *yaml.Node
	for i := 0; i < len(docNode.Content); i += 2 {
		if docNode.Content[i].Value == "rules" {
			rulesNode = docNode.Content[i+1]
			break
		}
	}

	// 如果没有 rules 节点，创建一个新的
	if rulesNode == nil {
		rulesNode = &yaml.Node{Kind: yaml.SequenceNode}
		docNode.Content = append(docNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "rules"},
			rulesNode,
		)
	} else if rulesNode.Kind != yaml.SequenceNode {
		return yamlContent
	}

	// 1. 获取所有自定义规则并建立指纹映射，用于后续原生规则的去重剥离
	customFingerprints := make(map[string]bool)
	var customRuleNodes []*yaml.Node
	var customTerminalRuleNodes []*yaml.Node
	hasCustomTerminalRule := false
	availableTargets := yamlDocumentPolicyTargetNames(docNode)

	for _, cr := range customRules {
		ruleStr, fingerprint := customRuleLineAndFingerprint(cr)
		customFingerprints[fingerprint] = true
		isTerminalRule := isTerminalRuleType(cr.Type)
		if isTerminalRule {
			hasCustomTerminalRule = true
		}
		if cr.Target == deletedCustomRuleTarget {
			continue
		}
		ruleNode := &yaml.Node{
			Kind:  yaml.ScalarNode,
			Value: ruleStr,
		}
		if isTerminalRule {
			customTerminalRuleNodes = append(customTerminalRuleNodes, ruleNode)
			continue
		}
		customRuleNodes = append(customRuleNodes, ruleNode)
	}

	// 2. 遍历原有订阅规则，剥离掉与自定义规则指纹冲突的部分（实现覆盖）
	var filteredOriginalRules []*yaml.Node
	var originalTerminalRules []*yaml.Node
	for _, rn := range rulesNode.Content {
		// 解析原生规则指纹
		parts := splitRuleParts(rn.Value)
		fingerprint := ruleFingerprintFromParts(parts)
		isTerminalRule := len(parts) > 0 && isTerminalRuleType(parts[0])
		if fingerprint != "" && customFingerprints[fingerprint] {
			continue
		}
		if !ruleTargetPolicyAvailable(ruleTargetPolicyFromParts(parts), availableTargets) {
			continue
		}
		if isTerminalRule {
			if hasCustomTerminalRule {
				continue
			}
			originalTerminalRules = append(originalTerminalRules, rn)
			continue
		}
		filteredOriginalRules = append(filteredOriginalRules, rn)
	}

	// 3. 普通自定义规则前置，MATCH/FINAL 等终止规则始终保留在末尾兜底。
	rulesNode.Content = append(customRuleNodes, filteredOriginalRules...)
	if hasCustomTerminalRule {
		rulesNode.Content = append(rulesNode.Content, customTerminalRuleNodes...)
	} else {
		rulesNode.Content = append(rulesNode.Content, originalTerminalRules...)
	}

	// 转回 YAML 字符串
	out, err := yaml.Marshal(&root)
	if err != nil {
		log.Printf("YAML marshal warning in injectCustomRules: %v", err)
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
	var bestCandidate subscriptionFetchCandidate
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

		candidate := newSubscriptionFetchCandidate(strategy.Name, string(bodyBytes))
		log.Printf("[Clash-Proxy selector] strategy=%s format=%s nodes=%d score=%d", candidate.Strategy, candidate.Format, candidate.NodeCount, candidate.Score)
		if isBetterSubscriptionFetchCandidate(candidate, bestCandidate) {
			bestCandidate = candidate
		}
		if candidate.NodeCount >= preferredSubscriptionNodeCount {
			return candidate.Body, nil
		}
	}

	if bestCandidate.Body != "" {
		return bestCandidate.Body, nil
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

const preferredSubscriptionNodeCount = 20

type subscriptionFetchCandidate struct {
	Strategy  string
	Body      string
	Format    string
	Score     int
	NodeCount int
}

func newSubscriptionFetchCandidate(strategyName string, body string) subscriptionFetchCandidate {
	candidate := subscriptionFetchCandidate{
		Strategy: strategyName,
		Body:     body,
		Format:   "unsupported",
		Score:    1,
	}

	normalizedContent, err := decodeSubscriptionPlainContent(body)
	if err != nil {
		return candidate
	}

	candidate.NodeCount = countClashProxyNodes(normalizedContent)
	candidate.Format = "clash-yaml"
	candidate.Score = 1000 + candidate.NodeCount
	if candidate.NodeCount > 0 {
		candidate.Score += 10000
	}
	return candidate
}

func isBetterSubscriptionFetchCandidate(candidate, current subscriptionFetchCandidate) bool {
	if current.Body == "" {
		return true
	}
	if candidate.Score != current.Score {
		return candidate.Score > current.Score
	}
	if candidate.NodeCount != current.NodeCount {
		return candidate.NodeCount > current.NodeCount
	}
	return len(candidate.Body) > len(current.Body)
}

func countClashProxyNodes(content string) int {
	var cfg struct {
		Proxies []map[string]interface{} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal([]byte(content), &cfg); err != nil {
		return 0
	}
	return len(cfg.Proxies)
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

func decodeSubscriptionPlainContent(rawResponse string) (string, error) {
	decodedContent, err := decodeAdaptiveBase64(rawResponse)
	if err != nil {
		if looksLikePlainSubscriptionConfig(rawResponse) {
			decodedContent = rawResponse
		} else {
			decodedContent = rawResponse
		}
	}

	if looksLikePlainSubscriptionConfig(decodedContent) {
		return unescapeSubscriptionLines(decodedContent), nil
	}

	if convertedContent, convertErr := convertProxyURIListToClashYAML(decodedContent); convertErr == nil {
		return convertedContent, nil
	}

	unescapedContent := unescapeSubscriptionLines(decodedContent)
	if convertedContent, convertErr := convertProxyURIListToClashYAML(unescapedContent); convertErr == nil {
		return convertedContent, nil
	}

	if err != nil {
		return "", fmt.Errorf("解析失败，且不包含常见配置文件特征，目标地址返回的内容格式不支持: %v", err)
	}
	return "", fmt.Errorf("解析失败，目标地址返回的内容既不是 Clash YAML，也不是可识别的节点 URI 列表")
}

func unescapeSubscriptionLines(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if unescaped, err := url.PathUnescape(strings.TrimSpace(line)); err == nil {
			lines[i] = unescaped
		}
	}
	return strings.Join(lines, "\n")
}

func convertProxyURIListToClashYAML(content string) (string, error) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	proxies := make([]map[string]interface{}, 0, len(lines))
	nodeNames := make([]string, 0, len(lines))
	usedNames := make(map[string]int)

	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "://") {
			continue
		}

		parsedProxy, err := ParseProxyLink(line)
		if err != nil {
			log.Printf("skip unsupported proxy uri: %v", err)
			continue
		}

		name := uniqueProxyName(parsedProxy.Name, usedNames)
		if name != parsedProxy.Name {
			parsedProxy.Config["name"] = name
		}
		proxies = append(proxies, parsedProxy.Config)
		nodeNames = append(nodeNames, name)
	}

	if len(proxies) == 0 {
		return "", fmt.Errorf("未找到可转换的节点 URI")
	}

	cfg := manualProfileConfig{
		MixedPort:   manualDefaultMixedPort,
		AllowLan:    true,
		Mode:        "rule",
		LogLevel:    "info",
		GeodataMode: true,
		Proxies:     proxies,
		ProxyGroups: []manualProxyGroupConfig{defaultManualProxyGroup(nodeNames)},
		Rules: []string{
			defaultGeositeDirectRule,
			defaultGeoIPDirectRule,
			defaultProxyMatchRule,
		},
	}

	out, err := yaml.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("生成 Clash YAML 失败: %w", err)
	}
	return string(out), nil
}

func uniqueProxyName(name string, usedNames map[string]int) string {
	baseName := strings.TrimSpace(name)
	if baseName == "" {
		baseName = "Proxy"
	}

	count := usedNames[baseName]
	usedNames[baseName] = count + 1
	if count == 0 {
		return baseName
	}
	return fmt.Sprintf("%s %d", baseName, count+1)
}

// truncateString 截断字符串，防止返回大文件时撑爆网络通道
func truncateString(str string, length int) string {
	if len(str) <= length {
		return str
	}
	return str[:length] + "...(已截断)"
}

// autoPruneDialerProxyLoops 自动清理因 dialer-proxy 引用自身代理组而产生的闭环
func autoPruneDialerProxyLoops(proxiesNode, proxyGroupsNode *yaml.Node) {
	if proxiesNode == nil || proxiesNode.Kind != yaml.SequenceNode {
		return
	}
	if proxyGroupsNode == nil || proxyGroupsNode.Kind != yaml.SequenceNode {
		return
	}

	// 1. 获取所有节点与其 dialer-proxy 的关系
	// nodeDialerMap[nodeName] = dialerProxyName
	nodeDialerMap := make(map[string]string)
	for _, proxyNode := range proxiesNode.Content {
		if proxyNode.Kind != yaml.MappingNode {
			continue
		}
		var name, dialerProxy string
		for i := 0; i < len(proxyNode.Content); i += 2 {
			key := proxyNode.Content[i].Value
			valNode := proxyNode.Content[i+1]
			switch key {
			case "name":
				name = valNode.Value
			case "dialer-proxy":
				dialerProxy = valNode.Value
			}
		}
		if name != "" && dialerProxy != "" {
			nodeDialerMap[name] = dialerProxy
		}
	}

	// 2. 遍历所有的代理组
	for _, groupNode := range proxyGroupsNode.Content {
		if groupNode.Kind != yaml.MappingNode {
			continue
		}
		var groupName string
		var proxiesSeq *yaml.Node
		for i := 0; i < len(groupNode.Content); i += 2 {
			key := groupNode.Content[i].Value
			valNode := groupNode.Content[i+1]
			switch key {
			case "name":
				groupName = valNode.Value
			case "proxies":
				proxiesSeq = valNode
			}
		}

		if groupName != "" && proxiesSeq != nil && proxiesSeq.Kind == yaml.SequenceNode {
			// 在这个代理组的候选列表中，移除所有拨号前置指向本组的节点
			var newContent []*yaml.Node
			for _, node := range proxiesSeq.Content {
				nodeName := node.Value
				// 如果该节点的拨号前置正是当前正在遍历的代理组，就不要把它加进来（避免闭环）
				if nodeDialerMap[nodeName] == groupName {
					continue
				}
				newContent = append(newContent, node)
			}
			proxiesSeq.Content = newContent
		}
	}
}

// ProcessSubscriptionRawData 集中处理订阅的解码、明文转换及各种自定义规则的注入 (DRY 原则)
func ProcessSubscriptionRawData(rawResponse string, profileID uint) (string, error) {
	decodedContent, err := decodeSubscriptionPlainContent(rawResponse)
	if err != nil {
		return "", err
	}

	decodedContent = injectCustomNodes(decodedContent, profileID)
	decodedContent = applyProfileResourceOrderToYAML(decodedContent, profileID, resourceOrderTypeNodes)
	decodedContent = injectCustomGroups(decodedContent, profileID)
	decodedContent = injectCustomRules(decodedContent, profileID)
	decodedContent = applyProfileResourceOrderToYAML(decodedContent, profileID, resourceOrderTypeGroups)

	return decodedContent, nil
}

// ReapplyRulesToProfile 对指定配置缓存重新应用自定义节点、策略组和规则。
func ReapplyRulesToProfile(profileID uint) {
	var profile SubscriptionProfile
	if err := DB.First(&profile, profileID).Error; err != nil {
		return
	}
	if profile.SourceType == profileSourceLocal {
		if manualYAML, err := BuildManualProfileYAML(profile.ID); err == nil {
			profile.RawResponse = manualYAML
			profile.Decoded = manualYAML
			DB.Save(&profile)
		}
		return
	}

	rawContent := profile.RawResponse
	if strings.TrimSpace(rawContent) == "" {
		return
	}
	if decodedContent, err := ProcessSubscriptionRawData(rawContent, profile.ID); err == nil {
		profile.Decoded = decodedContent
		DB.Save(&profile)
	}
}

func ReapplyRulesToAllProfiles() {
	var profiles []SubscriptionProfile
	if err := DB.Find(&profiles).Error; err != nil {
		return
	}
	for _, profile := range profiles {
		ReapplyRulesToProfile(profile.ID)
	}
}

// handleGetSubToken 获取当前有效的订阅 Token，不创建新 Token
func handleGetSubToken(c *gin.Context) {
	profile, err := getDefaultProfile()
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "暂无可用配置，请先创建配置"})
		return
	}

	hasToken := profile.SubToken != ""
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "获取成功",
		"data": gin.H{
			"token":     profile.SubToken,
			"has_token": hasToken,
			"profile":   profileListItem(profile),
		},
	})
}

// handleGenerateSubToken 生成新的订阅 Token 并作废旧 Token
func handleGenerateSubToken(c *gin.Context) {
	username, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "未登录"})
		return
	}

	var user User
	if err := DB.Where("username = ?", username).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "用户不存在"})
		return
	}

	profile, err := getDefaultProfile()
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "暂无可用配置，请先创建配置"})
		return
	}

	token := generateProfileSubToken(profile)
	profile.SubToken = token
	user.SubToken = token
	if err := DB.Save(&profile).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "保存配置 Token 失败"})
		return
	}
	if err := DB.Save(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "保存 Token 失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "Token 生成成功", "data": gin.H{"token": token, "profile": profileListItem(profile)}})
}

// handleFinalSubscription 最终订阅地址接口
func handleFinalSubscription(c *gin.Context) {
	format := strings.ToLower(strings.TrimSpace(c.DefaultQuery("format", "clash")))
	if format == "" {
		format = "clash"
	}
	if !isSupportedSubscriptionFormat(format) {
		c.String(http.StatusBadRequest, "Unsupported subscription format: "+format)
		return
	}

	serveSubscriptionByFormat(c, format)
}

// handleShadowrocketSubscription 使用明确的 .conf 路径输出 Shadowrocket 配置文件。
func handleShadowrocketSubscription(c *gin.Context) {
	serveSubscriptionByFormat(c, "shadowrocket")
}

// handleSurgeSubscription 使用明确的 .conf 路径输出 Surge 最新版配置文件。
func handleSurgeSubscription(c *gin.Context) {
	serveSubscriptionByFormat(c, "surge")
}

// handleSurge576Subscription 使用明确的 .conf 路径输出 Surge 5.7.6 兼容配置文件。
func handleSurge576Subscription(c *gin.Context) {
	serveSubscriptionByFormat(c, "surge-5.7.6")
}

func handleShadowrocketPathSubscription(c *gin.Context) {
	token, err := parseShadowrocketConfigToken(c.Param("tokenFile"))
	if err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	serveSubscriptionByToken(c, "shadowrocket", token)
}

func handleShadowrocketInstall(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.String(http.StatusUnauthorized, "Missing token")
		return
	}

	if _, err := findProfileBySubToken(token); err != nil {
		c.String(http.StatusUnauthorized, "Invalid or expired token")
		return
	}

	configURL := buildAbsoluteRequestURL(c, "/shadowrocket/config/"+url.PathEscape(token)+".conf")
	installURL := "shadowrocket://config/add/" + configURL
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(buildShadowrocketInstallHTML(installURL)))
}

func parseShadowrocketConfigToken(tokenFile string) (string, error) {
	if !strings.HasSuffix(strings.ToLower(tokenFile), ".conf") {
		return "", fmt.Errorf("invalid Shadowrocket config filename")
	}

	escapedToken := strings.TrimSuffix(tokenFile, ".conf")
	token, err := url.PathUnescape(escapedToken)
	if err != nil {
		return "", fmt.Errorf("invalid Shadowrocket config token")
	}
	if strings.TrimSpace(token) == "" {
		return "", fmt.Errorf("missing Shadowrocket config token")
	}
	return token, nil
}

func buildShadowrocketInstallHTML(installURL string) string {
	return fmt.Sprintf(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>安装到 Shadowrocket</title>
  <meta http-equiv="refresh" content="0;url=%s">
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; padding: 24px; line-height: 1.6; color: #111827; }
    a { color: #16a34a; font-weight: 700; }
  </style>
</head>
<body>
  <p>正在打开 Shadowrocket 安装配置...</p>
  <p>如果没有自动跳转，请点击：<a href="%s">安装到 Shadowrocket</a></p>
  <script>window.location.replace(%q);</script>
</body>
</html>`, html.EscapeString(installURL), html.EscapeString(installURL), installURL)
}

func isSupportedSubscriptionFormat(format string) bool {
	switch format {
	case "clash", "shadowrocket", "surge", "surge-5.7.6":
		return true
	default:
		return false
	}
}

func buildAbsoluteRequestURL(c *gin.Context, path string) string {
	scheme := c.GetHeader("X-Forwarded-Proto")
	if scheme == "" {
		if c.Request.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}

	host := c.GetHeader("X-Forwarded-Host")
	if host == "" {
		host = c.Request.Host
	}

	return scheme + "://" + host + path
}

func serveSubscriptionByFormat(c *gin.Context, format string) {
	token := c.Query("token")
	if token == "" {
		c.String(http.StatusUnauthorized, "Missing token")
		return
	}
	serveSubscriptionByToken(c, format, token)
}

func findProfileBySubToken(token string) (SubscriptionProfile, error) {
	var profile SubscriptionProfile
	if err := DB.Where("sub_token = ?", token).First(&profile).Error; err == nil {
		return profile, nil
	}

	var user User
	if err := DB.Where("sub_token = ?", token).First(&user).Error; err != nil {
		return SubscriptionProfile{}, err
	}
	return getDefaultProfile()
}

func serveSubscriptionByToken(c *gin.Context, format string, token string) {
	profile, err := findProfileBySubToken(token)
	if err != nil {
		c.String(http.StatusUnauthorized, "Invalid or expired token")
		return
	}

	if err := refreshProfileCache(&profile); err != nil {
		if profile.Decoded != "" {
			serveFinalSubscription(c, format, profile.Decoded)
			return
		}
		c.String(http.StatusBadGateway, "Failed to build subscription: "+err.Error())
		return
	}

	serveFinalSubscription(c, format, profile.Decoded)
}

func serveFinalSubscription(c *gin.Context, format string, clashYAML string) {
	if format == "shadowrocket" {
		shadowrocketConfig, err := ConvertClashYAMLToShadowrocket(clashYAML)
		if err != nil {
			c.String(http.StatusUnprocessableEntity, "Failed to generate Shadowrocket config: "+err.Error())
			return
		}
		c.Header("Content-Disposition", "attachment; filename=\"shadowrocket.conf\"")
		c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(shadowrocketConfig))
		return
	}

	if strings.HasPrefix(format, "surge") {
		surgeConfig, filename, err := convertFinalSubscriptionToSurge(format, clashYAML)
		if err != nil {
			c.String(http.StatusUnprocessableEntity, "Failed to generate Surge config: "+err.Error())
			return
		}
		c.Header("Content-Disposition", "attachment; filename=\""+filename+"\"")
		c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(surgeConfig))
		return
	}

	c.Data(http.StatusOK, "text/yaml; charset=utf-8", []byte(clashYAML))
}

func convertFinalSubscriptionToSurge(format string, clashYAML string) (string, string, error) {
	if format == "surge-5.7.6" {
		config, err := ConvertClashYAMLToSurge576(clashYAML)
		return config, "surge-5.7.6.conf", err
	}

	config, err := ConvertClashYAMLToSurge(clashYAML)
	return config, "surge.conf", err
}
