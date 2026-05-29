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
}

type CustomProxyGroup struct {
	ID        uint   `gorm:"primarykey"`
	Name      string `gorm:"uniqueIndex;not null"`
	Type      string `gorm:"not null"`
	Proxies   string `gorm:"type:text;not null"` // JSON array of string
	CreatedAt int64  `gorm:"autoCreateTime"`
}

var DB *gorm.DB

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

	dsn := fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%d sslmode=%s TimeZone=%s",
		cfg.Database.Host,
		cfg.Database.User,
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

	err = db.AutoMigrate(&CustomProxyGroup{})
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
