package main

import "github.com/BurntSushi/toml"

type General struct {
	Resolver string
}

type ConfigProvider struct {
	Hostname string
	Username string
	Password string
}

type DomainConfig struct {
	IPv4 bool
	IPv6 bool
}

type Config struct {
	General  General
	Provider ConfigProvider
	Domain   map[string]DomainConfig
}

func ReadFromFile(path string) (*Config, error) {
	var config Config

	if _, err := toml.DecodeFile(path, &config); err != nil {
		return nil, err
	}

	return &config, nil
}
