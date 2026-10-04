package config

import "os"

type Config struct {
	APIKey string
	Region string
}

func FromEnv() Config {
	return Config{
		APIKey: os.Getenv("ACME_API_KEY"),
		Region: os.Getenv("ACME_REGION"),
	}
}
