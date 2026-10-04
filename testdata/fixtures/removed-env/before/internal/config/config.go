package config

import "os"

// Config is read from the environment.
type Config struct {
	APIKey string
	Token  string
	Region string
}

// Load reads the configuration.
func Load() Config {
	return Config{
		APIKey: os.Getenv("ACME_API_KEY"),
		Token:  os.Getenv("ACME_LEGACY_TOKEN"),
		Region: os.Getenv("ACME_REGION"),
	}
}
