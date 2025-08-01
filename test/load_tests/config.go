package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	LoadTest LoadTestConfig `json:"loadTest" yaml:"loadTest"`
	Docker   DockerConfig   `json:"docker" yaml:"docker"`
	Porch    PorchConfig    `json:"porch" yaml:"porch"`
	Output   OutputConfig   `json:"output" yaml:"output"`
}

type LoadTestConfig struct {
	NumUsers int           `json:"numUsers" yaml:"numUsers"`
	RampUp   time.Duration `json:"rampUp" yaml:"rampUp"`
	Duration time.Duration `json:"duration" yaml:"duration"`
}

type DockerConfig struct {
	ContainerName string        `json:"containerName" yaml:"containerName"`
	StatsInterval time.Duration `json:"statsInterval" yaml:"statsInterval"`
	OutputFile    string        `json:"outputFile" yaml:"outputFile"`
}

type PorchConfig struct {
	Namespace string `json:"namespace" yaml:"namespace"`
	Repo      string `json:"repo" yaml:"repo"`
	Workspace string `json:"workspace" yaml:"workspace"`
}

type OutputConfig struct {
	ChartsEnabled bool   `json:"chartsEnabled" yaml:"chartsEnabled"`
	LogLevel      string `json:"logLevel" yaml:"logLevel"`
	OutputDir     string `json:"outputDir" yaml:"outputDir"`
}

func DefaultConfig() *Config {
	return &Config{
		LoadTest: LoadTestConfig{
			NumUsers: 10,
			RampUp:   60 * time.Second,
			Duration: 600 * time.Second,
		},
		Docker: DockerConfig{
			ContainerName: "porch-test-control-plane",
			StatsInterval: time.Second,
			OutputFile:    "docker_stats.csv",
		},
		Porch: PorchConfig{
			Namespace: "porch-demo",
			Repo:      "porch-test",
			Workspace: "1",
		},
		Output: OutputConfig{
			ChartsEnabled: true,
			LogLevel:      "info",
			OutputDir:     ".",
		},
	}
}

func LoadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	config := DefaultConfig()

	if err := yaml.Unmarshal(data, config); err != nil {
		if err := json.Unmarshal(data, config); err != nil {
			return nil, fmt.Errorf("failed to parse config file (neither YAML nor JSON): %w", err)
		}
	}

	return config, nil
}

func SaveConfig(config *Config, filename string) error {
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	return os.WriteFile(filename, data, 0644)
}

func ValidateConfig(config *Config) error {
	if config.LoadTest.NumUsers <= 0 {
		return fmt.Errorf("numUsers must be positive")
	}
	if config.LoadTest.RampUp < 0 {
		return fmt.Errorf("rampUp cannot be negative")
	}
	if config.LoadTest.Duration <= 0 {
		return fmt.Errorf("duration must be positive")
	}
	if config.Docker.ContainerName == "" {
		return fmt.Errorf("docker container name cannot be empty")
	}
	if config.Porch.Namespace == "" {
		return fmt.Errorf("porch namespace cannot be empty")
	}
	if config.Porch.Repo == "" {
		return fmt.Errorf("porch repo cannot be empty")
	}
	return nil
} 