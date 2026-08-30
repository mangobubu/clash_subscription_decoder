package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	loginEntryHashByteLength = 16
	authSettingSingletonID   = 1
)

var loginEntryHashPattern = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

// AuthSetting 保存需要跨进程重启复用的认证入口配置。
// 当前仅使用 ID=1 的单例记录。
type AuthSetting struct {
	ID             uint   `gorm:"primarykey"`
	LoginEntryHash string `gorm:"size:32;not null"`
	CreatedAt      int64  `gorm:"autoCreateTime"`
	UpdatedAt      int64  `gorm:"autoUpdateTime"`
}

type loginEntryHashRepository interface {
	Load() (string, error)
	SaveIfAbsent(candidate string) (string, error)
}

type gormLoginEntryHashRepository struct {
	db *gorm.DB
}

func (repository gormLoginEntryHashRepository) Load() (string, error) {
	var setting AuthSetting
	err := repository.db.First(&setting, authSettingSingletonID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("读取登录入口失败: %w", err)
	}
	return setting.LoginEntryHash, nil
}

func (repository gormLoginEntryHashRepository) SaveIfAbsent(candidate string) (string, error) {
	setting := AuthSetting{
		ID:             authSettingSingletonID,
		LoginEntryHash: candidate,
	}
	if err := repository.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&setting).Error; err != nil {
		return "", fmt.Errorf("保存登录入口失败: %w", err)
	}

	persisted, err := repository.Load()
	if err != nil {
		return "", err
	}
	if persisted == "" {
		return "", errors.New("保存登录入口后未读取到持久化记录")
	}
	return persisted, nil
}

func normalizeLoginEntryHash(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if !loginEntryHashPattern.MatchString(value) {
		return "", errors.New("登录入口哈希必须是 32 位十六进制字符")
	}
	return strings.ToLower(value), nil
}

func selectLoginEntryHashOverride(configValue, environmentValue string) string {
	if strings.TrimSpace(environmentValue) != "" {
		return environmentValue
	}
	return configValue
}

func generateLoginEntryHash(randomSource io.Reader) (string, error) {
	randomBytes := make([]byte, loginEntryHashByteLength)
	if _, err := io.ReadFull(randomSource, randomBytes); err != nil {
		return "", fmt.Errorf("生成随机登录入口失败: %w", err)
	}
	return hex.EncodeToString(randomBytes), nil
}

func resolveLoginEntryHash(
	configuredValue string,
	repository loginEntryHashRepository,
	randomSource io.Reader,
) (string, error) {
	configuredHash, err := normalizeLoginEntryHash(configuredValue)
	if err != nil {
		return "", err
	}
	if configuredHash != "" {
		return configuredHash, nil
	}

	persistedHash, err := repository.Load()
	if err != nil {
		return "", err
	}
	if persistedHash != "" {
		return normalizePersistedLoginEntryHash(persistedHash)
	}

	generatedHash, err := generateLoginEntryHash(randomSource)
	if err != nil {
		return "", err
	}
	persistedHash, err = repository.SaveIfAbsent(generatedHash)
	if err != nil {
		return "", err
	}
	return normalizePersistedLoginEntryHash(persistedHash)
}

func normalizePersistedLoginEntryHash(value string) (string, error) {
	normalized, err := normalizeLoginEntryHash(value)
	if err != nil {
		return "", fmt.Errorf("数据库中的登录入口哈希无效: %w", err)
	}
	if normalized == "" {
		return "", errors.New("数据库中的登录入口哈希为空")
	}
	return normalized, nil
}

func initializeLoginEntryHash(db *gorm.DB, configuredValue string) (string, error) {
	return resolveLoginEntryHash(
		configuredValue,
		gormLoginEntryHashRepository{db: db},
		rand.Reader,
	)
}

func loginEntryPath(hash string) string {
	return "/" + hash
}

func newFrontendRouteHandler(frontend fs.FS, entryPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		requestPath := c.Request.URL.Path
		if isBackendOnlyPath(requestPath) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Not Found"})
			return
		}

		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			serveFrontendNotFound(c, frontend)
			return
		}

		if requestPath == entryPath {
			if !serveFrontendHTML(c, frontend, "index.html", http.StatusOK) {
				c.String(http.StatusInternalServerError, "Embedded frontend index.html is missing")
			}
			return
		}

		if isExistingFrontendAsset(frontend, requestPath) {
			http.FileServer(http.FS(frontend)).ServeHTTP(c.Writer, c.Request)
			return
		}

		serveFrontendNotFound(c, frontend)
	}
}

func isExistingFrontendAsset(frontend fs.FS, requestPath string) bool {
	filePath := strings.TrimPrefix(requestPath, "/")
	if !fs.ValidPath(filePath) {
		return false
	}

	extension := strings.ToLower(path.Ext(filePath))
	if extension == ".html" || extension == ".htm" {
		return false
	}

	fileInfo, err := fs.Stat(frontend, filePath)
	return err == nil && !fileInfo.IsDir()
}

func serveFrontendNotFound(c *gin.Context, frontend fs.FS) {
	if serveFrontendHTML(c, frontend, "404.html", http.StatusNotFound) {
		return
	}
	writeHTMLResponse(c, http.StatusNotFound, []byte("404 page not found"))
}

func serveFrontendHTML(c *gin.Context, frontend fs.FS, filePath string, status int) bool {
	content, err := fs.ReadFile(frontend, filePath)
	if err != nil {
		return false
	}
	writeHTMLResponse(c, status, content)
	return true
}

func writeHTMLResponse(c *gin.Context, status int, content []byte) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(status)
	if c.Request.Method == http.MethodHead {
		c.Writer.WriteHeaderNow()
		return
	}
	_, _ = c.Writer.Write(content)
}
