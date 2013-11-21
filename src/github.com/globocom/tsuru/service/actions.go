// Copyright 2013 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package service

import (
	"errors"
	"github.com/globocom/tsuru/action"
	"github.com/globocom/tsuru/db"
	"labix.org/v2/mgo/bson"
)

// createServiceInstance in an action that calls the service endpoint
// to creates the service instance.
//
// The first argument in the context must be an Service.
// The second argument in the context must be an ServiceInstance.
var createServiceInstance = action.Action{
	Name: "create-service-instance",
	Forward: func(ctx action.FWContext) (action.Result, error) {
		service, ok := ctx.Params[0].(Service)
		if !ok {
			return nil, errors.New("First parameter must be a Service.")
		}
		endpoint, err := service.getClient("production")
		if err != nil {
			return nil, err
		}
		instance, ok := ctx.Params[1].(ServiceInstance)
		if !ok {
			return nil, errors.New("Second parameter must be a ServiceInstance.")
		}
		err = endpoint.Create(&instance)
		if err != nil {
			return nil, err
		}
		return instance, nil
	},
	Backward: func(ctx action.BWContext) {
		service, ok := ctx.Params[0].(Service)
		if !ok {
			return
		}
		endpoint, err := service.getClient("production")
		if err != nil {
			return
		}
		instance, ok := ctx.Params[1].(ServiceInstance)
		if !ok {
			return
		}
		endpoint.Destroy(&instance)
	},
	MinParams: 2,
}

// insertServiceInstance is an action that inserts an instance in the database.
//
// The first argument in the context must be a Service Instance.
var insertServiceInstance = action.Action{
	Name: "insert-service-instance",
	Forward: func(ctx action.FWContext) (action.Result, error) {
		instance, ok := ctx.Params[1].(ServiceInstance)
		if !ok {
			return nil, errors.New("Second parameter must be a ServiceInstance.")
		}
		conn, err := db.Conn()
		if err != nil {
			return nil, err
		}
		defer conn.Close()
		err = conn.ServiceInstances().Insert(&instance)
		if err != nil {
			return nil, err
		}
		return nil, nil
	},
	Backward: func(ctx action.BWContext) {
		instance, ok := ctx.Params[1].(ServiceInstance)
		if !ok {
			return
		}
		conn, err := db.Conn()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.ServiceInstances().Remove(bson.M{"name": instance.Name})
	},
	MinParams: 2,
}
