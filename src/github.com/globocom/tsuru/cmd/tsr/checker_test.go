// Copyright 2014 Globo.com. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"github.com/tsuru/config"
	"launchpad.net/gocheck"
)

type CheckerSuite struct{}

var _ = gocheck.Suite(&CheckerSuite{})

var configFixture = `
listen: 0.0.0.0:8080
host: http://127.0.0.1:8080
debug: true
admin-team: admin

database:
  url: 127.0.0.1:3435
  name: tsuru

git:
  unit-repo: /home/application/current
  api-server: http://127.0.0.1:8000
  rw-host: 127.0.0.1
  ro-host: 127.0.0.1

auth:
  user-registration: true
  scheme: native

provisioner: docker
hipache:
  domain: tsuru-sample.com
queue: redis
redis-queue:
  host: localhost
  port: 6379
docker:
  collection: docker_containers
  repository-namespace: tsuru
  router: hipache
  deploy-cmd: /var/lib/tsuru/deploy
  segregate: true
  cluster:
    storage: redis
    redis-server: 127.0.0.1:6379
    redis-prefix: docker-cluster
  run-cmd:
    bin: /var/lib/tsuru/start
    port: 8888
  ssh:
    add-key-cmd: /var/lib/tsuru/add-key
    user: ubuntu
`

func (s *CheckerSuite) SetUpTest(c *gocheck.C) {
	err := config.ReadConfigBytes([]byte(configFixture))
	c.Assert(err, gocheck.IsNil)
}

func (s *CheckerSuite) TestCheckDockerJustCheckIfProvisionerIsDocker(c *gocheck.C) {
	config.Set("provisioner", "test")
	err := CheckProvisioner()
	c.Assert(err, gocheck.IsNil)
}

func (s *CheckerSuite) TestCheckDockerIsNotConfigured(c *gocheck.C) {
	config.Unset("docker")
	err := CheckDocker()
	c.Assert(err, gocheck.NotNil)
}

func (s *CheckerSuite) TestCheckDockerBasicConfig(c *gocheck.C) {
	err := CheckDockerBasicConfig()
	c.Assert(err, gocheck.IsNil)
}

func (s *CheckerSuite) TestCheckDockerBasicConfigError(c *gocheck.C) {
	config.Unset("docker:collection")
	err := CheckDockerBasicConfig()
	c.Assert(err, gocheck.NotNil)
}

func (s *CheckerSuite) TestCheckSchedulerConfig(c *gocheck.C) {
	err := CheckScheduler()
	c.Assert(err, gocheck.IsNil)
}

func (s *CheckerSuite) TestCheckSchedulerSegregateWithServersConfig(c *gocheck.C) {
	config.Set("docker:servers", []string{"server1", "server2"})
	err := CheckScheduler()
	c.Assert(err, gocheck.NotNil)
}

func (s *CheckerSuite) TestCheckSchedulerRoundRobinWithoutServersConfig(c *gocheck.C) {
	config.Set("docker:segregate", false)
	err := CheckScheduler()
	c.Assert(err, gocheck.NotNil)
}

func (s *CheckerSuite) TestCheckClusterWithRedis(c *gocheck.C) {
	err := checkCluster()
	c.Assert(err, gocheck.IsNil)
	toFail := []string{"docker:cluster:storage", "docker:cluster:redis-server", "docker:cluster:redis-prefix"}
	for _, prop := range toFail {
		val, _ := config.Get(prop)
		config.Unset(prop)
		err := checkCluster()
		c.Assert(err, gocheck.NotNil)
		config.Set(prop, val)
	}
}

func (s *CheckerSuite) TestCheckClusterWithMongo(c *gocheck.C) {
	config.Set("docker:cluster:storage", "mongodb")
	err := checkCluster()
	c.Assert(err, gocheck.NotNil)
	config.Set("docker:cluster:mongo-url", "xxx")
	err = checkCluster()
	c.Assert(err, gocheck.NotNil)
	config.Set("docker:cluster:mongo-database", "xxx")
	err = checkCluster()
	c.Assert(err, gocheck.IsNil)
}

func (s *CheckerSuite) TestCheckRouter(c *gocheck.C) {
	err := CheckRouter()
	c.Assert(err, gocheck.IsNil)
}

func (s *CheckerSuite) TestCheckRouterHipacheShouldHaveHipachConf(c *gocheck.C) {
	config.Unset("hipache")
	err := CheckRouter()
	c.Assert(err, gocheck.NotNil)
}

func (s *CheckerSuite) TestCheckBeanstalkdRedisQueue(c *gocheck.C) {
	err := CheckBeanstalkd()
	c.Assert(err, gocheck.IsNil)
}

func (s *CheckerSuite) TestCheckBeanstalkdNoQueueConfigured(c *gocheck.C) {
	old, _ := config.Get("queue")
	defer config.Set("queue", old)
	config.Unset("queue")
	err := CheckBeanstalkd()
	c.Assert(err, gocheck.IsNil)
}

func (s *CheckerSuite) TestCheckBeanstalkdDefinedInQueue(c *gocheck.C) {
	old, _ := config.Get("queue")
	defer config.Set("queue", old)
	config.Set("queue", "beanstalkd")
	err := CheckBeanstalkd()
	c.Assert(err.Error(), gocheck.Equals, "beanstalkd is no longer supported, please use redis instead")
}

func (w *CheckerSuite) TestCheckBeanstalkdQueueServerDefined(c *gocheck.C) {
	config.Set("queue-server", "127.0.0.1:11300")
	defer config.Unset("queue-server")
	err := CheckBeanstalkd()
	c.Assert(err.Error(), gocheck.Equals, `beanstalkd is no longer supported, please remove the "queue-server" setting from your config file`)
}
