// Copyright 2014 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ec2

import (
	"github.com/tsuru/config"
	"github.com/tsuru/tsuru/iaas"
	"launchpad.net/goamz/aws"
	"launchpad.net/goamz/ec2"
	"launchpad.net/goamz/ec2/ec2test"
	"launchpad.net/gocheck"
	"testing"
)

func Test(t *testing.T) { gocheck.TestingT(t) }

type S struct {
	srv    *ec2test.Server
	region aws.Region
}

var _ = gocheck.Suite(&S{})

func (s *S) SetUpSuite(c *gocheck.C) {
	var err error
	s.srv, err = ec2test.NewServer()
	c.Assert(err, gocheck.IsNil)
	s.region = aws.Region{
		Name:        "myregion",
		EC2Endpoint: s.srv.URL(),
	}
	aws.Regions["myregion"] = s.region
	config.Set("iaas:ec2:key-id", "mykey")
	config.Set("iaas:ec2:secret-key", "mysecret")
}

func (s *S) TearDownSuite(c *gocheck.C) {
	s.srv.Quit()
}

func (s *S) TestCreateEC2Handler(c *gocheck.C) {
	handler, err := createEC2Handler(aws.APNortheast)
	c.Assert(err, gocheck.IsNil)
	c.Assert(handler.Region, gocheck.DeepEquals, aws.APNortheast)
	c.Assert(handler.Auth.AccessKey, gocheck.Equals, "mykey")
	c.Assert(handler.Auth.SecretKey, gocheck.Equals, "mysecret")
}

func (s *S) TestCreateMachine(c *gocheck.C) {
	params := map[string]string{
		"region": "myregion",
		"image":  "ami-xxxxxx",
		"type":   "m1.micro",
	}
	iaas := &EC2IaaS{}
	m, err := iaas.CreateMachine(params)
	m.CreationParams = map[string]string{"region": "myregion"}
	defer iaas.DeleteMachine(m)
	c.Assert(err, gocheck.IsNil)
	c.Assert(m.Id, gocheck.Matches, `i-\d`)
	c.Assert(m.Address, gocheck.Matches, `i-\d.testing.invalid`)
	c.Assert(m.Status, gocheck.Equals, "pending")
}

func (s *S) TestWaitForDnsName(c *gocheck.C) {
	handler, err := createEC2Handler(s.region)
	c.Assert(err, gocheck.IsNil)
	options := ec2.RunInstances{
		ImageId:      "ami-xxx",
		InstanceType: "m1.small",
		MinCount:     1,
		MaxCount:     1,
	}
	resp, err := handler.RunInstances(&options)
	c.Assert(err, gocheck.IsNil)
	instance := &resp.Instances[0]
	instance.DNSName = ""
	instance, err = waitForDnsName(handler, instance)
	c.Assert(err, gocheck.IsNil)
	c.Assert(instance.DNSName, gocheck.Matches, `i-\d.testing.invalid`)
}

func (s *S) TestCreateMachineValidations(c *gocheck.C) {
	iaas := &EC2IaaS{}
	_, err := iaas.CreateMachine(map[string]string{
		"region": "invalid-region",
	})
	c.Assert(err, gocheck.ErrorMatches, `region "invalid-region" not found`)
	_, err = iaas.CreateMachine(map[string]string{
		"region": "myregion",
	})
	c.Assert(err, gocheck.ErrorMatches, "image param required")
	_, err = iaas.CreateMachine(map[string]string{
		"region": "myregion",
		"image":  "ami-xxxxx",
	})
	c.Assert(err, gocheck.ErrorMatches, "type param required")
}

func (s *S) TestDeleteMachine(c *gocheck.C) {
	m := iaas.Machine{
		Id:             "i-0",
		CreationParams: map[string]string{"region": "myregion"},
	}
	iaas := &EC2IaaS{}
	err := iaas.DeleteMachine(&m)
	c.Assert(err, gocheck.IsNil)
}

func (s *S) TestDeleteMachineValidations(c *gocheck.C) {
	m := &iaas.Machine{
		Id:             "i-0",
		CreationParams: map[string]string{},
	}
	ec2Iaas := &EC2IaaS{}
	err := ec2Iaas.DeleteMachine(m)
	c.Assert(err, gocheck.ErrorMatches, "region creation param required")
	m = &iaas.Machine{
		Id:             "i-0",
		CreationParams: map[string]string{"region": "invalid"},
	}
	err = ec2Iaas.DeleteMachine(m)
	c.Assert(err, gocheck.ErrorMatches, `region "invalid" not found`)
}
