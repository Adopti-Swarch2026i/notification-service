// Package discovery handles registration with Consul for service discovery.
package discovery

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"go.uber.org/zap"
)

type service struct {
	ID      string   `json:"ID"`
	Name    string   `json:"Name"`
	Address string   `json:"Address"`
	Port    int      `json:"Port"`
	Tags    []string `json:"Tags"`
	Check   check    `json:"Check"`
}

type check struct {
	TCP                            string `json:"TCP"`
	Interval                       string `json:"Interval"`
	Timeout                        string `json:"Timeout"`
	DeregisterCriticalServiceAfter string `json:"DeregisterCriticalServiceAfter"`
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func consulURL(path string) string {
	return fmt.Sprintf("http://%s:%s%s",
		getenv("CONSUL_HOST", "consul"),
		getenv("CONSUL_PORT", "8500"),
		path)
}

// Register puts this instance into Consul's service catalog.
func Register(logger *zap.Logger) {
	name := getenv("SERVICE_NAME", "notification-service")
	id := getenv("SERVICE_ID", name)
	host := getenv("SERVICE_HOST", id)
	port, err := strconv.Atoi(getenv("SERVICE_PORT", "8443"))
	if err != nil {
		port = 8443
	}

	svc := service{
		ID: id, Name: name, Address: host, Port: port,
		Tags: []string{"go", "https", "mtls"},
		Check: check{
			TCP:                            fmt.Sprintf("%s:%d", host, port),
			Interval:                       "10s",
			Timeout:                        "5s",
			DeregisterCriticalServiceAfter: "1m",
		},
	}
	body, _ := json.Marshal(svc)
	req, _ := http.NewRequest(http.MethodPut, consulURL("/v1/agent/service/register"), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logger.Warn("Failed to register with Consul", zap.Error(err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		logger.Warn("Consul register returned non-2xx", zap.Int("status", resp.StatusCode))
		return
	}
	logger.Info("Registered with Consul", zap.String("id", id), zap.String("name", name))
}

// Deregister removes this instance from Consul's service catalog.
func Deregister(logger *zap.Logger) {
	id := getenv("SERVICE_ID", getenv("SERVICE_NAME", "notification-service"))
	req, _ := http.NewRequest(http.MethodPut,
		consulURL("/v1/agent/service/deregister/"+id), nil)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logger.Warn("Failed to deregister from Consul", zap.Error(err))
		return
	}
	resp.Body.Close()
	logger.Info("Deregistered from Consul", zap.String("id", id))
}
