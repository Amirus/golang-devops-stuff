// Copyright 2014 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/tsuru/config"
	"github.com/tsuru/docker-cluster/cluster"
	"github.com/tsuru/tsuru/app"
	"github.com/tsuru/tsuru/db"
	"github.com/tsuru/tsuru/errors"
	"github.com/tsuru/tsuru/iaas"
	"github.com/tsuru/tsuru/provision"
	"github.com/tsuru/tsuru/safe"
	"github.com/tsuru/tsuru/testing"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"io/ioutil"
	"launchpad.net/gocheck"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"strings"
)

type TestIaaS struct{}

func (TestIaaS) DeleteMachine(m *iaas.Machine) error {
	return nil
}

func (TestIaaS) CreateMachine(params map[string]string) (*iaas.Machine, error) {
	m := iaas.Machine{
		Id:      params["id"],
		Status:  "running",
		Address: params["id"] + ".fake.host",
	}
	return &m, nil
}

func (TestIaaS) Describe() string {
	return "my iaas description"
}

type HandlersSuite struct {
	conn   *db.Storage
	server *httptest.Server
}

var _ = gocheck.Suite(&HandlersSuite{})

func (s *HandlersSuite) SetUpSuite(c *gocheck.C) {
	config.Set("database:name", "docker_provision_handlers_tests_s")
	config.Set("docker:collection", "docker_handler_suite")
	config.Set("docker:run-cmd:port", 8888)
	config.Set("docker:router", "fake")
	config.Set("docker:cluster:storage", "redis")
	config.Set("docker:cluster:redis-prefix", "redis-scheduler-storage-handlers-test")
	config.Set("iaas:default", "test-iaas")
	config.Set("iaas:node-protocol", "http")
	config.Set("iaas:node-port", 1234)
	var err error
	s.conn, err = db.Conn()
	c.Assert(err, gocheck.IsNil)
	s.conn.Collection(schedulerCollection).RemoveAll(nil)
	s.server = httptest.NewServer(nil)
}

func (s *HandlersSuite) SetUpTest(c *gocheck.C) {
	clearRedisKeys("redis-scheduler-storage-handlers-test*", c)
}

func (s *HandlersSuite) TearDownSuite(c *gocheck.C) {
	coll := collection()
	defer coll.Close()
	err := coll.Database.DropDatabase()
	c.Assert(err, gocheck.IsNil)
	s.conn.Close()
}

func (s *HandlersSuite) TestAddNodeHandler(c *gocheck.C) {
	dCluster, _ = cluster.New(segregatedScheduler{}, &cluster.MapStorage{})
	p := Pool{Name: "pool1"}
	s.conn.Collection(schedulerCollection).Insert(p)
	defer s.conn.Collection(schedulerCollection).RemoveId("pool1")
	json := fmt.Sprintf(`{"address": "%s", "pool": "pool1"}`, s.server.URL)
	b := bytes.NewBufferString(json)
	req, err := http.NewRequest("POST", "/docker/node?register=true", b)
	c.Assert(err, gocheck.IsNil)
	rec := httptest.NewRecorder()
	err = addNodeHandler(rec, req, nil)
	c.Assert(err, gocheck.IsNil)
	nodes, err := dCluster.Nodes()
	c.Assert(err, gocheck.IsNil)
	c.Assert(nodes, gocheck.HasLen, 1)
	c.Assert(nodes[0].Address, gocheck.Equals, s.server.URL)
	c.Assert(nodes[0].Metadata, gocheck.DeepEquals, map[string]string{
		"pool": "pool1",
	})
}

func (s *HandlersSuite) TestAddNodeHandlerCreatingAnIaasMachine(c *gocheck.C) {
	iaas.RegisterIaasProvider("test-iaas", TestIaaS{})
	dCluster, _ = cluster.New(segregatedScheduler{}, &cluster.MapStorage{})
	p := Pool{Name: "pool1"}
	s.conn.Collection(schedulerCollection).Insert(p)
	defer s.conn.Collection(schedulerCollection).RemoveId("pool1")
	b := bytes.NewBufferString(`{"pool": "pool1", "id": "test1"}`)
	req, err := http.NewRequest("POST", "/docker/node?register=false", b)
	c.Assert(err, gocheck.IsNil)
	rec := httptest.NewRecorder()
	err = addNodeHandler(rec, req, nil)
	c.Assert(err, gocheck.IsNil)
	var result map[string]string
	err = json.NewDecoder(rec.Body).Decode(&result)
	c.Assert(err, gocheck.IsNil)
	c.Assert(result, gocheck.DeepEquals, map[string]string{"description": "my iaas description"})
	nodes, err := dCluster.Nodes()
	c.Assert(err, gocheck.IsNil)
	c.Assert(nodes, gocheck.HasLen, 1)
	c.Assert(nodes[0].Address, gocheck.Equals, "http://test1.fake.host:1234")
	c.Assert(nodes[0].Metadata, gocheck.DeepEquals, map[string]string{
		"id":   "test1",
		"pool": "pool1",
		"iaas": "test-iaas",
	})
}

func (s *HandlersSuite) TestAddNodeHandlerCreatingAnIaasMachineExplicit(c *gocheck.C) {
	iaas.RegisterIaasProvider("test-iaas", TestIaaS{})
	iaas.RegisterIaasProvider("another-test-iaas", TestIaaS{})
	dCluster, _ = cluster.New(segregatedScheduler{}, &cluster.MapStorage{})
	p := Pool{Name: "pool1"}
	s.conn.Collection(schedulerCollection).Insert(p)
	defer s.conn.Collection(schedulerCollection).RemoveId("pool1")
	b := bytes.NewBufferString(`{"pool": "pool1", "id": "test1", "iaas": "another-test-iaas"}`)
	req, err := http.NewRequest("POST", "/docker/node?register=false", b)
	c.Assert(err, gocheck.IsNil)
	rec := httptest.NewRecorder()
	err = addNodeHandler(rec, req, nil)
	c.Assert(err, gocheck.IsNil)
	nodes, err := dCluster.Nodes()
	c.Assert(err, gocheck.IsNil)
	c.Assert(nodes, gocheck.HasLen, 1)
	c.Assert(nodes[0].Address, gocheck.Equals, "http://test1.fake.host:1234")
	c.Assert(nodes[0].Metadata, gocheck.DeepEquals, map[string]string{
		"id":   "test1",
		"pool": "pool1",
		"iaas": "another-test-iaas",
	})
}

func (s *HandlersSuite) TestAddNodeHandlerWithoutdCluster(c *gocheck.C) {
	p := Pool{Name: "pool1"}
	s.conn.Collection(schedulerCollection).Insert(p)
	defer s.conn.Collection(schedulerCollection).RemoveId("pool1")
	config.Set("docker:segregate", true)
	defer config.Unset("docker:segregate")
	config.Set("docker:cluster:redis-server", "127.0.0.1:6379")
	defer config.Unset("docker:cluster:redis-server")
	dCluster = nil
	b := bytes.NewBufferString(fmt.Sprintf(`{"address": "%s", "pool": "pool1"}`, s.server.URL))
	req, err := http.NewRequest("POST", "/docker/node?register=true", b)
	c.Assert(err, gocheck.IsNil)
	rec := httptest.NewRecorder()
	err = addNodeHandler(rec, req, nil)
	c.Assert(err, gocheck.IsNil)
	nodes, err := dockerCluster().Nodes()
	c.Assert(err, gocheck.IsNil)
	c.Assert(nodes, gocheck.HasLen, 1)
	c.Assert(nodes[0].Address, gocheck.Equals, s.server.URL)
	c.Assert(nodes[0].Metadata, gocheck.DeepEquals, map[string]string{
		"pool": "pool1",
	})
}

func (s *HandlersSuite) TestAddNodeHandlerWithoutdAddress(c *gocheck.C) {
	config.Set("docker:cluster:redis-server", "127.0.0.1:6379")
	defer config.Unset("docker:cluster:redis-server")
	b := bytes.NewBufferString(`{"pool": "pool1"}`)
	req, err := http.NewRequest("POST", "/docker/node?register=true", b)
	c.Assert(err, gocheck.IsNil)
	rec := httptest.NewRecorder()
	err = addNodeHandler(rec, req, nil)
	var result map[string]string
	err = json.NewDecoder(rec.Body).Decode(&result)
	c.Assert(err, gocheck.IsNil)
	c.Assert(rec.Code, gocheck.Equals, http.StatusBadRequest)
	c.Assert(result["error"], gocheck.Matches, "address=url parameter is required")
}

func (s *HandlersSuite) TestAddNodeHandlerWithInvalidURLAddress(c *gocheck.C) {
	config.Set("docker:cluster:redis-server", "127.0.0.1:6379")
	defer config.Unset("docker:cluster:redis-server")
	b := bytes.NewBufferString(`{"address": "/invalid", "pool": "pool1"}`)
	req, err := http.NewRequest("POST", "/docker/node?register=true", b)
	c.Assert(err, gocheck.IsNil)
	rec := httptest.NewRecorder()
	err = addNodeHandler(rec, req, nil)
	c.Assert(err, gocheck.IsNil)
	var result map[string]string
	err = json.NewDecoder(rec.Body).Decode(&result)
	c.Assert(err, gocheck.IsNil)
	c.Assert(rec.Code, gocheck.Equals, http.StatusBadRequest)
	c.Assert(result["error"], gocheck.Matches, "Invalid address url: host cannot be empty")
	b = bytes.NewBufferString(`{"address": "xxx://abc/invalid", "pool": "pool1"}`)
	req, err = http.NewRequest("POST", "/docker/node?register=true", b)
	c.Assert(err, gocheck.IsNil)
	rec = httptest.NewRecorder()
	err = addNodeHandler(rec, req, nil)
	c.Assert(err, gocheck.IsNil)
	err = json.NewDecoder(rec.Body).Decode(&result)
	c.Assert(err, gocheck.IsNil)
	c.Assert(rec.Code, gocheck.Equals, http.StatusBadRequest)
	c.Assert(result["error"], gocheck.Matches, `Invalid address url: scheme must be http\[s\]`)
}

func (s *HandlersSuite) TestValidateNodeAddress(c *gocheck.C) {
	err := validateNodeAddress("/invalid")
	c.Assert(err, gocheck.ErrorMatches, "Invalid address url: host cannot be empty")
	err = validateNodeAddress("xxx://abc/invalid")
	c.Assert(err, gocheck.ErrorMatches, `Invalid address url: scheme must be http\[s\]`)
	err = validateNodeAddress("")
	c.Assert(err, gocheck.ErrorMatches, "address=url parameter is required")
}

func (s *HandlersSuite) TestRemoveNodeHandler(c *gocheck.C) {
	var err error
	dCluster, err = cluster.New(nil, &cluster.MapStorage{})
	c.Assert(err, gocheck.IsNil)
	err = dCluster.Register("host.com:4243", nil)
	c.Assert(err, gocheck.IsNil)
	b := bytes.NewBufferString(`{"address": "host.com:4243"}`)
	req, err := http.NewRequest("POST", "/node/remove", b)
	c.Assert(err, gocheck.IsNil)
	rec := httptest.NewRecorder()
	err = removeNodeHandler(rec, req, nil)
	c.Assert(err, gocheck.IsNil)
	nodes, err := dCluster.Nodes()
	c.Assert(len(nodes), gocheck.Equals, 0)
}

func (s *HandlersSuite) TestRemoveNodeHandlerWithoutRemoveIaaS(c *gocheck.C) {
	iaas.RegisterIaasProvider("some-iaas", TestIaaS{})
	machine, err := iaas.CreateMachineForIaaS("some-iaas", map[string]string{})
	c.Assert(err, gocheck.IsNil)
	dCluster, err = cluster.New(nil, &cluster.MapStorage{})
	c.Assert(err, gocheck.IsNil)
	err = dCluster.Register(fmt.Sprintf("http://%s:4243", machine.Address), nil)
	c.Assert(err, gocheck.IsNil)
	b := bytes.NewBufferString(fmt.Sprintf(`{"address": "http://%s:4243", "remove_iaas": "false"}`, machine.Address))
	req, err := http.NewRequest("POST", "/node/remove", b)
	c.Assert(err, gocheck.IsNil)
	rec := httptest.NewRecorder()
	err = removeNodeHandler(rec, req, nil)
	c.Assert(err, gocheck.IsNil)
	nodes, err := dCluster.Nodes()
	c.Assert(len(nodes), gocheck.Equals, 0)
	dbM, err := iaas.FindMachineById(machine.Id)
	c.Assert(err, gocheck.IsNil)
	c.Assert(dbM.Id, gocheck.Equals, machine.Id)
}

func (s *HandlersSuite) TestRemoveNodeHandlerRemoveIaaS(c *gocheck.C) {
	iaas.RegisterIaasProvider("my-xxx-iaas", TestIaaS{})
	machine, err := iaas.CreateMachineForIaaS("my-xxx-iaas", map[string]string{})
	c.Assert(err, gocheck.IsNil)
	dCluster, err = cluster.New(nil, &cluster.MapStorage{})
	c.Assert(err, gocheck.IsNil)
	err = dCluster.Register(fmt.Sprintf("http://%s:4243", machine.Address), nil)
	c.Assert(err, gocheck.IsNil)
	b := bytes.NewBufferString(fmt.Sprintf(`{"address": "http://%s:4243", "remove_iaas": "true"}`, machine.Address))
	req, err := http.NewRequest("POST", "/node/remove", b)
	c.Assert(err, gocheck.IsNil)
	rec := httptest.NewRecorder()
	err = removeNodeHandler(rec, req, nil)
	c.Assert(err, gocheck.IsNil)
	nodes, err := dCluster.Nodes()
	c.Assert(len(nodes), gocheck.Equals, 0)
	_, err = iaas.FindMachineById(machine.Id)
	c.Assert(err, gocheck.Equals, mgo.ErrNotFound)
}

func (s *HandlersSuite) TestListNodeHandler(c *gocheck.C) {
	var result struct {
		Nodes    []cluster.Node `json:"nodes"`
		Machines []iaas.Machine `json:"machines"`
	}
	var err error
	dCluster, err = cluster.New(nil, &cluster.MapStorage{})
	c.Assert(err, gocheck.IsNil)
	err = dCluster.Register("host1.com:4243", map[string]string{"pool": "pool1"})
	c.Assert(err, gocheck.IsNil)
	err = dCluster.Register("host2.com:4243", map[string]string{"pool": "pool2", "foo": "bar"})
	c.Assert(err, gocheck.IsNil)
	req, err := http.NewRequest("GET", "/node/", nil)
	rec := httptest.NewRecorder()
	err = listNodeHandler(rec, req, nil)
	c.Assert(err, gocheck.IsNil)
	body, err := ioutil.ReadAll(rec.Body)
	c.Assert(err, gocheck.IsNil)
	err = json.Unmarshal(body, &result)
	c.Assert(err, gocheck.IsNil)
	c.Assert(result.Nodes[0].Address, gocheck.Equals, "host1.com:4243")
	c.Assert(result.Nodes[0].Metadata, gocheck.DeepEquals, map[string]string{"pool": "pool1"})
	c.Assert(result.Nodes[1].Address, gocheck.Equals, "host2.com:4243")
	c.Assert(result.Nodes[1].Metadata, gocheck.DeepEquals, map[string]string{"pool": "pool2", "foo": "bar"})
}

func (s *HandlersSuite) TestFixContainerHandler(c *gocheck.C) {
	coll := collection()
	defer coll.Close()
	err := coll.Insert(
		container{
			ID:       "9930c24f1c4x",
			AppName:  "makea",
			Type:     "python",
			Status:   provision.StatusStarted.String(),
			IP:       "127.0.0.4",
			HostPort: "9025",
			HostAddr: "127.0.0.1",
		},
	)
	c.Assert(err, gocheck.IsNil)
	defer coll.RemoveAll(bson.M{"appname": "makea"})
	cleanup, server := startDocker()
	defer cleanup()
	var storage cluster.MapStorage
	storage.StoreContainer("9930c24f1c4x", server.URL)
	cmutex.Lock()
	dCluster, err = cluster.New(nil, &storage,
		cluster.Node{Address: server.URL},
	)
	cmutex.Unlock()
	request, err := http.NewRequest("POST", "/fix-containers", nil)
	c.Assert(err, gocheck.IsNil)
	recorder := httptest.NewRecorder()
	err = fixContainersHandler(recorder, request, nil)
	c.Assert(err, gocheck.IsNil)
	cont, err := getContainer("9930c24f1c4x")
	c.Assert(err, gocheck.IsNil)
	c.Assert(cont.IP, gocheck.Equals, "127.0.0.9")
	c.Assert(cont.HostPort, gocheck.Equals, "9999")
}

func (s *HandlersSuite) TestListContainersByHostHandler(c *gocheck.C) {
	var result []container
	coll := collection()
	dCluster, _ = cluster.New(segregatedScheduler{}, nil)
	err := coll.Insert(container{ID: "blabla", Type: "python", HostAddr: "http://cittavld1182.globoi.com"})
	c.Assert(err, gocheck.IsNil)
	defer coll.Remove(bson.M{"id": "blabla"})
	err = coll.Insert(container{ID: "bleble", Type: "java", HostAddr: "http://cittavld1182.globoi.com"})
	c.Assert(err, gocheck.IsNil)
	defer coll.Remove(bson.M{"id": "bleble"})
	req, err := http.NewRequest("GET", "/node/cittavld1182.globoi.com/containers?:address=http://cittavld1182.globoi.com", nil)
	rec := httptest.NewRecorder()
	err = listContainersHandler(rec, req, nil)
	c.Assert(err, gocheck.IsNil)
	body, err := ioutil.ReadAll(rec.Body)
	c.Assert(err, gocheck.IsNil)
	err = json.Unmarshal(body, &result)
	c.Assert(err, gocheck.IsNil)
	c.Assert(result[0].ID, gocheck.DeepEquals, "blabla")
	c.Assert(result[0].Type, gocheck.DeepEquals, "python")
	c.Assert(result[0].HostAddr, gocheck.DeepEquals, "http://cittavld1182.globoi.com")
	c.Assert(result[1].ID, gocheck.DeepEquals, "bleble")
	c.Assert(result[1].Type, gocheck.DeepEquals, "java")
	c.Assert(result[1].HostAddr, gocheck.DeepEquals, "http://cittavld1182.globoi.com")
}

func (s *HandlersSuite) TestListContainersByAppHandler(c *gocheck.C) {
	var result []container
	coll := collection()
	dCluster, _ = cluster.New(segregatedScheduler{}, nil)
	err := coll.Insert(container{ID: "blabla", AppName: "appbla", HostAddr: "http://cittavld1182.globoi.com"})
	c.Assert(err, gocheck.IsNil)
	defer coll.Remove(bson.M{"id": "blabla"})
	err = coll.Insert(container{ID: "bleble", AppName: "appbla", HostAddr: "http://cittavld1180.globoi.com"})
	c.Assert(err, gocheck.IsNil)
	defer coll.Remove(bson.M{"id": "bleble"})
	req, err := http.NewRequest("GET", "/node/appbla/containers?:appname=appbla", nil)
	rec := httptest.NewRecorder()
	err = listContainersHandler(rec, req, nil)
	c.Assert(err, gocheck.IsNil)
	body, err := ioutil.ReadAll(rec.Body)
	c.Assert(err, gocheck.IsNil)
	err = json.Unmarshal(body, &result)
	c.Assert(err, gocheck.IsNil)
	c.Assert(result[0].ID, gocheck.DeepEquals, "blabla")
	c.Assert(result[0].AppName, gocheck.DeepEquals, "appbla")
	c.Assert(result[0].HostAddr, gocheck.DeepEquals, "http://cittavld1182.globoi.com")
	c.Assert(result[1].ID, gocheck.DeepEquals, "bleble")
	c.Assert(result[1].AppName, gocheck.DeepEquals, "appbla")
	c.Assert(result[1].HostAddr, gocheck.DeepEquals, "http://cittavld1180.globoi.com")
}

func (s *HandlersSuite) TestMoveContainersEmptyBodyHandler(c *gocheck.C) {
	b := bytes.NewBufferString("")
	req, err := http.NewRequest("POST", "/containers/move", b)
	rec := httptest.NewRecorder()
	err = moveContainersHandler(rec, req, nil)
	c.Assert(err, gocheck.NotNil)
	c.Assert(err.Error(), gocheck.Equals, "unexpected end of JSON input")
}

func (s *HandlersSuite) TestMoveContainersEmptyToHandler(c *gocheck.C) {
	b := bytes.NewBufferString(`{"from": "fromhost", "to": ""}`)
	req, err := http.NewRequest("POST", "/containers/move", b)
	rec := httptest.NewRecorder()
	err = moveContainersHandler(rec, req, nil)
	c.Assert(err, gocheck.NotNil)
	c.Assert(err.Error(), gocheck.Equals, "Invalid params: from: fromhost - to: ")
}

func (s *HandlersSuite) TestMoveContainersHandler(c *gocheck.C) {
	b := bytes.NewBufferString(`{"from": "localhost", "to": "127.0.0.1"}`)
	req, err := http.NewRequest("POST", "/containers/move", b)
	rec := httptest.NewRecorder()
	err = moveContainersHandler(rec, req, nil)
	c.Assert(err, gocheck.IsNil)
	body, err := ioutil.ReadAll(rec.Body)
	c.Assert(err, gocheck.IsNil)
	validJson := fmt.Sprintf("[%s]", strings.Replace(strings.Trim(string(body), "\n "), "\n", ",", -1))
	var result []progressLog
	err = json.Unmarshal([]byte(validJson), &result)
	c.Assert(err, gocheck.IsNil)
	c.Assert(result, gocheck.DeepEquals, []progressLog{
		{Message: "No units to move in localhost."},
		{Message: "Containers moved successfully!"},
	})
}

func (s *HandlersSuite) TestMoveContainerHandler(c *gocheck.C) {
	b := bytes.NewBufferString(`{"to": "127.0.0.1"}`)
	req, err := http.NewRequest("POST", "/container/myid/move?:id=myid", b)
	rec := httptest.NewRecorder()
	err = moveContainerHandler(rec, req, nil)
	c.Assert(err, gocheck.IsNil)
	body, err := ioutil.ReadAll(rec.Body)
	c.Assert(err, gocheck.IsNil)
	var result progressLog
	err = json.Unmarshal(body, &result)
	c.Assert(err, gocheck.IsNil)
	expected := progressLog{Message: "Error trying to move container: not found"}
	c.Assert(result, gocheck.DeepEquals, expected)
}

func (s *S) TestRebalanceContainersEmptyBodyHandler(c *gocheck.C) {
	cluster, err := s.startMultipleServersCluster()
	c.Assert(err, gocheck.IsNil)
	defer s.stopMultipleServersCluster(cluster)
	err = newImage("tsuru/python", s.server.URL())
	c.Assert(err, gocheck.IsNil)
	appInstance := testing.NewFakeApp("myapp", "python", 0)
	var p dockerProvisioner
	defer p.Destroy(appInstance)
	p.Provision(appInstance)
	coll := collection()
	defer coll.Close()
	coll.Insert(container{ID: "container-id", AppName: appInstance.GetName(), Version: "container-version", Image: "tsuru/python"})
	defer coll.RemoveAll(bson.M{"appname": appInstance.GetName()})
	units, err := addContainersWithHost(nil, appInstance, 5, "localhost")
	c.Assert(err, gocheck.IsNil)
	conn, err := db.Conn()
	c.Assert(err, gocheck.IsNil)
	defer conn.Close()
	appStruct := &app.App{
		Name:     appInstance.GetName(),
		Platform: appInstance.GetPlatform(),
	}
	err = conn.Apps().Insert(appStruct)
	c.Assert(err, gocheck.IsNil)
	defer conn.Apps().Remove(bson.M{"name": appStruct.Name})
	err = conn.Apps().Update(
		bson.M{"name": appStruct.Name},
		bson.M{"$set": bson.M{"units": units}},
	)
	c.Assert(err, gocheck.IsNil)
	b := bytes.NewBufferString("")
	req, err := http.NewRequest("POST", "/containers/move", b)
	rec := httptest.NewRecorder()
	err = rebalanceContainersHandler(rec, req, nil)
	c.Assert(err, gocheck.IsNil)
	body, err := ioutil.ReadAll(rec.Body)
	c.Assert(err, gocheck.IsNil)
	validJson := fmt.Sprintf("[%s]", strings.Replace(strings.Trim(string(body), "\n "), "\n", ",", -1))
	var result []progressLog
	err = json.Unmarshal([]byte(validJson), &result)
	c.Assert(err, gocheck.IsNil)
	c.Assert(len(result), gocheck.Equals, 10)
	c.Assert(result[0].Message, gocheck.Equals, "Rebalancing app \"myapp\" (6 units)...")
	c.Assert(result[1].Message, gocheck.Equals, "Trying to move 2 units for \"myapp\" from localhost...")
	c.Assert(result[2].Message, gocheck.Matches, "Moving unit .*")
	c.Assert(result[3].Message, gocheck.Matches, "Moving unit .*")
	c.Assert(result[8].Message, gocheck.Equals, "Rebalance finished for \"myapp\"")
	c.Assert(result[9].Message, gocheck.Equals, "Containers rebalanced successfully!")
}

func (s *S) TestRebalanceContainersDryBodyHandler(c *gocheck.C) {
	cluster, err := s.startMultipleServersCluster()
	c.Assert(err, gocheck.IsNil)
	defer s.stopMultipleServersCluster(cluster)
	err = newImage("tsuru/python", s.server.URL())
	c.Assert(err, gocheck.IsNil)
	appInstance := testing.NewFakeApp("myapp", "python", 0)
	var p dockerProvisioner
	defer p.Destroy(appInstance)
	p.Provision(appInstance)
	coll := collection()
	defer coll.Close()
	coll.Insert(container{ID: "container-id", AppName: appInstance.GetName(), Version: "container-version", Image: "tsuru/python"})
	defer coll.RemoveAll(bson.M{"appname": appInstance.GetName()})
	units, err := addContainersWithHost(nil, appInstance, 5, "localhost")
	c.Assert(err, gocheck.IsNil)
	conn, err := db.Conn()
	c.Assert(err, gocheck.IsNil)
	defer conn.Close()
	appStruct := &app.App{
		Name:     appInstance.GetName(),
		Platform: appInstance.GetPlatform(),
	}
	err = conn.Apps().Insert(appStruct)
	c.Assert(err, gocheck.IsNil)
	defer conn.Apps().Remove(bson.M{"name": appStruct.Name})
	err = conn.Apps().Update(
		bson.M{"name": appStruct.Name},
		bson.M{"$set": bson.M{"units": units}},
	)
	c.Assert(err, gocheck.IsNil)
	b := bytes.NewBufferString(`{"dry": "true"}`)
	req, err := http.NewRequest("POST", "/containers/move", b)
	rec := httptest.NewRecorder()
	err = rebalanceContainersHandler(rec, req, nil)
	c.Assert(err, gocheck.IsNil)
	body, err := ioutil.ReadAll(rec.Body)
	c.Assert(err, gocheck.IsNil)
	validJson := fmt.Sprintf("[%s]", strings.Replace(strings.Trim(string(body), "\n "), "\n", ",", -1))
	var result []progressLog
	err = json.Unmarshal([]byte(validJson), &result)
	c.Assert(err, gocheck.IsNil)
	c.Assert(len(result), gocheck.Equals, 6)
	c.Assert(result[0].Message, gocheck.Equals, "Rebalancing app \"myapp\" (6 units)...")
	c.Assert(result[1].Message, gocheck.Equals, "Trying to move 2 units for \"myapp\" from localhost...")
	c.Assert(result[2].Message, gocheck.Matches, "Would move unit .*")
	c.Assert(result[3].Message, gocheck.Matches, "Would move unit .*")
	c.Assert(result[4].Message, gocheck.Equals, "Rebalance finished for \"myapp\"")
	c.Assert(result[5].Message, gocheck.Equals, "Containers rebalanced successfully!")
}

func (s *HandlersSuite) TestAddPoolHandler(c *gocheck.C) {
	b := bytes.NewBufferString(`{"pool": "pool1"}`)
	req, err := http.NewRequest("POST", "/pool", b)
	c.Assert(err, gocheck.IsNil)
	rec := httptest.NewRecorder()
	err = addPoolHandler(rec, req, nil)
	c.Assert(err, gocheck.IsNil)
	defer s.conn.Collection(schedulerCollection).RemoveId("pool1")
	n, err := s.conn.Collection(schedulerCollection).FindId("pool1").Count()
	c.Assert(err, gocheck.IsNil)
	c.Assert(n, gocheck.Equals, 1)
}

func (s *HandlersSuite) TestRemovePoolHandler(c *gocheck.C) {
	pool := Pool{Name: "pool1"}
	err := s.conn.Collection(schedulerCollection).Insert(pool)
	c.Assert(err, gocheck.IsNil)
	b := bytes.NewBufferString(`{"pool": "pool1"}`)
	req, err := http.NewRequest("DELETE", "/pool", b)
	c.Assert(err, gocheck.IsNil)
	rec := httptest.NewRecorder()
	err = removePoolHandler(rec, req, nil)
	c.Assert(err, gocheck.IsNil)
	p, err := s.conn.Collection(schedulerCollection).FindId("pool1").Count()
	c.Assert(err, gocheck.IsNil)
	c.Assert(p, gocheck.Equals, 0)
}

func (s *HandlersSuite) TestListPoolsHandler(c *gocheck.C) {
	pool := Pool{Name: "pool1", Teams: []string{"tsuruteam", "ateam"}}
	err := s.conn.Collection(schedulerCollection).Insert(pool)
	c.Assert(err, gocheck.IsNil)
	defer s.conn.Collection(schedulerCollection).RemoveId(pool.Name)
	poolsExpected := []Pool{pool}
	req, err := http.NewRequest("GET", "/pool", nil)
	c.Assert(err, gocheck.IsNil)
	rec := httptest.NewRecorder()
	err = listPoolHandler(rec, req, nil)
	c.Assert(err, gocheck.IsNil)
	var pools []Pool
	err = json.NewDecoder(rec.Body).Decode(&pools)
	c.Assert(err, gocheck.IsNil)
	c.Assert(pools, gocheck.DeepEquals, poolsExpected)
}

func (s *HandlersSuite) TestAddTeamsToPoolHandler(c *gocheck.C) {
	pool := Pool{Name: "pool1"}
	err := s.conn.Collection(schedulerCollection).Insert(pool)
	c.Assert(err, gocheck.IsNil)
	defer s.conn.Collection(schedulerCollection).RemoveId(pool.Name)
	b := bytes.NewBufferString(`{"pool": "pool1", "teams": ["test"]}`)
	req, err := http.NewRequest("POST", "/pool/team", b)
	c.Assert(err, gocheck.IsNil)
	rec := httptest.NewRecorder()
	err = addTeamToPoolHandler(rec, req, nil)
	c.Assert(err, gocheck.IsNil)
	var p Pool
	err = s.conn.Collection(schedulerCollection).FindId("pool1").One(&p)
	c.Assert(err, gocheck.IsNil)
	c.Assert(p.Teams, gocheck.DeepEquals, []string{"test"})
}

func (s *HandlersSuite) TestRemoveTeamsToPoolHandler(c *gocheck.C) {
	pool := Pool{Name: "pool1", Teams: []string{"test"}}
	err := s.conn.Collection(schedulerCollection).Insert(pool)
	c.Assert(err, gocheck.IsNil)
	defer s.conn.Collection(schedulerCollection).RemoveId(pool.Name)
	b := bytes.NewBufferString(`{"pool": "pool1", "teams": ["test"]}`)
	req, err := http.NewRequest("DELETE", "/pool/team", b)
	c.Assert(err, gocheck.IsNil)
	rec := httptest.NewRecorder()
	err = removeTeamToPoolHandler(rec, req, nil)
	c.Assert(err, gocheck.IsNil)
	var p Pool
	err = s.conn.Collection(schedulerCollection).FindId("pool1").One(&p)
	c.Assert(err, gocheck.IsNil)
	c.Assert(p.Teams, gocheck.DeepEquals, []string{})
}

func (s *HandlersSuite) TestSSHToContainerHandler(c *gocheck.C) {
	sshServer := newMockSSHServer(c, 2e9)
	defer sshServer.Shutdown()
	coll := collection()
	defer coll.Close()
	container := container{
		ID:          "9930c24f1c4x",
		AppName:     "makea",
		Type:        "python",
		Status:      provision.StatusStarted.String(),
		IP:          "127.0.0.4",
		HostPort:    "9025",
		HostAddr:    "localhost",
		SSHHostPort: sshServer.port,
		PrivateKey:  string(fakeServerPrivateKey),
		User:        sshUsername(),
	}
	err := coll.Insert(container)
	c.Assert(err, gocheck.IsNil)
	defer coll.RemoveAll(bson.M{"appname": "makea"})
	tmpDir, err := ioutil.TempDir("", "containerssh")
	defer os.RemoveAll(tmpDir)
	filepath := path.Join(tmpDir, "file.txt")
	file, err := os.Create(filepath)
	c.Assert(err, gocheck.IsNil)
	file.Write([]byte("hello"))
	file.Close()
	buf := safe.NewBuffer([]byte("cat " + filepath + "\nexit\n"))
	recorder := hijacker{conn: &fakeConn{buf}}
	request, err := http.NewRequest("GET", "/?:container_id="+container.ID, nil)
	c.Assert(err, gocheck.IsNil)
	err = sshToContainerHandler(&recorder, request, nil)
	c.Assert(err, gocheck.IsNil)
}

func (s *HandlersSuite) TestSSHToContainerHandlerUnhijackable(c *gocheck.C) {
	coll := collection()
	defer coll.Close()
	container := container{
		ID:       "9930c24f1c4x",
		AppName:  "makea",
		Type:     "python",
		Status:   provision.StatusStarted.String(),
		IP:       "127.0.0.4",
		HostPort: "9025",
		HostAddr: "127.0.0.1",
	}
	err := coll.Insert(container)
	c.Assert(err, gocheck.IsNil)
	defer coll.RemoveAll(bson.M{"appname": "makea"})
	recorder := httptest.NewRecorder()
	request, err := http.NewRequest("GET", "/?:container_id="+container.ID, nil)
	c.Assert(err, gocheck.IsNil)
	err = sshToContainerHandler(recorder, request, nil)
	c.Assert(err, gocheck.NotNil)
	e, ok := err.(*errors.HTTP)
	c.Assert(ok, gocheck.Equals, true)
	c.Assert(e.Code, gocheck.Equals, http.StatusInternalServerError)
	c.Assert(e.Message, gocheck.Equals, "cannot hijack connection")
}

func (s *HandlersSuite) TestSSHToContainerHandlerContainerNotFound(c *gocheck.C) {
	recorder := httptest.NewRecorder()
	request, err := http.NewRequest("GET", "/?:container_id=a12345", nil)
	c.Assert(err, gocheck.IsNil)
	err = sshToContainerHandler(recorder, request, nil)
	c.Assert(err, gocheck.NotNil)
	e, ok := err.(*errors.HTTP)
	c.Assert(ok, gocheck.Equals, true)
	c.Assert(e.Code, gocheck.Equals, http.StatusNotFound)
	c.Assert(e.Message, gocheck.Equals, "not found")
}

func (s *HandlersSuite) TestSSHToContainerFailToHijack(c *gocheck.C) {
	coll := collection()
	defer coll.Close()
	container := container{
		ID:       "9930c24f1c4x",
		AppName:  "makea",
		Type:     "python",
		Status:   provision.StatusStarted.String(),
		IP:       "127.0.0.4",
		HostPort: "9025",
		HostAddr: "127.0.0.1",
	}
	err := coll.Insert(container)
	c.Assert(err, gocheck.IsNil)
	defer coll.RemoveAll(bson.M{"appname": "makea"})
	recorder := hijacker{
		err: fmt.Errorf("are you going to hijack the connection? seriously?"),
	}
	request, err := http.NewRequest("GET", "/?:container_id="+container.ID, nil)
	c.Assert(err, gocheck.IsNil)
	err = sshToContainerHandler(&recorder, request, nil)
	c.Assert(err, gocheck.NotNil)
	e, ok := err.(*errors.HTTP)
	c.Assert(ok, gocheck.Equals, true)
	c.Assert(e.Code, gocheck.Equals, http.StatusInternalServerError)
	c.Assert(e.Message, gocheck.Equals, recorder.err.Error())
}
