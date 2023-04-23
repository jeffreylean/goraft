package config

import (
	"io/ioutil"
	"log"
	"os"

	"gopkg.in/yaml.v3"
)

type Cluster struct {
	ID     int      `yaml:"id"`
	Port   int      `yaml:"port"`
	Host   string   `yaml:"host"`
	Routes []string `yaml:"routes"`
}

type Config struct {
	Cluster Cluster `yaml:"cluster"`
}

func Load(path string) *Config {
	// Read the file
	buf, err := ioutil.ReadFile(path)
	if err != nil {
		log.Fatalf("Err: %v", err)
	}

	conf := new(Config)
	if err := yaml.Unmarshal(buf, conf); err != nil {
		log.Printf("Err: %v", err)
		os.Exit(1)
	}
	return conf
}
