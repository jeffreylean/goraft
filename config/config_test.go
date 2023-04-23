package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoad(t *testing.T) {
	conf := Load("test.yaml")
	expected := Config{
		Cluster: Cluster{Id: 1, Port: 8081, Host: "127.0.0.1", Routes: nil},
	}

	assert.Equal(t, expected, *conf)

}
