// Copyright 2014 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"errors"
	"fmt"
	"github.com/fsouza/go-dockerclient"
	"github.com/tsuru/config"
	"github.com/tsuru/tsuru/action"
	"github.com/tsuru/tsuru/app"
	"github.com/tsuru/tsuru/cmd"
	"github.com/tsuru/tsuru/db"
	"github.com/tsuru/tsuru/db/storage"
	"github.com/tsuru/tsuru/exec"
	"github.com/tsuru/tsuru/log"
	"github.com/tsuru/tsuru/provision"
	"github.com/tsuru/tsuru/router"
	_ "github.com/tsuru/tsuru/router/hipache"
	_ "github.com/tsuru/tsuru/router/testing"
	"io"
	"io/ioutil"
	"net/url"
	"sync"
)

func init() {
	provision.Register("docker", &dockerProvisioner{})
}

var (
	execut exec.Executor
	emutex sync.Mutex
)

func executor() exec.Executor {
	emutex.Lock()
	defer emutex.Unlock()
	if execut == nil {
		execut = exec.OsExecutor{}
	}
	return execut
}

func getRouter() (router.Router, error) {
	r, err := config.GetString("docker:router")
	if err != nil {
		return nil, err
	}
	return router.Get(r)
}

type dockerProvisioner struct{}

// Provision creates a route for the container
func (p *dockerProvisioner) Provision(app provision.App) error {
	r, err := getRouter()
	if err != nil {
		log.Fatalf("Failed to get router: %s", err)
		return err
	}
	err = app.Ready()
	if err != nil {
		return err
	}
	return r.AddBackend(app.GetName())
}

func (p *dockerProvisioner) Restart(app provision.App) error {
	err := p.Stop(app)
	if err != nil {
		return err
	}
	return p.Start(app)
}

func (*dockerProvisioner) Start(app provision.App) error {
	containers, err := listContainersByApp(app.GetName())
	if err != nil {
		return errors.New(fmt.Sprintf("Got error while getting app containers: %s", err))
	}
	var wg sync.WaitGroup
	errCh := make(chan error, len(containers)+1)
	for _, c := range containers {
		wg.Add(1)
		go func(c container) {
			defer wg.Done()
			err := c.start()
			if err != nil {
				errCh <- err
				return
			}
			c.setStatus(provision.StatusStarted.String())
			if info, err := c.networkInfo(); err == nil {
				fixContainer(&c, info)
			}
		}(c)
	}
	wg.Wait()
	close(errCh)
	return <-errCh
}

func (p *dockerProvisioner) Stop(app provision.App) error {
	containers, err := listContainersByApp(app.GetName())
	if err != nil {
		log.Errorf("Got error while getting app containers: %s", err)
		return nil
	}
	var wg sync.WaitGroup
	errCh := make(chan error, len(containers)+1)
	for _, c := range containers {
		wg.Add(1)
		go func(c container) {
			defer wg.Done()
			err := c.stop()
			if err != nil {
				log.Errorf("Failed to stop %q: %s", app.GetName(), err)
				errCh <- err
			}
		}(c)
	}
	wg.Wait()
	close(errCh)
	return <-errCh
}

func (dockerProvisioner) Swap(app1, app2 provision.App) error {
	r, err := getRouter()
	if err != nil {
		return err
	}
	return r.Swap(app1.GetName(), app2.GetName())
}

func (p *dockerProvisioner) GitDeploy(a provision.App, version string, w io.Writer) error {
	imageId, err := gitDeploy(a, version, w)
	if err != nil {
		return err
	}
	return p.deploy(a, imageId, w)
}

func (p *dockerProvisioner) ArchiveDeploy(a provision.App, archiveURL string, w io.Writer) error {
	imageId, err := archiveDeploy(a, archiveURL, w)
	if err != nil {
		return err
	}
	return p.deploy(a, imageId, w)
}

func (p *dockerProvisioner) deploy(a provision.App, imageId string, w io.Writer) error {
	containers, err := listContainersByApp(a.GetName())
	if len(containers) == 0 {
		_, err = runCreateUnitsPipeline(w, a, 1)
	} else {
		_, err = runReplaceUnitsPipeline(w, a, containers)
	}
	return err
}

func (p *dockerProvisioner) Destroy(app provision.App) error {
	containers, err := listContainersByApp(app.GetName())
	if err != nil {
		log.Errorf("Failed to list app containers: %s", err.Error())
		return err
	}
	var containersGroup sync.WaitGroup
	containersGroup.Add(len(containers))
	for _, c := range containers {
		go func(c container) {
			defer containersGroup.Done()
			err := removeContainer(&c)
			if err != nil {
				log.Errorf("Unable to destroy container %s: %s", c.ID, err.Error())
			}
		}(c)
	}
	containersGroup.Wait()
	err = removeImage(assembleImageName(app.GetName()))
	if err != nil {
		log.Errorf("Failed to remove image: %s", err.Error())
	}
	r, err := getRouter()
	if err != nil {
		log.Errorf("Failed to get router: %s", err.Error())
		return err
	}
	err = r.RemoveBackend(app.GetName())
	if err != nil {
		log.Errorf("Failed to remove route backend: %s", err.Error())
		return err
	}
	return nil
}

func (*dockerProvisioner) Addr(app provision.App) (string, error) {
	r, err := getRouter()
	if err != nil {
		log.Errorf("Failed to get router: %s", err)
		return "", err
	}
	addr, err := r.Addr(app.GetName())
	if err != nil {
		log.Errorf("Failed to obtain app %s address: %s", app.GetName(), err)
		return "", err
	}
	return addr, nil
}

func addContainersWithHost(w io.Writer, a provision.App, units int, destinationHost ...string) ([]container, error) {
	if units == 0 {
		return nil, errors.New("Cannot add 0 units")
	}
	if w == nil {
		w = ioutil.Discard
	}
	writer := app.LogWriter{App: a, Writer: w}
	imageId := assembleImageName(a.GetName())
	wg := sync.WaitGroup{}
	createdContainers := make(chan *container, units)
	errors := make(chan error, units)
	var plural string
	if units > 1 {
		plural = "s"
	}
	fmt.Fprintf(&writer, "\n---- Starting %d new unit%s ----\n", units, plural)
	for i := 0; i < units; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := start(a, imageId, &writer, destinationHost...)
			if err != nil {
				errors <- err
				return
			}
			createdContainers <- c
		}()
	}
	wg.Wait()
	close(errors)
	close(createdContainers)
	if err := <-errors; err != nil {
		for c := range createdContainers {
			log.Errorf("Removing container %q due failed add units: %s", c.ID, err.Error())
			errRem := removeContainer(c)
			if errRem != nil {
				log.Errorf("Unable to destroy container %s: %s", c.ID, err.Error())
			}
		}
		return nil, err
	}
	result := make([]container, units)
	i := 0
	for c := range createdContainers {
		result[i] = *c
		i++
		fmt.Fprintf(&writer, " ---> Started unit %d/%d...\n", i, units)
	}
	return result, nil
}

func (*dockerProvisioner) AddUnits(a provision.App, units uint) ([]provision.Unit, error) {
	length, err := getContainerCountForAppName(a.GetName())
	if err != nil {
		return nil, err
	}
	if length < 1 {
		return nil, errors.New("New units can only be added after the first deployment")
	}
	conts, err := runCreateUnitsPipeline(nil, a, int(units))
	if err != nil {
		return nil, err
	}
	result := make([]provision.Unit, len(conts))
	for i, c := range conts {
		result[i] = c.asUnit(a)
	}
	return result, nil
}

func (*dockerProvisioner) RemoveUnits(a provision.App, units uint) error {
	if a == nil {
		return errors.New("remove units: app should not be nil")
	}
	if units < 1 {
		return errors.New("remove units: units must be at least 1")
	}
	containers, err := listContainersByAppOrderedByStatus(a.GetName())
	if err != nil {
		return err
	}
	if units >= uint(len(containers)) {
		return errors.New("remove units: cannot remove all units from app")
	}
	var wg sync.WaitGroup
	for i := 0; i < int(units); i++ {
		wg.Add(1)
		go func(c container) {
			removeContainer(&c)
			wg.Done()
		}(containers[i])
	}
	wg.Wait()
	return nil
}

func (*dockerProvisioner) RemoveUnit(unit provision.Unit) error {
	container, err := getContainer(unit.Name)
	if err != nil {
		return err
	}
	return removeContainer(container)
}

func removeContainer(c *container) error {
	err := c.stop()
	if err != nil {
		log.Errorf("error on stop unit %s - %s", c.ID, err)
	}
	err = c.remove()
	if err != nil {
		log.Errorf("error on remove container %s - %s", c.ID, err)
	}
	return err
}

func (p *dockerProvisioner) SetUnitStatus(unit provision.Unit, status provision.Status) error {
	container, err := getContainer(unit.Name)
	if err != nil {
		return err
	}
	if container.AppName != unit.AppName {
		return errors.New("wrong app name")
	}
	return container.setStatus(status.String())
}

func (*dockerProvisioner) ExecuteCommandOnce(stdout, stderr io.Writer, app provision.App, cmd string, args ...string) error {
	containers, err := listContainersByApp(app.GetName())
	if err != nil {
		return err
	}
	if len(containers) == 0 {
		return provision.ErrEmptyApp
	}
	container := containers[0]
	return container.ssh(stdout, stderr, cmd, args...)
}

func (*dockerProvisioner) ExecuteCommand(stdout, stderr io.Writer, app provision.App, cmd string, args ...string) error {
	containers, err := listContainersByApp(app.GetName())
	if err != nil {
		return err
	}
	if len(containers) == 0 {
		return provision.ErrEmptyApp
	}
	for _, c := range containers {
		err = c.ssh(stdout, stderr, cmd, args...)
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *dockerProvisioner) SetCName(app provision.App, cname string) error {
	r, err := getRouter()
	if err != nil {
		return err
	}
	return r.SetCName(cname, app.GetName())
}

func (p *dockerProvisioner) UnsetCName(app provision.App, cname string) error {
	r, err := getRouter()
	if err != nil {
		return err
	}
	return r.UnsetCName(cname, app.GetName())
}

func (p *dockerProvisioner) Commands() []cmd.Command {
	return []cmd.Command{
		&sshAgentCmd{},
	}
}

func (p *dockerProvisioner) AdminCommands() []cmd.Command {
	return []cmd.Command{
		&moveContainerCmd{},
		&moveContainersCmd{},
		&rebalanceContainersCmd{},
		&addNodeToSchedulerCmd{},
		&removeNodeFromSchedulerCmd{},
		listNodesInTheSchedulerCmd{},
		addPoolToSchedulerCmd{},
		removePoolFromSchedulerCmd{},
		listPoolsInTheSchedulerCmd{},
		addTeamsToPoolCmd{},
		removeTeamsFromPoolCmd{},
		fixContainersCmd{},
		sshToContainerCmd{},
	}
}

func collection() *storage.Collection {
	name, err := config.GetString("docker:collection")
	if err != nil {
		log.Fatal(err.Error())
	}
	conn, err := db.Conn()
	if err != nil {
		log.Errorf("Failed to connect to the database: %s", err)
	}
	return conn.Collection(name)
}

func (p *dockerProvisioner) DeployPipeline() *action.Pipeline {
	actions := []*action.Action{
		&app.ProvisionerDeploy,
		&app.IncrementDeploy,
		&app.BindService,
	}
	pipeline := action.NewPipeline(actions...)
	return pipeline
}

// PlatformAdd build and push a new docker platform to register
func (p *dockerProvisioner) PlatformAdd(name string, args map[string]string, w io.Writer) error {
	if args["dockerfile"] == "" {
		return errors.New("Dockerfile is required.")
	}
	if _, err := url.ParseRequestURI(args["dockerfile"]); err != nil {
		return errors.New("dockerfile parameter should be an url.")
	}
	imageName := assembleImageName(name)
	dockerCluster := dockerCluster()
	buildOptions := docker.BuildImageOptions{
		Name:           imageName,
		NoCache:        true,
		RmTmpContainer: true,
		Remote:         args["dockerfile"],
		InputStream:    nil,
		OutputStream:   w,
	}
	err := dockerCluster.BuildImage(buildOptions)
	if err != nil {
		return err
	}
	return pushImage(imageName)
}

func (p *dockerProvisioner) PlatformUpdate(name string, args map[string]string, w io.Writer) error {
	return p.PlatformAdd(name, args, w)
}

func (p *dockerProvisioner) Units(app provision.App) []provision.Unit {
	containers, err := listContainersByApp(app.GetName())
	if err != nil {
		return nil
	}
	units := []provision.Unit{}
	for _, container := range containers {
		unit := unitFromContainer(container)
		units = append(units, unit)
	}
	return units
}
