package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"goim/internal/core"
)

const (
	activationFile           = ".goim-activation.json"
	activationIOTimeout      = 400 * time.Millisecond
	activationStartupTimeout = 3 * time.Second
)

type activationEndpoint struct {
	Port  int
	PID   int
	Token string
}

type activationListener struct {
	listener net.Listener
	path     string
	requests chan struct{}
	done     chan struct{}
}

// openOrActivate uses the existing database lock to elect the owner. A
// concurrent launch may find the lock before the owner publishes its endpoint.
func openOrActivate(options core.Options) (*core.Node, error) {
	deadline := time.Now().Add(activationStartupTimeout)
	for {
		node, err := core.New(options)
		if !errors.Is(err, core.ErrDataDirectoryInUse) {
			return node, err
		}
		activationErr := requestActivation(options.DataDir)
		if activationErr == nil {
			return nil, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%w，无法唤回主窗口：%v", err, activationErr)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Publish an authenticated, loopback-only endpoint while holding the data lock.
func listenForActivation(directory string) (*activationListener, error) {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return nil, err
	}
	server := &activationListener{
		listener: listener, path: filepath.Join(directory, activationFile),
		requests: make(chan struct{}, 1), done: make(chan struct{}),
	}
	endpoint := activationEndpoint{Port: listener.Addr().(*net.TCPAddr).Port, PID: os.Getpid(), Token: rand.Text()}
	if err := publishActivationEndpoint(server.path, endpoint); err != nil {
		_ = listener.Close()
		return nil, err
	}
	go server.serve(endpoint.Token)
	return server, nil
}

func publishActivationEndpoint(path string, endpoint activationEndpoint) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".goim-activation-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	writeErr := json.NewEncoder(temp).Encode(endpoint)
	if err := errors.Join(writeErr, temp.Close()); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

func (server *activationListener) serve(token string) {
	defer close(server.done)
	for {
		conn, err := server.listener.Accept()
		if err != nil {
			return
		}
		server.accept(conn, token)
		_ = conn.Close()
	}
}

func (server *activationListener) accept(conn net.Conn, token string) {
	_ = conn.SetDeadline(time.Now().Add(activationIOTimeout))
	proof := make([]byte, len(token))
	if _, err := io.ReadFull(conn, proof); err != nil || subtle.ConstantTimeCompare(proof, []byte(token)) != 1 {
		return
	}
	select {
	case server.requests <- struct{}{}:
	default: // the existing pending request will reopen the same window
	}
	_, _ = io.WriteString(conn, "1")
}

func (server *activationListener) close() {
	_ = server.listener.Close()
	<-server.done
	_ = os.Remove(server.path)
}

func requestActivation(directory string) error {
	data, err := os.ReadFile(filepath.Join(directory, activationFile))
	if err != nil {
		return err
	}
	var endpoint activationEndpoint
	if err := json.Unmarshal(data, &endpoint); err != nil {
		return err
	}
	if endpoint.Port < 1 || endpoint.Port > 65535 || endpoint.PID < 1 || len(endpoint.Token) < 26 || len(endpoint.Token) > 128 {
		return errors.New("GoIM 重开地址无效")
	}
	conn, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(endpoint.Port)), activationIOTimeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(activationIOTimeout))
	allowActivationForeground(endpoint.PID)
	if _, err := io.WriteString(conn, endpoint.Token); err != nil {
		return err
	}
	var reply [1]byte
	if _, err := io.ReadFull(conn, reply[:]); err != nil {
		return err
	}
	if reply[0] != '1' {
		return errors.New("GoIM 未确认重开请求")
	}
	return nil
}
