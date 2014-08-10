// Copyright 2014 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/tsuru/tsuru/cmd"
	"github.com/tsuru/tsuru/cmd/testing"
	"github.com/tsuru/tsuru/errors"
	ttesting "github.com/tsuru/tsuru/testing"
	"io/ioutil"
	"launchpad.net/gocheck"
	"net"
	"net/http"
	"net/http/httptest"
)

func (s *S) TestMoveContainersInfo(c *gocheck.C) {
	expected := &cmd.Info{
		Name:    "containers-move",
		Usage:   "containers-move <from host> <to host>",
		Desc:    "Move all containers from one host to another.\nThis command is especially useful for host maintenance.",
		MinArgs: 2,
	}
	c.Assert((&moveContainersCmd{}).Info(), gocheck.DeepEquals, expected)
}

func (s *S) TestMoveContainersRun(c *gocheck.C) {
	var stdout, stderr bytes.Buffer
	context := cmd.Context{
		Stdout: &stdout,
		Stderr: &stderr,
		Args:   []string{"from", "to"},
	}
	msg, _ := json.Marshal(progressLog{Message: "progress msg"})
	result := string(msg)
	trans := &testing.ConditionalTransport{
		Transport: testing.Transport{Message: result, Status: http.StatusOK},
		CondFunc: func(req *http.Request) bool {
			defer req.Body.Close()
			body, err := ioutil.ReadAll(req.Body)
			c.Assert(err, gocheck.IsNil)
			expected := map[string]string{
				"from": "from",
				"to":   "to",
			}
			result := map[string]string{}
			err = json.Unmarshal(body, &result)
			c.Assert(expected, gocheck.DeepEquals, result)
			return req.URL.Path == "/docker/containers/move" && req.Method == "POST"
		},
	}
	manager := cmd.NewManager("admin", "0.1", "admin-ver", &stdout, &stderr, nil, nil)
	client := cmd.NewClient(&http.Client{Transport: trans}, nil, manager)
	cmd := moveContainersCmd{}
	err := cmd.Run(&context, client)
	c.Assert(err, gocheck.IsNil)
	expected := "progress msg\n"
	c.Assert(stdout.String(), gocheck.Equals, expected)
}

func (s *S) TestMoveContainerInfo(c *gocheck.C) {
	expected := &cmd.Info{
		Name:    "container-move",
		Usage:   "container-move <container id> <to host>",
		Desc:    "Move specified container to another host.",
		MinArgs: 2,
	}
	c.Assert((&moveContainerCmd{}).Info(), gocheck.DeepEquals, expected)
}

func (s *S) TestMoveContainerRun(c *gocheck.C) {
	var stdout, stderr bytes.Buffer
	context := cmd.Context{
		Stdout: &stdout,
		Stderr: &stderr,
		Args:   []string{"contId", "toHost"},
	}
	msg, _ := json.Marshal(progressLog{Message: "progress msg"})
	result := string(msg)
	trans := &testing.ConditionalTransport{
		Transport: testing.Transport{Message: result, Status: http.StatusOK},
		CondFunc: func(req *http.Request) bool {
			defer req.Body.Close()
			body, err := ioutil.ReadAll(req.Body)
			c.Assert(err, gocheck.IsNil)
			expected := map[string]string{
				"to": "toHost",
			}
			result := map[string]string{}
			err = json.Unmarshal(body, &result)
			c.Assert(expected, gocheck.DeepEquals, result)
			return req.URL.Path == "/docker/container/contId/move" && req.Method == "POST"
		},
	}
	manager := cmd.NewManager("admin", "0.1", "admin-ver", &stdout, &stderr, nil, nil)
	client := cmd.NewClient(&http.Client{Transport: trans}, nil, manager)
	cmd := moveContainerCmd{}
	err := cmd.Run(&context, client)
	c.Assert(err, gocheck.IsNil)
	expected := "progress msg\n"
	c.Assert(stdout.String(), gocheck.Equals, expected)
}

func (s *S) TestRebalanceContainersInfo(c *gocheck.C) {
	expected := &cmd.Info{
		Name:    "containers-rebalance",
		Usage:   "containers-rebalance [--dry]",
		Desc:    "Move containers creating a more even distribution between docker nodes.",
		MinArgs: 0,
	}
	c.Assert((&rebalanceContainersCmd{}).Info(), gocheck.DeepEquals, expected)
}

func (s *S) TestRebalanceContainersRun(c *gocheck.C) {
	var stdout, stderr bytes.Buffer
	context := cmd.Context{
		Stdout: &stdout,
		Stderr: &stderr,
	}
	msg, _ := json.Marshal(progressLog{Message: "progress msg"})
	result := string(msg)
	expectedDry := "true"
	trans := &testing.ConditionalTransport{
		Transport: testing.Transport{Message: result, Status: http.StatusOK},
		CondFunc: func(req *http.Request) bool {
			defer req.Body.Close()
			body, err := ioutil.ReadAll(req.Body)
			c.Assert(err, gocheck.IsNil)
			expected := map[string]string{
				"dry": expectedDry,
			}
			result := map[string]string{}
			err = json.Unmarshal(body, &result)
			c.Assert(expected, gocheck.DeepEquals, result)
			return req.URL.Path == "/docker/containers/rebalance" && req.Method == "POST"
		},
	}
	manager := cmd.NewManager("admin", "0.1", "admin-ver", &stdout, &stderr, nil, nil)
	client := cmd.NewClient(&http.Client{Transport: trans}, nil, manager)
	cmd := rebalanceContainersCmd{}
	cmd.Flags().Parse(true, []string{"--dry"})
	err := cmd.Run(&context, client)
	c.Assert(err, gocheck.IsNil)
	expected := "progress msg\n"
	c.Assert(stdout.String(), gocheck.Equals, expected)
	expectedDry = "false"
	cmd2 := rebalanceContainersCmd{}
	err = cmd2.Run(&context, client)
	c.Assert(err, gocheck.IsNil)
}

func (s *S) TestFixContainersCmdRun(c *gocheck.C) {
	var buf bytes.Buffer
	context := cmd.Context{Stdout: &buf, Stderr: &buf}
	trans := &testing.ConditionalTransport{
		Transport: testing.Transport{Message: "", Status: http.StatusOK},
		CondFunc: func(req *http.Request) bool {
			return req.URL.Path == "/docker/fix-containers" && req.Method == "POST"
		},
	}
	manager := cmd.NewManager("admin", "0.1", "admin-ver", &buf, &buf, nil, nil)
	client := cmd.NewClient(&http.Client{Transport: trans}, nil, manager)
	cmd := fixContainersCmd{}
	err := cmd.Run(&context, client)
	c.Assert(err, gocheck.IsNil)
	c.Assert(buf.String(), gocheck.Equals, "")
}

func (s *S) TestFixContainersCmdInfo(c *gocheck.C) {
	expected := cmd.Info{
		Name:  "fix-containers",
		Usage: "fix-containers",
		Desc:  "Fix containers that are broken in the cluster.",
	}
	command := fixContainersCmd{}
	info := command.Info()
	c.Assert(*info, gocheck.DeepEquals, expected)
}

func (s *S) TestSSHToContainerCmdInfo(c *gocheck.C) {
	expected := cmd.Info{
		Name:    "ssh",
		Usage:   "ssh <container-id>",
		Desc:    "Open a SSH shell to the given container.",
		MinArgs: 1,
	}
	var command sshToContainerCmd
	info := command.Info()
	c.Assert(*info, gocheck.DeepEquals, expected)
}

func (s *S) TestSSHToContainerCmdRun(c *gocheck.C) {
	var closeClientConn func()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/docker/ssh/af3332d" && r.Method == "GET" && r.Header.Get("Authorization") == "bearer abc123" {
			conn, _, err := w.(http.Hijacker).Hijack()
			c.Assert(err, gocheck.IsNil)
			conn.Write([]byte("hello my friend\n"))
			conn.Write([]byte("glad to see you here\n"))
			closeClientConn()
		} else {
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()
	closeClientConn = server.CloseClientConnections
	target := "http://" + server.Listener.Addr().String()
	targetRecover := ttesting.SetTargetFile(c, []byte(target))
	defer ttesting.RollbackFile(targetRecover)
	tokenRecover := ttesting.SetTokenFile(c, []byte("abc123"))
	defer ttesting.RollbackFile(tokenRecover)
	var stdout, stderr, stdin bytes.Buffer
	context := cmd.Context{
		Args:   []string{"af3332d"},
		Stdout: &stdout,
		Stderr: &stderr,
		Stdin:  &stdin,
	}
	var command sshToContainerCmd
	err := command.Run(&context, nil)
	c.Assert(err, gocheck.IsNil)
	c.Assert(stdout.String(), gocheck.Equals, "hello my friend\nglad to see you here\n")
}

func (s *S) TestSSHToContainerCmdNoToken(c *gocheck.C) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "You must provide a valid Authorization header", http.StatusUnauthorized)
	}))
	defer server.Close()
	target := "http://" + server.Listener.Addr().String()
	targetRecover := ttesting.SetTargetFile(c, []byte(target))
	defer ttesting.RollbackFile(targetRecover)
	var buf bytes.Buffer
	context := cmd.Context{
		Args:   []string{"af3332d"},
		Stdout: &buf,
		Stderr: &buf,
		Stdin:  &buf,
	}
	var command sshToContainerCmd
	err := command.Run(&context, nil)
	c.Assert(err, gocheck.FitsTypeOf, &errors.HTTP{})
	httpErr := err.(*errors.HTTP)
	c.Assert(httpErr.Code, gocheck.Equals, 401)
	c.Assert(httpErr.Message, gocheck.Equals, "HTTP/1.1 401 Unauthorized")
}

func (s *S) TestSSHToContainerCmdSmallData(c *gocheck.C) {
	var closeClientConn func()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		c.Assert(err, gocheck.IsNil)
		conn.Write([]byte("hello"))
		closeClientConn()
	}))
	defer server.Close()
	closeClientConn = server.CloseClientConnections
	target := "http://" + server.Listener.Addr().String()
	targetRecover := ttesting.SetTargetFile(c, []byte(target))
	defer ttesting.RollbackFile(targetRecover)
	var stdout, stderr, stdin bytes.Buffer
	context := cmd.Context{
		Args:   []string{"af3332d"},
		Stdout: &stdout,
		Stderr: &stderr,
		Stdin:  &stdin,
	}
	var command sshToContainerCmd
	err := command.Run(&context, nil)
	c.Assert(err, gocheck.IsNil)
	c.Assert(stdout.String(), gocheck.Equals, "hello")
}

func (s *S) TestSSHToContainerCmdLongNoNewLine(c *gocheck.C) {
	var closeClientConn func()
	expected := fmt.Sprintf("%0200s", "x")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		c.Assert(err, gocheck.IsNil)
		conn.Write([]byte(expected))
		closeClientConn()
	}))
	defer server.Close()
	closeClientConn = server.CloseClientConnections
	target := "http://" + server.Listener.Addr().String()
	targetRecover := ttesting.SetTargetFile(c, []byte(target))
	defer ttesting.RollbackFile(targetRecover)
	var stdout, stderr, stdin bytes.Buffer
	context := cmd.Context{
		Args:   []string{"af3332d"},
		Stdout: &stdout,
		Stderr: &stderr,
		Stdin:  &stdin,
	}
	var command sshToContainerCmd
	err := command.Run(&context, nil)
	c.Assert(err, gocheck.IsNil)
	c.Assert(stdout.String(), gocheck.Equals, expected)
}

func (s *S) TestSSHToContainerCmdConnectionRefused(c *gocheck.C) {
	server := httptest.NewServer(nil)
	addr := server.Listener.Addr().String()
	server.Close()
	targetRecover := ttesting.SetTargetFile(c, []byte("http://"+addr))
	defer ttesting.RollbackFile(targetRecover)
	tokenRecover := ttesting.SetTokenFile(c, []byte("abc123"))
	defer ttesting.RollbackFile(tokenRecover)
	var buf bytes.Buffer
	context := cmd.Context{
		Args:   []string{"af3332d"},
		Stdout: &buf,
		Stderr: &buf,
		Stdin:  &buf,
	}
	var command sshToContainerCmd
	err := command.Run(&context, nil)
	c.Assert(err, gocheck.NotNil)
	opErr, ok := err.(*net.OpError)
	c.Assert(ok, gocheck.Equals, true)
	c.Assert(opErr.Net, gocheck.Equals, "tcp")
	c.Assert(opErr.Op, gocheck.Equals, "dial")
	c.Assert(opErr.Addr.String(), gocheck.Equals, addr)
}
