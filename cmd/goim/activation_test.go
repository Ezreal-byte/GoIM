package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goim/internal/core"
)

func TestSecondLaunchActivatesOwnerOfSameDataDirectory(t *testing.T) {
	options := core.Options{
		DataDir: t.TempDir(), Listen: "127.0.0.1:0", DisableDiscovery: true, DisableCompatibility: true,
	}
	owner, err := openOrActivate(options)
	if err != nil || owner == nil {
		t.Fatalf("first launch: node=%v error=%v", owner != nil, err)
	}
	defer owner.Close()
	if second, err := core.New(options); !errors.Is(err, core.ErrDataDirectoryInUse) {
		if second != nil {
			second.Close()
		}
		t.Fatalf("opened the same data directory twice: %v", err)
	}

	result := make(chan error, 1)
	go func() {
		second, err := openOrActivate(options)
		if second != nil {
			second.Close()
			err = errors.New("second launch created another node")
		}
		result <- err
	}()
	// Publish after another launch can see the lock, as on simultaneous startup.
	time.Sleep(80 * time.Millisecond)
	server, err := listenForActivation(owner.DataDir())
	if err != nil {
		t.Fatal(err)
	}
	defer server.close()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second launch did not finish")
	}
	select {
	case <-server.requests:
	default:
		t.Fatal("owner did not receive a reopen request")
	}

	independent := options
	independent.DataDir = t.TempDir()
	node, err := openOrActivate(independent)
	if err != nil || node == nil {
		t.Fatalf("another data directory should have its own instance: %v", err)
	}
	node.Close()
}

func TestActivationRejectsWrongTokenAndClearsEndpointOnClose(t *testing.T) {
	server, err := listenForActivation(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		server.close()
		if _, err := os.Stat(server.path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("activation endpoint after shutdown: %v", err)
		}
	}()
	data, err := os.ReadFile(server.path)
	if err != nil {
		t.Fatal(err)
	}
	var endpoint activationEndpoint
	if err := json.Unmarshal(data, &endpoint); err != nil {
		t.Fatal(err)
	}
	invalid := t.TempDir()
	endpoint.Token = "!" + endpoint.Token[1:]
	if err := publishActivationEndpoint(filepath.Join(invalid, activationFile), endpoint); err != nil {
		t.Fatal(err)
	}
	if err := requestActivation(invalid); err == nil {
		t.Fatal("activation with the wrong token succeeded")
	}
	select {
	case <-server.requests:
		t.Fatal("invalid request reached the owner")
	default:
	}
}
