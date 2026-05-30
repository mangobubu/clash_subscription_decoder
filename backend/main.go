package main

import (
	"crypto/tls"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/color"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/crypto/bcrypt"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/mojocn/base64Captcha"
	"gorm.io/gorm"
	"gopkg.in/yaml.v3"
)


//go:embed dist
var frontendFS embed.FS

// DecodeRequest 定义解码请求的参数结构
type DecodeRequest struct {
	URL string `json:"url" binding:"required"`
}

// DecodeResponse 定义解码成功的响应结构
type DecodeResponse struct {
	URL         string `json:"url"`
	RawResponse string `json:"raw_response"`
	Decoded     string `json:"decoded"`
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
	r.GET("/sub", handleFinalSubscription) // 最终订阅地址

	// 需要认证的接口组
	api := r.Group("/api")
	api.Use(AuthMiddleware())
	
	// 注册验证接口
	api.GET("/verify", handleVerify)
	api.POST("/logout", handleLogout)
	api.POST("/change-password", handleChangePassword)
	api.POST("/generate-sub-token", handleGenerateSubToken)

	// 数据备份与导入恢复
	api.GET("/backup", handleBackup)
	api.POST("/import", handleImport)

	// 注册解码接口
	api.POST("/decode", handleDecode)
	
	// 注册获取最新订阅接口
	api.GET("/subscription", handleGetSubscription)
	
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

		// 如果是后端 API 或订阅请求但没匹配到，走 404 处理，不要 fallback 返回 index.html
		if strings.HasPrefix(path, "/api") || strings.HasPrefix(path, "/sub") {
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

func parseColor(s string) *color.RGBA {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "rgba") {
		s = strings.ReplaceAll(s, " ", "")
		var r, g, b uint8
		var a float32
		fmt.Sscanf(s, "rgba(%d,%d,%d,%f)", &r, &g, &b, &a)
		return &color.RGBA{R: r, G: g, B: b, A: uint8(a * 255)}
	} else if strings.HasPrefix(s, "#") {
		s = strings.TrimPrefix(s, "#")
		if len(s) == 6 {
			var r, g, b uint8
			fmt.Sscanf(s, "%02x%02x%02x", &r, &g, &b)
			return &color.RGBA{R: r, G: g, B: b, A: 255}
		}
		if len(s) == 8 {
			var r, g, b, a uint8
			fmt.Sscanf(s, "%02x%02x%02x%02x", &r, &g, &b, &a)
			return &color.RGBA{R: r, G: g, B: b, A: a}
		}
	}
	return &color.RGBA{0, 0, 0, 0}
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
	
	driver := &base64Captcha.DriverMath{
		Height:          AppConfig.Auth.CaptchaHeight,
		Width:           AppConfig.Auth.CaptchaWidth,
		NoiseCount:      0,
		ShowLineOptions: 0,
		BgColor:         parseColor(AppConfig.Auth.CaptchaBgColor),
	}
	cp := base64Captcha.NewCaptcha(driver, captchaStore)
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

type BackupData struct {
	Groups []CustomProxyGroup `json:"groups"`
	Nodes  []CustomNode       `json:"nodes"`
	Rules  []CustomRule       `json:"rules"`
}

func handleBackup(c *gin.Context) {
	var data BackupData
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

		for i := range data.Groups {
			data.Groups[i].ID = 0
		}
		for i := range data.Nodes {
			data.Nodes[i].ID = 0
		}
		for i := range data.Rules {
			data.Rules[i].ID = 0
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
		return nil
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "导入失败，数据库事务已回滚: " + err.Error()})
		return
	}

	ReapplyRulesToLatestSubscription()
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

	// 1. 发起请求获取远端配置内容 (可能是 Base64 也可能是明文 YAML)
	rawResponse, err := fetchURLContent(targetURL)
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
	decoded, err := decodeAdaptiveBase64(rawResponse)
	if err != nil {
		// 也许它本身就是明文的配置文件 (如 YAML)? 我们判断一下有没有常见的配置特征 (KISS 原则: 灵活兼容)
		if strings.Contains(rawResponse, "proxies:") || strings.Contains(rawResponse, "outbounds:") || strings.Contains(rawResponse, "servers:") {
			decodedContent = rawResponse
		} else {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"code":    422,
				"message": "解析失败，且不包含常见配置文件特征，目标地址返回的内容格式不支持",
				"error":   err.Error(),
				"preview": truncateString(rawResponse, 200), // 提供前200个字符用于排错
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

	// 🌟 AST 无损智能覆盖逻辑：自动将自定义分流规则优先注入并实现订阅规则覆盖去重
	decodedContent = injectCustomRules(decodedContent)

	// 保存至数据库 Subscription 表
	var sub Subscription
	if err := DB.Where("url = ?", targetURL).First(&sub).Error; err != nil {
		sub = Subscription{URL: targetURL}
	}
	sub.RawResponse = rawResponse
	sub.Decoded = decodedContent
	DB.Save(&sub)

	// 3. 成功返回
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "success",
		"data": DecodeResponse{
			URL:         targetURL,
			RawResponse: truncateString(rawResponse, 1000), // 限制原始响应返回长度，避免传输体过大
			Decoded:     decodedContent,
		},
	})
}

// handleGetSubscription 获取最新保存的订阅配置
func handleGetSubscription(c *gin.Context) {
	var sub Subscription
	if err := DB.Order("updated_at desc").First(&sub).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "暂无保存的订阅"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": DecodeResponse{
			URL:         sub.URL,
			RawResponse: truncateString(sub.RawResponse, 1000),
			Decoded:     sub.Decoded,
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
		Exclude string   `json:"exclude"`
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
		Exclude: req.Exclude,
	}
	if err := DB.Create(&group).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "保存失败", "error": err.Error()})
		return
	}
	ReapplyRulesToLatestSubscription()
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

	group.Name = req.Name
	group.Type = req.Type
	group.Proxies = string(proxiesBytes)
	group.Exclude = req.Exclude

	if err := DB.Save(&group).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "更新失败", "error": err.Error()})
		return
	}
	ReapplyRulesToLatestSubscription()
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "更新成功", "data": group})
}

// handleDeleteCustomGroup 删除自定义组
func handleDeleteCustomGroup(c *gin.Context) {
	id := c.Param("id")
	if err := DB.Delete(&CustomProxyGroup{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "删除失败", "error": err.Error()})
		return
	}
	ReapplyRulesToLatestSubscription()
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
	ReapplyRulesToLatestSubscription()
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "保存成功", "data": node})
}

// handleDeleteCustomNode 删除自定义节点
func handleDeleteCustomNode(c *gin.Context) {
	id := c.Param("id")
	if err := DB.Delete(&CustomNode{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "删除失败", "error": err.Error()})
		return
	}
	ReapplyRulesToLatestSubscription()
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

	node.Name = req.Name
	node.Type = req.Type
	node.Server = req.Server
	node.Port = req.Port
	node.Config = string(configBytes)

	if err := DB.Save(&node).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "更新失败", "error": err.Error()})
		return
	}
	ReapplyRulesToLatestSubscription()
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "更新成功", "data": node})
}

// handleGetCustomRules 获取所有自定义规则
func handleGetCustomRules(c *gin.Context) {
	rules, err := GetCustomRules()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "获取失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": rules})
}

// handleCreateCustomRule 创建或保存自定义规则 (支持按 Type 和 Payload 进行 Upsert 智能覆盖)
func handleCreateCustomRule(c *gin.Context) {
	var req struct {
		Type    string `json:"type" binding:"required"`
		Payload string `json:"payload" binding:"required"`
		Target  string `json:"target" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数错误"})
		return
	}

	var existingRule CustomRule
	if err := DB.Where("type = ? AND payload = ?", req.Type, req.Payload).First(&existingRule).Error; err == nil {
		// 已存在相同的拦截条件，执行覆盖更新
		existingRule.Target = req.Target
		if err := DB.Save(&existingRule).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "覆盖更新失败", "error": err.Error()})
			return
		}
		ReapplyRulesToLatestSubscription()
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "规则已覆盖更新", "data": existingRule})
		return
	}

	// 不存在，创建新规则
	rule := CustomRule{
		Type:    req.Type,
		Payload: req.Payload,
		Target:  req.Target,
	}
	if err := DB.Create(&rule).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "保存失败", "error": err.Error()})
		return
	}
	ReapplyRulesToLatestSubscription()
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
	ReapplyRulesToLatestSubscription()
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "更新成功", "data": rule})
}

// handleDeleteCustomRule 删除自定义规则
func handleDeleteCustomRule(c *gin.Context) {
	id := c.Param("id")
	if err := DB.Delete(&CustomRule{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "删除失败", "error": err.Error()})
		return
	}
	ReapplyRulesToLatestSubscription()
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

	// 自动清理因 dialer-proxy 引用自身代理组而产生的闭环
	autoPruneDialerProxyLoops(proxiesNode, proxyGroupsNode)

	// 转回 YAML 字符串（保持原格式和注释）
	out, err := yaml.Marshal(&root)
	if err != nil {
		log.Printf("YAML marshal warning: %v", err)
		return yamlContent
	}
	return string(out)
}

// injectCustomRules 基于 yaml.v3 Node 树完成分流规则的强力接管、前置注入与原生去重
func injectCustomRules(yamlContent string) string {
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
	customRules, _ := GetCustomRules()
	customFingerprints := make(map[string]bool)
	var customRuleNodes []*yaml.Node

	for _, cr := range customRules {
		// 生成规则字符串: TYPE,PAYLOAD,TARGET
		// 若 PAYLOAD 为 "-", 说明这是没有中间 payload 的规则（如 MATCH）
		var ruleStr string
		var fingerprint string
		if cr.Payload == "-" || cr.Payload == "" {
			ruleStr = fmt.Sprintf("%s,%s", cr.Type, cr.Target)
			fingerprint = cr.Type
		} else {
			ruleStr = fmt.Sprintf("%s,%s,%s", cr.Type, cr.Payload, cr.Target)
			fingerprint = fmt.Sprintf("%s,%s", cr.Type, cr.Payload)
		}
		
		customFingerprints[fingerprint] = true
		customRuleNodes = append(customRuleNodes, &yaml.Node{
			Kind:  yaml.ScalarNode,
			Value: ruleStr,
		})
	}

	// 2. 遍历原有订阅规则，剥离掉与自定义规则指纹冲突的部分（实现覆盖）
	var filteredOriginalRules []*yaml.Node
	for _, rn := range rulesNode.Content {
		// 解析原生规则指纹
		parts := strings.Split(rn.Value, ",")
		if len(parts) >= 1 {
			var fingerprint string
			if len(parts) >= 3 {
				fingerprint = fmt.Sprintf("%s,%s", parts[0], parts[1])
			} else if len(parts) == 2 {
				// MATCH,Proxy
				fingerprint = parts[0]
			} else {
				fingerprint = rn.Value
			}

			// 如果该原生指纹在自定义规则中已存在，则抛弃（跳过），完成覆盖逻辑
			if customFingerprints[fingerprint] {
				continue
			}
		}
		filteredOriginalRules = append(filteredOriginalRules, rn)
	}

	// 3. 强行接管：将自定义规则永远排在原生规则的最前面（享有最高处理优先级）
	rulesNode.Content = append(customRuleNodes, filteredOriginalRules...)

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
func ProcessSubscriptionRawData(rawResponse string) (string, error) {
	var decodedContent string
	decoded, err := decodeAdaptiveBase64(rawResponse)
	if err != nil {
		if strings.Contains(rawResponse, "proxies:") || strings.Contains(rawResponse, "outbounds:") || strings.Contains(rawResponse, "servers:") {
			decodedContent = rawResponse
		} else {
			return "", fmt.Errorf("解析失败，且不包含常见配置文件特征，目标地址返回的内容格式不支持: %v", err)
		}
	} else {
		decodedContent = decoded
	}

	lines := strings.Split(decodedContent, "\n")
	for i, line := range lines {
		if unescaped, err := url.PathUnescape(strings.TrimSpace(line)); err == nil {
			lines[i] = unescaped
		}
	}
	decodedContent = strings.Join(lines, "\n")
	
	decodedContent = injectCustomNodes(decodedContent)
	decodedContent = injectCustomGroups(decodedContent)
	decodedContent = injectCustomRules(decodedContent)

	return decodedContent, nil
}

// ReapplyRulesToLatestSubscription 获取最新订阅并重新应用所有规则
func ReapplyRulesToLatestSubscription() {
	var sub Subscription
	if err := DB.Order("updated_at desc").First(&sub).Error; err != nil {
		return
	}
	if sub.RawResponse == "" {
		return
	}
	if decodedContent, err := ProcessSubscriptionRawData(sub.RawResponse); err == nil {
		sub.Decoded = decodedContent
		DB.Save(&sub)
	}
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

	rawToken := fmt.Sprintf("%s|%d|%s", user.Username, time.Now().Unix(), uuid.New().String())
	token := base64.URLEncoding.EncodeToString([]byte(rawToken))

	user.SubToken = token
	if err := DB.Save(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "保存 Token 失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "Token 生成成功", "data": gin.H{"token": token}})
}

// handleFinalSubscription 最终订阅地址接口
func handleFinalSubscription(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.String(http.StatusUnauthorized, "Missing token")
		return
	}

	var user User
	if err := DB.Where("sub_token = ?", token).First(&user).Error; err != nil {
		c.String(http.StatusUnauthorized, "Invalid or expired token")
		return
	}

	var sub Subscription
	if err := DB.Order("updated_at desc").First(&sub).Error; err != nil {
		c.String(http.StatusNotFound, "No subscription found in database")
		return
	}

	targetURL := sub.URL
	if targetURL == "" {
		c.String(http.StatusBadRequest, "Subscription URL is empty")
		return
	}

	// 1. 尝试从上游拉取最新响应
	rawResponse, err := fetchURLContent(targetURL)
	if err != nil {
		// 容错降级返回缓存
		if sub.Decoded != "" {
			c.Data(http.StatusOK, "text/yaml; charset=utf-8", []byte(sub.Decoded))
			return
		}
		c.String(http.StatusBadGateway, "Failed to fetch original subscription: "+err.Error())
		return
	}

	// 2. 解码并清洗
	var decodedContent string
	decoded, err := decodeAdaptiveBase64(rawResponse)
	if err != nil {
		if strings.Contains(rawResponse, "proxies:") || strings.Contains(rawResponse, "outbounds:") || strings.Contains(rawResponse, "servers:") {
			decodedContent = rawResponse
		} else {
			if sub.Decoded != "" {
				c.Data(http.StatusOK, "text/yaml; charset=utf-8", []byte(sub.Decoded))
				return
			}
			c.String(http.StatusUnprocessableEntity, "Unsupported format")
			return
		}
	} else {
		decodedContent = decoded
	}

	lines := strings.Split(decodedContent, "\n")
	for i, line := range lines {
		if unescaped, err := url.PathUnescape(strings.TrimSpace(line)); err == nil {
			lines[i] = unescaped
		}
	}
	decodedContent = strings.Join(lines, "\n")

	// 3. AST 注入
	decodedContent = injectCustomNodes(decodedContent)
	decodedContent = injectCustomGroups(decodedContent)
	decodedContent = injectCustomRules(decodedContent)

	// 4. 更新缓存
	sub.RawResponse = rawResponse
	sub.Decoded = decodedContent
	DB.Save(&sub)

	// 5. 返回纯 YAML
	c.Data(http.StatusOK, "text/yaml; charset=utf-8", []byte(decodedContent))
}
