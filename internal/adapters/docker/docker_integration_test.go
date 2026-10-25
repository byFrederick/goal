//go:build integration

package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync/atomic"
	"testing"

	"github.com/moby/moby/client"
)

// testOrg labels the tests' containers; no store knows it.
const testOrg = "org-reap-it"

// runSeq numbers the tests' runs, so their containers' names never collide.
var runSeq atomic.Int64

// newTestDocker returns a client of the local Docker, closed when the test ends. It skips the
// test when Docker is not reachable.
func newTestDocker(t *testing.T) *client.Client {
	t.Helper()

	docker, err := client.New(client.FromEnv)
	if err != nil {
		t.Skipf("docker client: %v", err)
	}

	t.Cleanup(func() { _ = docker.Close() })

	if _, err := docker.Ping(t.Context(), client.PingOptions{}); err != nil {
		t.Skipf("docker is not reachable: %v", err)
	}

	return docker
}

// buildImage builds tag from a build context of the given files.
func buildImage(docker *client.Client, tag string, files map[string][]byte) error {
	var buf bytes.Buffer

	tw := tar.NewWriter(&buf)

	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body))}); err != nil {
			return err
		}

		if _, err := tw.Write(body); err != nil {
			return err
		}
	}

	if err := tw.Close(); err != nil {
		return err
	}

	res, err := docker.ImageBuild(context.Background(), &buf, client.ImageBuildOptions{Tags: []string{tag}, Remove: true})
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()

	dec := json.NewDecoder(res.Body)

	for {
		var msg struct {
			Error string `json:"error"`
		}

		if err := dec.Decode(&msg); errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return err
		}

		if msg.Error != "" {
			return errors.New(msg.Error)
		}
	}
}
