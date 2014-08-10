// Copyright 2014 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"errors"
	"fmt"
	"github.com/fsouza/go-dockerclient"
	"github.com/tsuru/tsuru/action"
	"github.com/tsuru/tsuru/log"
	"github.com/tsuru/tsuru/provision"
	"github.com/tsuru/tsuru/router"
	"gopkg.in/mgo.v2/bson"
	"io"
	"io/ioutil"
	"sync"
)

type runContainerActionsArgs struct {
	app              provision.App
	imageID          string
	commands         []string
	destinationHosts []string
	privateKey       []byte
	writer           io.Writer
}

type changeUnitsPipelineArgs struct {
	app        provision.App
	writer     io.Writer
	toRemove   []container
	unitsToAdd int
	toHost     string
}

var insertEmptyContainerInDB = action.Action{
	Name: "insert-empty-container",
	Forward: func(ctx action.FWContext) (action.Result, error) {
		args := ctx.Params[0].(runContainerActionsArgs)
		contName := containerName()
		cont := container{
			AppName:    args.app.GetName(),
			Type:       args.app.GetPlatform(),
			Name:       contName,
			Status:     "created",
			Image:      args.imageID,
			PrivateKey: string(args.privateKey),
		}
		coll := collection()
		defer coll.Close()
		if err := coll.Insert(cont); err != nil {
			log.Errorf("error on inserting container into database %s - %s", cont.Name, err)
			return nil, err
		}
		return cont, nil
	},
	Backward: func(ctx action.BWContext) {
		c := ctx.FWResult.(container)
		coll := collection()
		defer coll.Close()
		coll.Remove(bson.M{"name": c.Name})
	},
}

var updateContainerInDB = action.Action{
	Name: "update-database-container",
	Forward: func(ctx action.FWContext) (action.Result, error) {
		coll := collection()
		defer coll.Close()
		cont := ctx.Previous.(container)
		err := coll.Update(bson.M{"name": cont.Name}, cont)
		if err != nil {
			log.Errorf("error on updating container into database %s - %s", cont.ID, err)
			return nil, err
		}
		return cont, nil
	},
	Backward: func(ctx action.BWContext) {
	},
}

var createContainer = action.Action{
	Name: "create-container",
	Forward: func(ctx action.FWContext) (action.Result, error) {
		cont := ctx.Previous.(container)
		args := ctx.Params[0].(runContainerActionsArgs)
		log.Debugf("create container for app %s, based on image %s, with cmds %s", args.app.GetName(), args.imageID, args.commands)
		err := cont.create(args.app, args.imageID, args.commands, args.destinationHosts...)
		if err != nil {
			log.Errorf("error on create container for app %s - %s", args.app.GetName(), err)
			return nil, err
		}
		return cont, nil
	},
	Backward: func(ctx action.BWContext) {
		c := ctx.FWResult.(container)
		err := dockerCluster().RemoveContainer(docker.RemoveContainerOptions{ID: c.ID})
		if err != nil {
			log.Errorf("Failed to remove the container %q: %s", c.ID, err)
		}
	},
}

var setNetworkInfo = action.Action{
	Name: "set-network-info",
	Forward: func(ctx action.FWContext) (action.Result, error) {
		c := ctx.Previous.(container)
		info, err := c.networkInfo()
		if err != nil {
			return nil, err
		}
		c.IP = info.IP
		c.HostPort = info.HTTPHostPort
		c.SSHHostPort = info.SSHHostPort
		return c, nil
	},
}

var startContainer = action.Action{
	Name: "start-container",
	Forward: func(ctx action.FWContext) (action.Result, error) {
		c := ctx.Previous.(container)
		log.Debugf("starting container %s", c.ID)
		err := c.start()
		if err != nil {
			log.Errorf("error on start container %s - %s", c.ID, err)
			return nil, err
		}
		return c, nil
	},
	Backward: func(ctx action.BWContext) {
		c := ctx.FWResult.(container)
		err := dockerCluster().StopContainer(c.ID, 10)
		if err != nil {
			log.Errorf("Failed to stop the container %q: %s", c.ID, err)
		}
	},
}

var provisionAddUnitsToHost = action.Action{
	Name: "provision-add-units-to-host",
	Forward: func(ctx action.FWContext) (action.Result, error) {
		args := ctx.Params[0].(changeUnitsPipelineArgs)
		var destinationHosts []string
		if args.toHost != "" {
			destinationHosts = []string{args.toHost}
		}
		containers, err := addContainersWithHost(args.writer, args.app, args.unitsToAdd, destinationHosts...)
		if err != nil {
			return nil, err
		}
		return containers, nil
	},
	Backward: func(ctx action.BWContext) {
		containers := ctx.FWResult.([]container)
		for _, cont := range containers {
			err := removeContainer(&cont)
			if err != nil {
				log.Errorf("Error removing added container %s: %s", cont.ID, err.Error())
			}
		}
	},
	MinParams: 1,
}

var addNewRoutes = action.Action{
	Name: "add-new-routes",
	Forward: func(ctx action.FWContext) (action.Result, error) {
		newContainers := ctx.Previous.([]container)
		r, err := getRouter()
		if err != nil {
			return nil, err
		}
		addedContainers := make([]container, 0, len(newContainers))
		for _, cont := range newContainers {
			err = r.AddRoute(cont.AppName, cont.getAddress())
			if err != nil {
				for _, toRemoveCont := range addedContainers {
					r.RemoveRoute(toRemoveCont.AppName, toRemoveCont.getAddress())
				}
				return nil, err
			}
			addedContainers = append(addedContainers, cont)
		}
		return newContainers, nil
	},
	Backward: func(ctx action.BWContext) {
		newContainers := ctx.FWResult.([]container)
		r, err := getRouter()
		if err != nil {
			log.Errorf("[add-new-routes:Backward] Error geting router: %s", err.Error())
		}
		for _, cont := range newContainers {
			err = r.RemoveRoute(cont.AppName, cont.getAddress())
			if err != nil {
				log.Errorf("[add-new-routes:Backward] Error removing route for %s: %s", cont.ID, err.Error())
			}
		}
	},
}

var removeOldRoutes = action.Action{
	Name: "remove-old-routes",
	Forward: func(ctx action.FWContext) (action.Result, error) {
		args := ctx.Params[0].(changeUnitsPipelineArgs)
		r, err := getRouter()
		if err != nil {
			return nil, err
		}
		removedConts := make([]container, 0, len(args.toRemove))
		for _, cont := range args.toRemove {
			err = r.RemoveRoute(cont.AppName, cont.getAddress())
			if err != router.ErrRouteNotFound && err != nil {
				for _, toAddCont := range removedConts {
					r.AddRoute(toAddCont.AppName, toAddCont.getAddress())
				}
				return nil, err
			}
			removedConts = append(removedConts, cont)
		}
		return ctx.Previous, nil
	},
	Backward: func(ctx action.BWContext) {
		args := ctx.Params[0].(changeUnitsPipelineArgs)
		r, err := getRouter()
		if err != nil {
			log.Errorf("[add-new-routes:Backward] Error geting router: %s", err.Error())
		}
		for _, cont := range args.toRemove {
			err = r.AddRoute(cont.AppName, cont.getAddress())
			if err != nil {
				log.Errorf("[remove-old-routes:Backward] Error adding back route for %s: %s", cont.ID, err.Error())
			}
		}
	},
	MinParams: 1,
}

var provisionRemoveOldUnits = action.Action{
	Name: "provision-remove-old-units",
	Forward: func(ctx action.FWContext) (action.Result, error) {
		var wg sync.WaitGroup
		args := ctx.Params[0].(changeUnitsPipelineArgs)
		removedContainers := make(chan *container, len(args.toRemove))
		writer := args.writer
		if writer == nil {
			writer = ioutil.Discard
		}
		total := len(args.toRemove)
		var plural string
		if total > 1 {
			plural = "s"
		}
		fmt.Fprintf(writer, "\n---- Removing %d old unit%s ----\n", total, plural)
		for _, cont := range args.toRemove {
			wg.Add(1)
			go func(cont container) {
				defer wg.Done()
				err := removeContainer(&cont)
				if err != nil {
					log.Errorf("Ignored error trying to remove old container %q: %s", cont.ID, err.Error())
				}
				removedContainers <- &cont
			}(cont)
		}
		go func() {
			wg.Wait()
			close(removedContainers)
		}()
		counter := 0
		for _ = range removedContainers {
			counter++
			fmt.Fprintf(writer, " ---> Removed old unit %d/%d\n", counter, total)
		}
		return ctx.Previous, nil
	},
	Backward: func(ctx action.BWContext) {
	},
	MinParams: 1,
}

var followLogsAndCommit = action.Action{
	Name: "follow-logs-and-commit",
	Forward: func(ctx action.FWContext) (action.Result, error) {
		c, ok := ctx.Previous.(container)
		if !ok {
			return nil, errors.New("Previous result must be a container.")
		}
		args := ctx.Params[0].(runContainerActionsArgs)
		err := c.logs(args.writer)
		if err != nil {
			log.Errorf("error on get logs for container %s - %s", c.ID, err)
			return nil, err
		}
		status, err := dockerCluster().WaitContainer(c.ID)
		if err != nil {
			log.Errorf("Process failed for container %q: %s", c.ID, err)
			return nil, err
		}
		if status != 0 {
			return nil, fmt.Errorf("Exit status %d", status)
		}
		imageId, err := c.commit()
		if err != nil {
			log.Errorf("error on commit container %s - %s", c.ID, err)
			return nil, err
		}
		c.remove()
		return imageId, nil
	},
	Backward: func(ctx action.BWContext) {
	},
	MinParams: 1,
}
