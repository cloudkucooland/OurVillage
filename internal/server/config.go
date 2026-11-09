package server

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type ServerConfig struct {
	Server  ServerSection  `yaml:"server" json:"server"`
	Clock   ClockSection   `yaml:"clock" json:"clock"`
	Decay   DecaySection   `yaml:"decay" json:"decay"`
	Weather WeatherSection `yaml:"weather" json:"weather"`
	Rest    RestSection    `yaml:"rest" json:"rest"`
}

type ServerSection struct {
	MQTTAddr    string `yaml:"mqtt_addr" json:"mqtt_addr"`
	MQTTTLSAddr string `yaml:"mqtt_tls_addr" json:"mqtt_tls_addr"`
	TLSCert     string `yaml:"tls_cert" json:"tls_cert"`
	TLSKey      string `yaml:"tls_key" json:"tls_key"`
	DBPath      string `yaml:"db_path" json:"db_path"`
}

type ClockSection struct {
	TickIntervalStr string        `yaml:"tick_interval" json:"tick_interval"`
	TickInterval    time.Duration `yaml:"-" json:"-"`
}

type DecaySection struct {
	HungerRate           int `yaml:"hunger_rate" json:"hunger_rate"`
	EnergyRate           int `yaml:"energy_rate" json:"energy_rate"`
	StarvationHealthRate int `yaml:"starvation_health_rate" json:"starvation_health_rate"`
	WeatherHealthRate    int `yaml:"weather_health_rate" json:"weather_health_rate"`
}

type WeatherSection struct {
	MinSafeTemp    float64 `yaml:"min_safe_temp" json:"min_safe_temp"`
	MaxSafeTemp    float64 `yaml:"max_safe_temp" json:"max_safe_temp"`
	DefaultTemp    float64 `yaml:"default_temp" json:"default_temp"`
	DefaultWeather string  `yaml:"default_weather" json:"default_weather"`
}

type RestSection struct {
	ZeroEnergyDurationTicks int `yaml:"zero_energy_duration_ticks" json:"zero_energy_duration_ticks"`
	ZeroHealthDurationTicks int `yaml:"zero_health_duration_ticks" json:"zero_health_duration_ticks"`
}

func DefaultConfig() *ServerConfig {
	return &ServerConfig{
		Server: ServerSection{
			MQTTAddr:    ":1883",
			MQTTTLSAddr: ":8883",
			TLSCert:     "server.crt",
			TLSKey:      "server.key",
			DBPath:      "agentmud.db",
		},
		Clock: ClockSection{
			TickIntervalStr: "90s",
			TickInterval:    90 * time.Second,
		},
		Decay: DecaySection{
			HungerRate:           1,
			EnergyRate:           1,
			StarvationHealthRate: 1,
			WeatherHealthRate:    1,
		},
		Weather: WeatherSection{
			MinSafeTemp:    10.0,
			MaxSafeTemp:    30.0,
			DefaultTemp:    20.0,
			DefaultWeather: "clear",
		},
		Rest: RestSection{
			ZeroEnergyDurationTicks: 30,
			ZeroHealthDurationTicks: 60,
		},
	}
}

func LoadConfig(configPath string) (*ServerConfig, error) {
	cfg := DefaultConfig()

	if configPath == "" {
		if envPath := os.Getenv("AGENTMUD_SERVER_CONFIG"); envPath != "" {
			configPath = envPath
		} else if _, err := os.Stat("server.yaml"); err == nil {
			configPath = "server.yaml"
		}
	}

	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err == nil {
			if err := yaml.Unmarshal(data, cfg); err != nil {
				return nil, err
			}
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}

	// Parse tick interval
	if cfg.Clock.TickIntervalStr != "" {
		if parsed, err := time.ParseDuration(cfg.Clock.TickIntervalStr); err == nil && parsed > 0 {
			cfg.Clock.TickInterval = parsed
		}
	}

	// Environment variable overrides
	if envInterval := os.Getenv("AGENTMUD_TICK_INTERVAL"); envInterval != "" {
		if parsed, err := time.ParseDuration(envInterval); err == nil && parsed > 0 {
			cfg.Clock.TickInterval = parsed
			cfg.Clock.TickIntervalStr = envInterval
		}
	}
	if envAddr := os.Getenv("AGENTMUD_MQTT_ADDR"); envAddr != "" {
		cfg.Server.MQTTAddr = envAddr
	}
	if envTLSAddr := os.Getenv("AGENTMUD_MQTT_TLS_ADDR"); envTLSAddr != "" {
		cfg.Server.MQTTTLSAddr = envTLSAddr
	}
	if envCert := os.Getenv("AGENTMUD_TLS_CERT"); envCert != "" {
		cfg.Server.TLSCert = envCert
	}
	if envKey := os.Getenv("AGENTMUD_TLS_KEY"); envKey != "" {
		cfg.Server.TLSKey = envKey
	}
	if envDB := os.Getenv("AGENTMUD_DB"); envDB != "" {
		cfg.Server.DBPath = envDB
	}

	return cfg, nil
}
