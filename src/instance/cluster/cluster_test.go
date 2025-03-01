// BSD 3-Clause License
//
// (C) Copyright 2025,  Alex Gaetano Padula & SuperMassive authors
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are met:
//
//  1. Redistributions of source code must retain the above copyright notice, this
//     list of conditions and the following disclaimer.
//
//  2. Redistributions in binary form must reproduce the above copyright notice,
//     this list of conditions and the following disclaimer in the documentation
//     and/or other materials provided with the distribution.
//
//  3. Neither the name of the copyright holder nor the names of its
//     contributors may be used to endorse or promote products derived from
//     this software without specific prior written permission.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
// AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
// DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
// FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
// SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
// CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
// OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
package cluster

import (
	"context"
	"encoding/base64"
	"fmt"
	"gopkg.in/yaml.v3"
	"io/ioutil"
	"log"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"supermassive/instance/node"
	"supermassive/instance/nodereplica"
	"supermassive/network/client"
	"supermassive/network/server"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	tests := []struct {
		name      string
		logger    *slog.Logger
		sharedKey string
		username  string
		password  string
		wantErr   bool
	}{
		{
			name:      "valid creation",
			logger:    logger,
			sharedKey: "test-key",
			username:  "test-user",
			password:  "test-pass",
			wantErr:   false,
		},
		{
			name:      "missing shared key",
			logger:    logger,
			sharedKey: "",
			username:  "test-user",
			password:  "test-pass",
			wantErr:   true,
		},
		{
			name:      "missing username",
			logger:    logger,
			sharedKey: "test-key",
			username:  "",
			password:  "test-pass",
			wantErr:   true,
		},
		{
			name:      "missing password",
			logger:    logger,
			sharedKey: "test-key",
			username:  "test-user",
			password:  "",
			wantErr:   true,
		},
		{
			name:      "nil logger",
			logger:    nil,
			sharedKey: "test-key",
			username:  "test-user",
			password:  "test-pass",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.logger, tt.sharedKey, tt.username, tt.password)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCluster_Open(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Create a context with timeout to prevent infinite hanging
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: &Config{
				HealthCheckInterval: 2,
				ServerConfig: &server.Config{
					Address:     "localhost:0", // Use port 0 to let OS assign random port
					UseTLS:      false,
					ReadTimeout: 10,
					BufferSize:  1024,
				},
				NodeConfigs: []*NodeConfig{
					{
						Node: &client.Config{
							ServerAddress:  "localhost:0", // Use port 0 to let OS assign random port
							ConnectTimeout: 1,             // Reduced timeout for testing
							WriteTimeout:   1,
							ReadTimeout:    1,
							BufferSize:     1024,
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a channel to signal test completion
			done := make(chan struct{})

			go func() {
				c, err := New(logger, "test-key", "test-user", "test-pass")
				if err != nil {
					t.Errorf("Failed to create cluster: %v", err)
					close(done)
					return
				}
				c.Config = tt.config

				go func() {
					err = c.Open()
					if (err != nil) != tt.wantErr {
						t.Errorf("Open() error = %v, wantErr %v", err, tt.wantErr)
					}
				}()

				// Brief delay to allow health check goroutine to start
				time.Sleep(100 * time.Millisecond)

				err = c.Close()
				if err != nil {
					t.Errorf("Failed to close cluster: %v", err)
				}

				close(done)

				os.Remove(".cluster")
			}()

			// Wait for either test completion or timeout
			select {
			case <-done:
				// Test completed normally
			case <-ctx.Done():
				t.Fatal("Test timed out")
			}
		})
	}
}

func TestOpenExistingConfigFile(t *testing.T) {
	// Create a temporary directory
	tempDir, err := ioutil.TempDir("", "test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a temporary config file
	configFilePath := filepath.Join(tempDir, ConfigFile)
	configData := &Config{
		HealthCheckInterval: 2,
		ServerConfig: &server.Config{
			Address:     "localhost:4000",
			UseTLS:      false,
			CertFile:    "/",
			KeyFile:     "/",
			ReadTimeout: 10,
			BufferSize:  1024,
		},
		NodeConfigs: []*NodeConfig{
			{
				Node: &client.Config{
					ServerAddress:  "localhost:4001",
					UseTLS:         false,
					ConnectTimeout: 5,
					WriteTimeout:   5,
					ReadTimeout:    5,
					MaxRetries:     3,
					RetryWaitTime:  1,
					BufferSize:     1024,
				},
				Replicas: []*client.Config{
					{
						ServerAddress:  "localhost:4002",
						UseTLS:         false,
						ConnectTimeout: 5,
						WriteTimeout:   5,
						ReadTimeout:    5,
						MaxRetries:     3,
						RetryWaitTime:  1,
						BufferSize:     1024,
					},
				},
			},
		},
	}

	data, err := yaml.Marshal(configData)
	if err != nil {
		t.Fatalf("Failed to marshal config data: %v", err)
	}

	err = ioutil.WriteFile(configFilePath, data, 0644)
	if err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Test the function
	config, err := openExistingConfigFile(tempDir)
	if err != nil {
		t.Fatalf("Failed to open existing config file: %v", err)
	}

	// Validate the config data
	if config.HealthCheckInterval != configData.HealthCheckInterval {
		t.Errorf("Expected HealthCheckInterval %d, got %d", configData.HealthCheckInterval, config.HealthCheckInterval)
	}

	if config.ServerConfig.Address != configData.ServerConfig.Address {
		t.Errorf("Expected ServerConfig.Address %s, got %s", configData.ServerConfig.Address, config.ServerConfig.Address)
	}

	if len(config.NodeConfigs) != len(configData.NodeConfigs) {
		t.Errorf("Expected %d NodeConfigs, got %d", len(configData.NodeConfigs), len(config.NodeConfigs))
	}

	if config.NodeConfigs[0].Node.ServerAddress != configData.NodeConfigs[0].Node.ServerAddress {
		t.Errorf("Expected Node.ServerAddress %s, got %s", configData.NodeConfigs[0].Node.ServerAddress, config.NodeConfigs[0].Node.ServerAddress)
	}

	if len(config.NodeConfigs[0].Replicas) != len(configData.NodeConfigs[0].Replicas) {
		t.Errorf("Expected %d Replicas, got %d", len(configData.NodeConfigs[0].Replicas), len(config.NodeConfigs[0].Replicas))
	}

	if config.NodeConfigs[0].Replicas[0].ServerAddress != configData.NodeConfigs[0].Replicas[0].ServerAddress {
		t.Errorf("Expected Replica.ServerAddress %s, got %s", configData.NodeConfigs[0].Replicas[0].ServerAddress, config.NodeConfigs[0].Replicas[0].ServerAddress)
	}
}

func TestCreateDefaultConfigFile(t *testing.T) {
	// Create a temporary directory
	tempDir := t.TempDir()

	// Call the createDefaultConfigFile function
	_, err := createDefaultConfigFile(tempDir)
	if err != nil {
		t.Fatalf("Failed to create default config file: %v", err)
	}

}

func TestServerAuth(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// We create a new cluster
	nr, err := New(logger, "test-key", "test-user", "test-pass")
	if err != nil {
		t.Fatalf("Failed to create cluster: %v", err)
	}

	// We open in background
	go func() {
		err := nr.Open()
		if err != nil {
			t.Fatalf("Failed to open cluster: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	defer os.Remove(".cluster")

	// We create a tcp client to the cluster, we know the default port is going to be 4000
	// Resolve the string address to a TCP address
	tcpAddr, err := net.ResolveTCPAddr("tcp4", "localhost:4000")
	if err != nil {
		nr.Close()
		t.Fatalf("Failed to resolve address: %v", err)
	}

	// Connect to the address with tcp
	conn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}

	authStr := base64.StdEncoding.EncodeToString([]byte("test-user\\0test-pass"))

	// We authenticate
	_, err = conn.Write([]byte(fmt.Sprintf("AUTH %s\r\n", authStr)))
	if err != nil {
		conn.Close()
		nr.Close()
		t.Fatalf("Failed to authenticate: %v", err)
	}

	// We expect "OK authenticated" as response
	buf := make([]byte, 1024)

	n, err := conn.Read(buf)
	if err != nil {
		nr.Close()
		t.Fatalf("Failed to read response: %v", err)
	}

	conn.Close()

	if string(buf[:n]) != "OK authenticated\r\n" {
		nr.Close()
		t.Fatalf("Expected 'OK authenticated', got %s", string(buf[:n]))
	}

	nr.Close()

}

func TestServerPing(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// We create a new cluster
	nr, err := New(logger, "test-key", "test-user", "test-pass")
	if err != nil {
		t.Fatalf("Failed to create cluster: %v", err)
	}

	// We open in background
	go func() {
		err := nr.Open()
		if err != nil {
			t.Fatalf("Failed to open cluster: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	defer os.Remove(".cluster")

	// We create a tcp client to the cluster, we know the default port is going to be 4000
	// Resolve the string address to a TCP address
	tcpAddr, err := net.ResolveTCPAddr("tcp4", "localhost:4000")
	if err != nil {
		nr.Close()
		t.Fatalf("Failed to resolve address: %v", err)
	}

	// Connect to the address with tcp
	conn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}

	// We ping
	_, err = conn.Write([]byte(fmt.Sprintf("PING\r\n")))
	if err != nil {
		conn.Close()
		nr.Close()
		t.Fatalf("Failed to authenticate: %v", err)
	}

	// We expect "OK authenticated" as response
	buf := make([]byte, 1024)

	n, err := conn.Read(buf)
	if err != nil {
		nr.Close()
		t.Fatalf("Failed to read response: %v", err)
	}

	conn.Close()

	if string(buf[:n]) != "OK PONG\r\n" {
		nr.Close()
		t.Fatalf("Expected 'OK PONG', got %s", string(buf[:n]))
	}

	nr.Close()

}

func TestServerPutNoPrimaries(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// We create a new cluster
	nr, err := New(logger, "test-key", "test-user", "test-pass")
	if err != nil {
		t.Fatalf("Failed to create cluster: %v", err)
	}

	// We open in background
	go func() {
		config := &Config{
			HealthCheckInterval: 2,
			ServerConfig: &server.Config{
				Address:     "localhost:4000",
				UseTLS:      false,
				CertFile:    "/",
				KeyFile:     "/",
				ReadTimeout: 10,
				BufferSize:  1024,
			},
		}

		// Marshal to yaml
		data, err := yaml.Marshal(config)
		if err != nil {
			t.Fatalf("Failed to marshal config data: %v", err)
		}

		// Write to file
		err = os.WriteFile(".cluster", data, 0644)
		if err != nil {
			t.Fatalf("Failed to write config file: %v", err)
		}

		err = nr.Open()
		if err != nil {
			t.Fatalf("Failed to open cluster: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	defer os.Remove(".cluster")

	tcpAddr, err := net.ResolveTCPAddr("tcp4", "localhost:4000")
	if err != nil {
		nr.Close()
		t.Fatalf("Failed to resolve address: %v", err)
	}

	// Connect to the address with tcp
	conn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}

	authStr := base64.StdEncoding.EncodeToString([]byte("test-user\\0test-pass"))

	// We authenticate
	_, err = conn.Write([]byte(fmt.Sprintf("AUTH %s\r\n", authStr)))
	if err != nil {
		conn.Close()
		nr.Close()
		t.Fatalf("Failed to authenticate: %v", err)
	}

	// We expect "OK authenticated" as response
	buf := make([]byte, 1024)

	n, err := conn.Read(buf)
	if err != nil {
		nr.Close()
		t.Fatalf("Failed to read response: %v", err)
	}

	if string(buf[:n]) != "OK authenticated\r\n" {
		conn.Close()
		nr.Close()
		t.Fatalf("Expected 'OK authenticated', got %s", string(buf[:n]))
	}

	_, err = conn.Write([]byte(fmt.Sprintf("PUT hello world\r\n")))
	if err != nil {
		conn.Close()
		nr.Close()
		t.Fatalf("Failed to write: %v", err)
	}

	buf = make([]byte, 1024)

	n, err = conn.Read(buf)
	if err != nil {
		conn.Close()
		nr.Close()
		t.Fatalf("Failed to read response: %v", err)
	}

	if string(buf[:n]) != "ERR no primary nodes available\r\n" {
		conn.Close()
		nr.Close()
		t.Fatalf("Expected 'ERR no primary nodes available', got %s", string(buf[:n]))
	}

	conn.Close()
	nr.Close()

}

func TestServerGetNoPrimaries(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// We create a new cluster
	nr, err := New(logger, "test-key", "test-user", "test-pass")
	if err != nil {
		t.Fatalf("Failed to create cluster: %v", err)
	}

	// We open in background
	go func() {
		config := &Config{
			HealthCheckInterval: 2,
			ServerConfig: &server.Config{
				Address:     "localhost:4000",
				UseTLS:      false,
				CertFile:    "/",
				KeyFile:     "/",
				ReadTimeout: 10,
				BufferSize:  1024,
			},
		}

		// Marshal to yaml
		data, err := yaml.Marshal(config)
		if err != nil {
			t.Fatalf("Failed to marshal config data: %v", err)
		}

		// Write to file
		err = os.WriteFile(".cluster", data, 0644)
		if err != nil {
			t.Fatalf("Failed to write config file: %v", err)
		}

		err = nr.Open()
		if err != nil {
			t.Fatalf("Failed to open cluster: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	defer os.Remove(".cluster")

	tcpAddr, err := net.ResolveTCPAddr("tcp4", "localhost:4000")
	if err != nil {
		nr.Close()
		t.Fatalf("Failed to resolve address: %v", err)
	}

	// Connect to the address with tcp
	conn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}

	authStr := base64.StdEncoding.EncodeToString([]byte("test-user\\0test-pass"))

	// We authenticate
	_, err = conn.Write([]byte(fmt.Sprintf("AUTH %s\r\n", authStr)))
	if err != nil {
		conn.Close()
		nr.Close()
		t.Fatalf("Failed to authenticate: %v", err)
	}

	// We expect "OK authenticated" as response
	buf := make([]byte, 1024)

	n, err := conn.Read(buf)
	if err != nil {
		nr.Close()
		t.Fatalf("Failed to read response: %v", err)
	}

	if string(buf[:n]) != "OK authenticated\r\n" {
		conn.Close()
		nr.Close()
		t.Fatalf("Expected 'OK authenticated', got %s", string(buf[:n]))
	}

	_, err = conn.Write([]byte(fmt.Sprintf("GET hello\r\n")))
	if err != nil {
		conn.Close()
		nr.Close()
		t.Fatalf("Failed to write: %v", err)
	}

	buf = make([]byte, 1024)

	n, err = conn.Read(buf)
	if err != nil {
		conn.Close()
		nr.Close()
		t.Fatalf("Failed to read response: %v", err)
	}

	if string(buf[:n]) != "ERR no primary nodes available\r\n" {
		conn.Close()
		nr.Close()
		t.Fatalf("Expected 'ERR no primary nodes available', got %s", string(buf[:n]))
	}

	conn.Close()
	nr.Close()

}

func TestServerDelNoPrimaries(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// We create a new cluster
	nr, err := New(logger, "test-key", "test-user", "test-pass")
	if err != nil {
		t.Fatalf("Failed to create cluster: %v", err)
	}

	// We open in background
	go func() {
		config := &Config{
			HealthCheckInterval: 2,
			ServerConfig: &server.Config{
				Address:     "localhost:4000",
				UseTLS:      false,
				CertFile:    "/",
				KeyFile:     "/",
				ReadTimeout: 10,
				BufferSize:  1024,
			},
		}

		// Marshal to yaml
		data, err := yaml.Marshal(config)
		if err != nil {
			t.Fatalf("Failed to marshal config data: %v", err)
		}

		// Write to file
		err = os.WriteFile(".cluster", data, 0644)
		if err != nil {
			t.Fatalf("Failed to write config file: %v", err)
		}

		err = nr.Open()
		if err != nil {
			t.Fatalf("Failed to open cluster: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	defer os.Remove(".cluster")

	tcpAddr, err := net.ResolveTCPAddr("tcp4", "localhost:4000")
	if err != nil {
		nr.Close()
		t.Fatalf("Failed to resolve address: %v", err)
	}

	// Connect to the address with tcp
	conn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}

	authStr := base64.StdEncoding.EncodeToString([]byte("test-user\\0test-pass"))

	// We authenticate
	_, err = conn.Write([]byte(fmt.Sprintf("AUTH %s\r\n", authStr)))
	if err != nil {
		conn.Close()
		nr.Close()
		t.Fatalf("Failed to authenticate: %v", err)
	}

	// We expect "OK authenticated" as response
	buf := make([]byte, 1024)

	n, err := conn.Read(buf)
	if err != nil {
		nr.Close()
		t.Fatalf("Failed to read response: %v", err)
	}

	if string(buf[:n]) != "OK authenticated\r\n" {
		conn.Close()
		nr.Close()
		t.Fatalf("Expected 'OK authenticated', got %s", string(buf[:n]))
	}

	_, err = conn.Write([]byte(fmt.Sprintf("DEL hello\r\n")))
	if err != nil {
		conn.Close()
		nr.Close()
		t.Fatalf("Failed to write: %v", err)
	}

	buf = make([]byte, 1024)

	n, err = conn.Read(buf)
	if err != nil {
		conn.Close()
		nr.Close()
		t.Fatalf("Failed to read response: %v", err)
	}

	if string(buf[:n]) != "ERR no primary nodes available\r\n" {
		conn.Close()
		nr.Close()
		t.Fatalf("Expected 'ERR no primary nodes available', got %s", string(buf[:n]))
	}

	conn.Close()
	nr.Close()

}

func TestServerRegxNoPrimaries(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// We create a new cluster
	nr, err := New(logger, "test-key", "test-user", "test-pass")
	if err != nil {
		t.Fatalf("Failed to create cluster: %v", err)
	}

	// We open in background
	go func() {
		config := &Config{
			HealthCheckInterval: 2,
			ServerConfig: &server.Config{
				Address:     "localhost:4000",
				UseTLS:      false,
				CertFile:    "/",
				KeyFile:     "/",
				ReadTimeout: 10,
				BufferSize:  1024,
			},
		}

		// Marshal to yaml
		data, err := yaml.Marshal(config)
		if err != nil {
			t.Fatalf("Failed to marshal config data: %v", err)
		}

		// Write to file
		err = os.WriteFile(".cluster", data, 0644)
		if err != nil {
			t.Fatalf("Failed to write config file: %v", err)
		}

		err = nr.Open()
		if err != nil {
			t.Fatalf("Failed to open cluster: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	defer os.Remove(".cluster")

	tcpAddr, err := net.ResolveTCPAddr("tcp4", "localhost:4000")
	if err != nil {
		nr.Close()
		t.Fatalf("Failed to resolve address: %v", err)
	}

	// Connect to the address with tcp
	conn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}

	authStr := base64.StdEncoding.EncodeToString([]byte("test-user\\0test-pass"))

	// We authenticate
	_, err = conn.Write([]byte(fmt.Sprintf("AUTH %s\r\n", authStr)))
	if err != nil {
		conn.Close()
		nr.Close()
		t.Fatalf("Failed to authenticate: %v", err)
	}

	// We expect "OK authenticated" as response
	buf := make([]byte, 1024)

	n, err := conn.Read(buf)
	if err != nil {
		nr.Close()
		t.Fatalf("Failed to read response: %v", err)
	}

	if string(buf[:n]) != "OK authenticated\r\n" {
		conn.Close()
		nr.Close()
		t.Fatalf("Expected 'OK authenticated', got %s", string(buf[:n]))
	}

	_, err = conn.Write([]byte(fmt.Sprintf("REGX pattern\r\n")))
	if err != nil {
		conn.Close()
		nr.Close()
		t.Fatalf("Failed to write: %v", err)
	}

	buf = make([]byte, 1024)

	n, err = conn.Read(buf)
	if err != nil {
		conn.Close()
		nr.Close()
		t.Fatalf("Failed to read response: %v", err)
	}

	if string(buf[:n]) != "ERR no primary nodes available\r\n" {
		conn.Close()
		nr.Close()
		t.Fatalf("Expected 'ERR no primary nodes available', got %s", string(buf[:n]))
	}

	conn.Close()
	nr.Close()

}

func TestServerIncrNoPrimaries(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// We create a new cluster
	nr, err := New(logger, "test-key", "test-user", "test-pass")
	if err != nil {
		t.Fatalf("Failed to create cluster: %v", err)
	}

	// We open in background
	go func() {
		config := &Config{
			HealthCheckInterval: 2,
			ServerConfig: &server.Config{
				Address:     "localhost:4000",
				UseTLS:      false,
				CertFile:    "/",
				KeyFile:     "/",
				ReadTimeout: 10,
				BufferSize:  1024,
			},
		}

		// Marshal to yaml
		data, err := yaml.Marshal(config)
		if err != nil {
			t.Fatalf("Failed to marshal config data: %v", err)
		}

		// Write to file
		err = os.WriteFile(".cluster", data, 0644)
		if err != nil {
			t.Fatalf("Failed to write config file: %v", err)
		}

		err = nr.Open()
		if err != nil {
			t.Fatalf("Failed to open cluster: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	defer os.Remove(".cluster")

	tcpAddr, err := net.ResolveTCPAddr("tcp4", "localhost:4000")
	if err != nil {
		nr.Close()
		t.Fatalf("Failed to resolve address: %v", err)
	}

	// Connect to the address with tcp
	conn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}

	authStr := base64.StdEncoding.EncodeToString([]byte("test-user\\0test-pass"))

	// We authenticate
	_, err = conn.Write([]byte(fmt.Sprintf("AUTH %s\r\n", authStr)))
	if err != nil {
		conn.Close()
		nr.Close()
		t.Fatalf("Failed to authenticate: %v", err)
	}

	// We expect "OK authenticated" as response
	buf := make([]byte, 1024)

	n, err := conn.Read(buf)
	if err != nil {
		nr.Close()
		t.Fatalf("Failed to read response: %v", err)
	}

	if string(buf[:n]) != "OK authenticated\r\n" {
		conn.Close()
		nr.Close()
		t.Fatalf("Expected 'OK authenticated', got %s", string(buf[:n]))
	}

	_, err = conn.Write([]byte(fmt.Sprintf("INCR n 1\r\n")))
	if err != nil {
		conn.Close()
		nr.Close()
		t.Fatalf("Failed to write: %v", err)
	}

	buf = make([]byte, 1024)

	n, err = conn.Read(buf)
	if err != nil {
		conn.Close()
		nr.Close()
		t.Fatalf("Failed to read response: %v", err)
	}

	if string(buf[:n]) != "ERR no primary nodes available\r\n" {
		conn.Close()
		nr.Close()
		t.Fatalf("Expected 'ERR no primary nodes available', got %s", string(buf[:n]))
	}

	conn.Close()
	nr.Close()

}

func TestServerDecrNoPrimaries(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// We create a new cluster
	nr, err := New(logger, "test-key", "test-user", "test-pass")
	if err != nil {
		t.Fatalf("Failed to create cluster: %v", err)
	}

	// We open in background
	go func() {
		config := &Config{
			HealthCheckInterval: 2,
			ServerConfig: &server.Config{
				Address:     "localhost:4000",
				UseTLS:      false,
				CertFile:    "/",
				KeyFile:     "/",
				ReadTimeout: 10,
				BufferSize:  1024,
			},
		}

		// Marshal to yaml
		data, err := yaml.Marshal(config)
		if err != nil {
			t.Fatalf("Failed to marshal config data: %v", err)
		}

		// Write to file
		err = os.WriteFile(".cluster", data, 0644)
		if err != nil {
			t.Fatalf("Failed to write config file: %v", err)
		}

		err = nr.Open()
		if err != nil {
			t.Fatalf("Failed to open cluster: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	defer os.Remove(".cluster")

	tcpAddr, err := net.ResolveTCPAddr("tcp4", "localhost:4000")
	if err != nil {
		nr.Close()
		t.Fatalf("Failed to resolve address: %v", err)
	}

	// Connect to the address with tcp
	conn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}

	authStr := base64.StdEncoding.EncodeToString([]byte("test-user\\0test-pass"))

	// We authenticate
	_, err = conn.Write([]byte(fmt.Sprintf("AUTH %s\r\n", authStr)))
	if err != nil {
		conn.Close()
		nr.Close()
		t.Fatalf("Failed to authenticate: %v", err)
	}

	// We expect "OK authenticated" as response
	buf := make([]byte, 1024)

	n, err := conn.Read(buf)
	if err != nil {
		nr.Close()
		t.Fatalf("Failed to read response: %v", err)
	}

	if string(buf[:n]) != "OK authenticated\r\n" {
		conn.Close()
		nr.Close()
		t.Fatalf("Expected 'OK authenticated', got %s", string(buf[:n]))
	}

	_, err = conn.Write([]byte(fmt.Sprintf("DECR n 1\r\n")))
	if err != nil {
		conn.Close()
		nr.Close()
		t.Fatalf("Failed to write: %v", err)
	}

	buf = make([]byte, 1024)

	n, err = conn.Read(buf)
	if err != nil {
		conn.Close()
		nr.Close()
		t.Fatalf("Failed to read response: %v", err)
	}

	if string(buf[:n]) != "ERR no primary nodes available\r\n" {
		conn.Close()
		nr.Close()
		t.Fatalf("Expected 'ERR no primary nodes available', got %s", string(buf[:n]))
	}

	conn.Close()
	nr.Close()

}

// We test the server with multiple primaries and no replicas
// writing data and verifying it's existence
func TestServerCrudMultiplePrimaries(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	var shard1, shard2 *node.Node // 2 primaries, no replicas

	go func() {
		config := &node.Config{
			HealthCheckInterval: 2,
			MaxMemoryThreshold:  75,
			ServerConfig: &server.Config{
				Address:     "localhost:4005",
				UseTLS:      false,
				CertFile:    "",
				KeyFile:     "",
				ReadTimeout: 10,
				BufferSize:  1024,
			},
			ReadReplicas: nil,
		}

		// We create temp shard1 directory
		shard1Dir, err := ioutil.TempDir("", "shard1")
		if err != nil {
			t.Fatalf("Failed to create temp directory: %v", err)
		}

		// Marshal yaml
		data, err := yaml.Marshal(config)
		if err != nil {
			t.Fatalf("Failed to marshal config data: %v", err)
		}

		// Write new config file for primary
		err = os.WriteFile(fmt.Sprintf("%s/.node", shard1Dir), data, 0644)
		if err != nil {
			t.Fatalf("Failed to write config file: %v", err)
		}

		shard1, err = node.New(logger, "test-key")
		if err != nil {
			t.Fatalf("Failed to create node: %v", err)
		}

		//shard1.Config = config

		err = shard1.Open(&shard1Dir)
		if err != nil {
			t.Fatalf("Failed to open node: %v", err)
		}

	}()

	go func() {
		config := &node.Config{
			HealthCheckInterval: 2,
			MaxMemoryThreshold:  75,
			ServerConfig: &server.Config{
				Address:     "localhost:4006",
				UseTLS:      false,
				CertFile:    "",
				KeyFile:     "",
				ReadTimeout: 10,
				BufferSize:  1024,
			},
			ReadReplicas: nil,
		}

		// We create temp shard1 directory
		shard2Dir, err := ioutil.TempDir("", "shard2")
		if err != nil {
			t.Fatalf("Failed to create temp directory: %v", err)
		}

		// Marshal yaml
		data, err := yaml.Marshal(config)
		if err != nil {
			t.Fatalf("Failed to marshal config data: %v", err)
		}

		// Write new config file for primary
		err = os.WriteFile(fmt.Sprintf("%s/.node", shard2Dir), data, 0644)
		if err != nil {
			t.Fatalf("Failed to write config file: %v", err)
		}

		shard2, err = node.New(logger, "test-key")
		if err != nil {
			t.Fatalf("Failed to create node: %v", err)
		}

		err = shard2.Open(&shard2Dir)
		if err != nil {
			t.Fatalf("Failed to open node: %v", err)
		}

	}()

	time.Sleep(time.Second) // Wait for primaries to open

	//We create a new cluster
	nr, err := New(logger, "test-key", "test-user", "test-pass")
	if err != nil {
		t.Fatalf("Failed to create cluster: %v", err)
	}

	//We open cluster in background
	go func() {
		config := &Config{
			HealthCheckInterval: 1,
			ServerConfig: &server.Config{
				Address:     "localhost:4004",
				UseTLS:      false,
				CertFile:    "/",
				KeyFile:     "/",
				ReadTimeout: 10,
				BufferSize:  1024,
			},
			NodeConfigs: []*NodeConfig{
				{
					Node: &client.Config{
						ServerAddress:  "localhost:4005",
						UseTLS:         false,
						ConnectTimeout: 5,
						WriteTimeout:   5,
						ReadTimeout:    5,
						MaxRetries:     3,
						RetryWaitTime:  1,
						BufferSize:     1024,
					},
				},
				{
					Node: &client.Config{
						ServerAddress:  "localhost:4006",
						UseTLS:         false,
						ConnectTimeout: 5,
						WriteTimeout:   5,
						ReadTimeout:    5,
						MaxRetries:     3,
						RetryWaitTime:  1,
						BufferSize:     1024,
					},
				},
			},
		}

		// Marshal to yaml
		data, err := yaml.Marshal(config)
		if err != nil {
			t.Fatalf("Failed to marshal config data: %v", err)
		}

		// Write to file
		err = os.WriteFile(".cluster", data, 0644)
		if err != nil {
			t.Fatalf("Failed to write config file: %v", err)
		}

		err = nr.Open()
		if err != nil {
			t.Fatalf("Failed to open cluster: %v", err)
		}
	}()

	time.Sleep(4 * time.Second) // Wait for cluster to start and connect to primaries

	defer os.Remove(".cluster")

	log.Println(shard2.SharedKey)
	log.Println(shard1.SharedKey)
	log.Println(nr.SharedKey)

	tcpAddr, err := net.ResolveTCPAddr("tcp4", "localhost:4004")
	if err != nil {
		nr.Close()
		t.Fatalf("Failed to resolve address: %v", err)
	}

	// Connect to the address with tcp
	conn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}

	authStr := base64.StdEncoding.EncodeToString([]byte("test-user\\0test-pass"))

	// We authenticate
	_, err = conn.Write([]byte(fmt.Sprintf("AUTH %s\r\n", authStr)))
	if err != nil {
		conn.Close()
		nr.Close()
		t.Fatalf("Failed to authenticate: %v", err)
	}

	// We expect "OK authenticated" as response
	buf := make([]byte, 1024)

	n, err := conn.Read(buf)
	if err != nil {
		nr.Close()
		t.Fatalf("Failed to read response: %v", err)
	}

	if string(buf[:n]) != "OK authenticated\r\n" {
		conn.Close()
		nr.Close()
		t.Fatalf("Expected 'OK authenticated', got %s", string(buf[:n]))
	}

	for i := 0; i < 10; i++ {
		_, err = conn.Write([]byte(fmt.Sprintf("PUT hello%d world\r\n", i)))
		if err != nil {
			conn.Close()
			shard1.Close()
			shard2.Close()
			nr.Close()
			t.Fatalf("Failed to write: %v", err)
		}

		buf = make([]byte, 1024)

		n, err = conn.Read(buf)
		if err != nil {
			conn.Close()
			shard1.Close()
			shard2.Close()
			nr.Close()
			t.Fatalf("Failed to read response: %v", err)
		}

		if !strings.HasPrefix(string(buf[:n]), "OK") {
			conn.Close()
			shard1.Close()
			shard2.Close()
			nr.Close()
			t.Fatalf("Expected 'OK', got %s", string(buf[:n]))
		}
	}

	// We verify the data
	for i := 0; i < 10; i++ {
		_, err = conn.Write([]byte(fmt.Sprintf("GET hello%d\r\n", i)))
		if err != nil {
			conn.Close()
			shard1.Close()
			shard2.Close()
			nr.Close()
			t.Fatalf("Failed to write: %v", err)
		}

		buf = make([]byte, 1024)

		n, err = conn.Read(buf)
		if err != nil {
			conn.Close()
			shard1.Close()
			shard2.Close()
			nr.Close()
			t.Fatalf("Failed to read response: %v", err)
		}

		if !strings.HasPrefix(string(buf[:n]), "OK") {
			conn.Close()
			shard1.Close()
			shard2.Close()
			nr.Close()
			t.Fatalf("Expected 'OK', got %s", string(buf[:n]))
		}
	}

	conn.Close()
	nr.Close()
	shard1.Close()
	shard2.Close()

}

// We have 1 primary, 1 replica
// We write data to primary, shutdown primary and check if replica has data
func TestServerCrudPrimaryDownUseReplica(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	var shard *node.Node
	var replica *nodereplica.NodeReplica

	go func() {
		config := &node.Config{
			HealthCheckInterval: 2,
			MaxMemoryThreshold:  75,
			ServerConfig: &server.Config{
				Address:     "localhost:4010",
				UseTLS:      false,
				CertFile:    "",
				KeyFile:     "",
				ReadTimeout: 10,
				BufferSize:  1024,
			},
			ReadReplicas: []*client.Config{
				{
					ServerAddress:  "localhost:4011",
					UseTLS:         false,
					ConnectTimeout: 5,
					WriteTimeout:   5,
					ReadTimeout:    5,
					MaxRetries:     3,
					RetryWaitTime:  1,
					BufferSize:     1024,
				},
			},
		}

		// We create temp shard directory
		shard1Dir, err := ioutil.TempDir("", "shard")
		if err != nil {
			t.Fatalf("Failed to create temp directory: %v", err)
		}

		// Marshal yaml
		data, err := yaml.Marshal(config)
		if err != nil {
			t.Fatalf("Failed to marshal config data: %v", err)
		}

		// Write new config file for primary
		err = os.WriteFile(fmt.Sprintf("%s/.node", shard1Dir), data, 0644)
		if err != nil {
			t.Fatalf("Failed to write config file: %v", err)
		}

		shard, err = node.New(logger, "test-key")
		if err != nil {
			t.Fatalf("Failed to create node: %v", err)
		}

		shard.Config = config

		err = shard.Open(&shard1Dir)
		if err != nil {
			t.Fatalf("Failed to open node: %v", err)
		}

	}()

	// We start replica
	go func() {
		config := &nodereplica.Config{
			MaxMemoryThreshold: 75,
			ServerConfig: &server.Config{
				Address:     "localhost:4011",
				UseTLS:      false,
				CertFile:    "",
				KeyFile:     "",
				ReadTimeout: 10,
				BufferSize:  1024,
			},
		}

		replicaDir, err := ioutil.TempDir("", "replica")
		if err != nil {
			t.Fatalf("Failed to create temp directory: %v", err)
		}

		// Marshal yaml
		data, err := yaml.Marshal(config)
		if err != nil {
			t.Fatalf("Failed to marshal config data: %v", err)
		}

		err = os.WriteFile(fmt.Sprintf("%s/.nodereplica", replicaDir), data, 0644)
		if err != nil {
			t.Fatalf("Failed to write config file: %v", err)
		}

		replica, err = nodereplica.New(logger, "test-key")
		if err != nil {
			t.Fatalf("Failed to create node: %v", err)
		}

		err = replica.Open(&replicaDir)
		if err != nil {
			t.Fatalf("Failed to open node: %v", err)
		}

	}()

	time.Sleep(time.Second) // Wait for nodes to open

	//We create a new cluster
	nr, err := New(logger, "test-key", "test-user", "test-pass")
	if err != nil {
		t.Fatalf("Failed to create cluster: %v", err)
	}

	//We open cluster in background
	go func() {
		config := &Config{
			HealthCheckInterval: 1,
			ServerConfig: &server.Config{
				Address:     "localhost:4012",
				UseTLS:      false,
				CertFile:    "/",
				KeyFile:     "/",
				ReadTimeout: 10,
				BufferSize:  1024,
			},
			NodeConfigs: []*NodeConfig{
				{
					Node: &client.Config{
						ServerAddress:  "localhost:4010",
						UseTLS:         false,
						ConnectTimeout: 5,
						WriteTimeout:   5,
						ReadTimeout:    5,
						MaxRetries:     3,
						RetryWaitTime:  1,
						BufferSize:     1024,
					},
					Replicas: []*client.Config{
						{
							ServerAddress:  "localhost:4011",
							UseTLS:         false,
							ConnectTimeout: 5,
							WriteTimeout:   5,
							ReadTimeout:    5,
							MaxRetries:     3,
							RetryWaitTime:  1,
							BufferSize:     1024,
						},
					},
				},
			},
		}

		// Marshal to yaml
		data, err := yaml.Marshal(config)
		if err != nil {
			t.Fatalf("Failed to marshal config data: %v", err)
		}

		// Write to file
		err = os.WriteFile(".cluster", data, 0644)
		if err != nil {
			t.Fatalf("Failed to write config file: %v", err)
		}

		err = nr.Open()
		if err != nil {
			t.Fatalf("Failed to open cluster: %v", err)
		}
	}()

	time.Sleep(4 * time.Second) // Wait for cluster to start and connect to primaries

	defer os.Remove(".cluster")

	tcpAddr, err := net.ResolveTCPAddr("tcp4", "localhost:4012")
	if err != nil {
		nr.Close()
		t.Fatalf("Failed to resolve address: %v", err)
	}

	// Connect to the address with tcp
	conn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}

	authStr := base64.StdEncoding.EncodeToString([]byte("test-user\\0test-pass"))

	// We authenticate
	_, err = conn.Write([]byte(fmt.Sprintf("AUTH %s\r\n", authStr)))
	if err != nil {
		conn.Close()
		nr.Close()
		t.Fatalf("Failed to authenticate: %v", err)
	}

	// We expect "OK authenticated" as response
	buf := make([]byte, 1024)

	n, err := conn.Read(buf)
	if err != nil {
		nr.Close()
		t.Fatalf("Failed to read response: %v", err)
	}

	if string(buf[:n]) != "OK authenticated\r\n" {
		conn.Close()
		nr.Close()
		t.Fatalf("Expected 'OK authenticated', got %s", string(buf[:n]))
	}

	for i := 0; i < 10; i++ {
		_, err = conn.Write([]byte(fmt.Sprintf("PUT hello%d world\r\n", i)))
		if err != nil {
			conn.Close()
			shard.Close()
			replica.Close()
			nr.Close()
			t.Fatalf("Failed to write: %v", err)
		}

		buf = make([]byte, 1024)

		n, err = conn.Read(buf)
		if err != nil {
			conn.Close()
			shard.Close()
			replica.Close()
			nr.Close()
			t.Fatalf("Failed to read response: %v", err)
		}

		if !strings.HasPrefix(string(buf[:n]), "OK") {
			conn.Close()
			shard.Close()
			replica.Close()
			nr.Close()
			t.Fatalf("Expected 'OK', got %s", string(buf[:n]))
		}
	}

	nr.NodeConnectionsLock.Lock()
	nr.NodeConnections[0].Health = false
	nr.NodeConnections[0].Client.Close()
	nr.NodeConnectionsLock.Unlock()

	time.Sleep(time.Second * 2)

	// We verify the data
	for i := 0; i < 10; i++ {
		_, err = conn.Write([]byte(fmt.Sprintf("GET hello%d\r\n", i)))
		if err != nil {
			conn.Close()
			replica.Close()
			nr.Close()
			t.Fatalf("Failed to write: %v", err)
		}

		buf = make([]byte, 1024)

		n, err = conn.Read(buf)
		if err != nil {
			conn.Close()
			replica.Close()
			nr.Close()
			t.Fatalf("Failed to read response: %v", err)
		}

		if !strings.HasPrefix(string(buf[:n]), "OK") {
			conn.Close()
			replica.Close()
			nr.Close()
			t.Fatalf("Expected 'OK', got %s", string(buf[:n]))
		}
	}

	conn.Close()

	nr.Close()
	shard.Close()
	replica.Close()

}
