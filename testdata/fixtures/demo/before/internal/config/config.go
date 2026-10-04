package config

import "os"

type Config struct {
	APIKey      string
	LegacyToken string
	Region      string
}

func FromEnv() Config {
	return Config{
		APIKey:      os.Getenv("ACME_API_KEY"),
		LegacyToken: os.Getenv("ACME_LEGACY_TOKEN"),
		Region:      os.Getenv("ACME_REGION"),
	}
}
