package coresql

import "time"

type Config struct {
	Driver      string        `yaml:"driver" mapstructure:"driver"`
	DSN         string        `yaml:"dsn" mapstructure:"dsn"`
	MaxOpen     int           `yaml:"maxOpen" mapstructure:"maxOpen"`
	MaxIdle     int           `yaml:"maxIdle" mapstructure:"maxIdle"`
	MaxLifetime time.Duration `yaml:"maxLifetime" mapstructure:"maxLifetime"`
	SlowQuery   time.Duration `yaml:"slowQuery" mapstructure:"slowQuery"`
}
