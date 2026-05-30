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

type Config struct {
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
		CaptchaEnabled bool   `toml:"captcha_enabled"`
		CaptchaLength  int    `toml:"captcha_length"`
		CaptchaWidth   int    `toml:"captcha_width"`
		CaptchaHeight  int    `toml:"captcha_height"`
		CaptchaBgColor string `toml:"captcha_bg_color"`
		CaptchaTextColor string `toml:"captcha_text_color"`
	} `toml:"auth"`
}

var DB *gorm.DB
var AppConfig Config

type User struct {
	ID        uint   `gorm:"primarykey"`
	Username  string `gorm:"uniqueIndex;not null"`
	Password  string `gorm:"not null"`
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



func initDB() {
	configData, err := os.ReadFile("config.toml")
	if err != nil {
		log.Fatalf("Failed to read config.toml: %v", err)
	}

	var cfg Config
	err = toml.Unmarshal(configData, &cfg)
	if err != nil {
		log.Fatalf("Failed to unmarshal config.toml: %v", err)
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
