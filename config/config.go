package config

import (
	"fmt"
	"github.com/ilyakaznacheev/cleanenv"
	"sync"
)

type Config struct {
	IsDebug        bool   `yaml:"is_debug" env-default:"false"`
	DisablePayment bool   `yaml:"disable_payment" env-default:"false"`
	LogRecords     int64  `yaml:"log_records" env-default:"0"`
	FirebaseKey    string `yaml:"firebase_key" env-default:""`
	Listen         struct {
		Type     string `yaml:"type" env-default:"port"`
		BindIP   string `yaml:"bind_ip" env-default:"0.0.0.0"`
		Port     string `yaml:"port" env-default:"5100"`
		TLS      bool   `yaml:"tls_enabled" env-default:"false"`
		CertFile string `yaml:"cert_file" env-default:""`
		KeyFile  string `yaml:"key_file" env-default:""`
	} `yaml:"listen"`
	Mongo struct {
		Enabled  bool   `yaml:"enabled" env-default:"false"`
		Host     string `yaml:"host" env-default:"127.0.0.1"`
		Port     string `yaml:"port" env-default:"27017"`
		User     string `yaml:"user" env-default:"admin"`
		Password string `yaml:"password" env-default:"pass"`
		Database string `yaml:"database" env-default:""`
	} `yaml:"mongo"`
	Merchant struct {
		Secret     string `yaml:"secret" env-default:"123456789012345678901234"`
		Code       string `yaml:"code" env-default:"1234567890"`
		Terminal   string `yaml:"terminal" env-default:"123"`
		RequestUrl string `yaml:"request_url" env-default:"https://test.terminal.com/"`
	} `yaml:"merchant"`
}

var instance *Config
var once sync.Once

func GetConfig(path string) (*Config, error) {
	var err error
	once.Do(func() {
		instance = &Config{}
		if err = cleanenv.ReadConfig(path, instance); err != nil {
			desc, _ := cleanenv.GetDescription(instance, nil)
			err = fmt.Errorf("%s; %s", err, desc)
			instance = nil
		}
	})
	return instance, err
}
