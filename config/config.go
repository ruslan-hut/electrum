// Package config provides configuration management for the Electrum payment service.
// Configuration can be loaded from YAML files and overridden by environment variables.
package config

import (
	"fmt"
	"github.com/ilyakaznacheev/cleanenv"
	"sync"
)

// Config holds all configuration for the Electrum payment service.
// Values can be set via YAML configuration file or environment variables.
// Environment variables take precedence over YAML values.
type Config struct {
	IsDebug        bool   `yaml:"is_debug" env:"DEBUG" env-default:"false"`
	DisablePayment bool   `yaml:"disable_payment" env:"DISABLE_PAYMENT" env-default:"false"`
	LogRecords     int64  `yaml:"log_records" env:"LOG_RECORDS" env-default:"0"`
	FirebaseKey    string `yaml:"firebase_key" env:"FIREBASE_KEY" env-default:""`
	Listen         struct {
		Type     string `yaml:"type" env:"LISTEN_TYPE" env-default:"port"`
		BindIP   string `yaml:"bind_ip" env:"BIND_IP" env-default:"0.0.0.0"`
		Port     string `yaml:"port" env:"PORT" env-default:"5100"`
		TLS      bool   `yaml:"tls_enabled" env:"TLS_ENABLED" env-default:"false"`
		CertFile string `yaml:"cert_file" env:"TLS_CERT_FILE" env-default:""`
		KeyFile  string `yaml:"key_file" env:"TLS_KEY_FILE" env-default:""`
	} `yaml:"listen"`
	Mongo struct {
		Enabled  bool   `yaml:"enabled" env:"MONGO_ENABLED" env-default:"false"`
		Host     string `yaml:"host" env:"MONGO_HOST" env-default:"127.0.0.1"`
		Port     string `yaml:"port" env:"MONGO_PORT" env-default:"27017"`
		User     string `yaml:"user" env:"MONGO_USER" env-default:"admin"`
		Password string `yaml:"password" env:"MONGO_PASSWORD" env-default:"pass"`
		Database string `yaml:"database" env:"MONGO_DATABASE" env-default:""`
	} `yaml:"mongo"`
	Merchant struct {
		Secret     string `yaml:"secret" env:"MERCHANT_SECRET" env-default:""`
		Code       string `yaml:"code" env:"MERCHANT_CODE" env-default:""`
		Terminal   string `yaml:"terminal" env:"MERCHANT_TERMINAL" env-default:""`
		RequestUrl string `yaml:"request_url" env:"MERCHANT_REQUEST_URL" env-default:"https://sis-t.redsys.es:25443/sis/rest/trataPeticionREST"`
	} `yaml:"merchant"`
}

var instance *Config
var once sync.Once

// GetConfig loads configuration from the specified YAML file path.
// Configuration values can be overridden by environment variables.
// This function uses a singleton pattern and only loads the config once.
//
// Environment variables take precedence over YAML values. See Config struct
// for the list of supported environment variables.
//
// Example:
//
//	cfg, err := config.GetConfig("config.yml")
//	if err != nil {
//	    log.Fatal(err)
//	}
func GetConfig(path string) (*Config, error) {
	var err error
	once.Do(func() {
		instance = &Config{}
		if err = cleanenv.ReadConfig(path, instance); err != nil {
			desc, _ := cleanenv.GetDescription(instance, nil)
			err = fmt.Errorf("load config: %w; %s", err, desc)
			instance = nil
		}
	})
	return instance, err
}
