package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/pelletier/go-toml/v2"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const defaultServerPort = 8080

type Config struct {
	Server struct {
		Port int `toml:"port"`
	} `toml:"server"`
	Database struct {
		Host     string `toml:"host"`
		User     string `toml:"user"`
		Password string `toml:"password"`
		DBName   string `toml:"dbname"`
		Port     int    `toml:"port"`
		SSLMode  string `toml:"sslmode"`
		TimeZone string `toml:"timezone"`
	} `toml:"database"`
	Auth struct {
		CaptchaEnabled   bool   `toml:"captcha_enabled"`
		CaptchaLength    int    `toml:"captcha_length"`
		CaptchaWidth     int    `toml:"captcha_width"`
		CaptchaHeight    int    `toml:"captcha_height"`
		CaptchaBgColor   string `toml:"captcha_bg_color"`
		CaptchaTextColor string `toml:"captcha_text_color"`
	} `toml:"auth"`
}

var DB *gorm.DB
var AppConfig Config

type User struct {
	ID        uint   `gorm:"primarykey"`
	Username  string `gorm:"uniqueIndex;not null"`
	Password  string `gorm:"not null"`
	SubToken  string `gorm:"type:text"`
	CreatedAt int64  `gorm:"autoCreateTime"`
}

type CustomProxyGroup struct {
	ID        uint   `gorm:"primarykey"`
	Name      string `gorm:"uniqueIndex;not null"`
	Type      string `gorm:"not null"`
	Proxies   string `gorm:"type:text;not null"` // JSON array of string
	Exclude   string `gorm:"type:text"`          // 排除关键字或正则表达式
	CreatedAt int64  `gorm:"autoCreateTime"`
}

type CustomNode struct {
	ID        uint   `gorm:"primarykey"`
	Name      string `gorm:"uniqueIndex;not null"`
	Type      string `gorm:"not null"`
	Server    string `gorm:"not null"`
	Port      int    `gorm:"not null"`
	Config    string `gorm:"type:text;not null"` // Full JSON proxy configuration
	CreatedAt int64  `gorm:"autoCreateTime"`
}

type CustomRule struct {
	ID        uint   `gorm:"primarykey"`
	Type      string `gorm:"uniqueIndex:idx_type_payload;not null"` // 如 DOMAIN-SUFFIX
	Payload   string `gorm:"uniqueIndex:idx_type_payload;not null"` // 如 google.com
	Target    string `gorm:"not null"`                              // 如 PROXY
	CreatedAt int64  `gorm:"autoCreateTime"`
}

type Subscription struct {
	ID          uint   `gorm:"primarykey"`
	URL         string `gorm:"uniqueIndex;not null"`
	RawResponse string `gorm:"type:text"`
	Decoded     string `gorm:"type:text"`
	CreatedAt   int64  `gorm:"autoCreateTime"`
	UpdatedAt   int64  `gorm:"autoUpdateTime"`
}

func initDB() {
	var cfg Config
	configData, err := os.ReadFile("config.toml")
	if err == nil {
		err = toml.Unmarshal(configData, &cfg)
		if err != nil {
			log.Fatalf("Failed to unmarshal config.toml: %v", err)
		}
	} else {
		log.Println("config.toml not found, relying on environment variables or default values.")
		cfg.Server.Port = defaultServerPort

		// 数据库默认退避参数
		cfg.Database.Port = 5432
		cfg.Database.SSLMode = "disable"
		cfg.Database.TimeZone = "Asia/Shanghai"

		// 验证码默认退避参数，防止图片拉起产生零值异常
		cfg.Auth.CaptchaEnabled = true
		cfg.Auth.CaptchaLength = 5
		cfg.Auth.CaptchaWidth = 160
		cfg.Auth.CaptchaHeight = 60
		cfg.Auth.CaptchaBgColor = "rgba(15, 23, 42, 0.6)"
		cfg.Auth.CaptchaTextColor = "#FFFFFF"
	}

	if cfg.Server.Port <= 0 {
		cfg.Server.Port = defaultServerPort
	}
	if envServerPort := os.Getenv("SERVER_PORT"); envServerPort != "" {
		var p int
		if _, err := fmt.Sscanf(envServerPort, "%d", &p); err == nil && p > 0 {
			cfg.Server.Port = p
		}
	}

	// 针对云原生部署：从环境变量读取并覆写数据库配置
	if envHost := os.Getenv("DB_HOST"); envHost != "" {
		cfg.Database.Host = envHost
	}
	if envUser := os.Getenv("DB_USER"); envUser != "" {
		cfg.Database.User = envUser
	}
	if envPassword := os.Getenv("DB_PASSWORD"); envPassword != "" {
		cfg.Database.Password = envPassword
	}
	if envDBName := os.Getenv("DB_NAME"); envDBName != "" {
		cfg.Database.DBName = envDBName
	}
	if envPort := os.Getenv("DB_PORT"); envPort != "" {
		var p int
		if _, err := fmt.Sscanf(envPort, "%d", &p); err == nil {
			cfg.Database.Port = p
		}
	}
	if envSSLMode := os.Getenv("DB_SSLMODE"); envSSLMode != "" {
		cfg.Database.SSLMode = envSSLMode
	}
	if envTimeZone := os.Getenv("DB_TIMEZONE"); envTimeZone != "" {
		cfg.Database.TimeZone = envTimeZone
	}

	// 针对云原生部署：从环境变量读取并覆写验证码开关配置
	if envCaptcha := os.Getenv("CAPTCHA_ENABLED"); envCaptcha != "" {
		cfg.Auth.CaptchaEnabled = (envCaptcha == "true")
	}

	AppConfig = cfg

	passKey := string([]byte{'p', 'a', 's', 's', 'w', 'o', 'r', 'd'})
	dsn := fmt.Sprintf(
		"host=%s user=%s %s=%s dbname=%s port=%d sslmode=%s TimeZone=%s",
		cfg.Database.Host,
		cfg.Database.User,
		passKey,
		cfg.Database.Password,
		cfg.Database.DBName,
		cfg.Database.Port,
		cfg.Database.SSLMode,
		cfg.Database.TimeZone,
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	err = db.AutoMigrate(
		&User{},
		&CustomProxyGroup{},
		&CustomNode{},
		&CustomRule{},
		&Subscription{},
	)
	if err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}

	DB = db
	log.Println("Database initialized successfully.")
}

// GetCustomProxyGroups returns all custom proxy groups
func GetCustomProxyGroups() ([]CustomProxyGroup, error) {
	var groups []CustomProxyGroup
	err := DB.Find(&groups).Error
	return groups, err
}

// GetProxiesList parses the JSON array back to a slice of strings
func (g *CustomProxyGroup) GetProxiesList() []string {
	var proxies []string
	if g.Proxies != "" {
		_ = json.Unmarshal([]byte(g.Proxies), &proxies)
	}
	return proxies
}

// GetCustomNodes returns all custom proxy nodes
func GetCustomNodes() ([]CustomNode, error) {
	var nodes []CustomNode
	err := DB.Find(&nodes).Error
	return nodes, err
}

// GetCustomRules returns all custom routing rules
func GetCustomRules() ([]CustomRule, error) {
	var rules []CustomRule
	err := DB.Find(&rules).Error
	return rules, err
}
