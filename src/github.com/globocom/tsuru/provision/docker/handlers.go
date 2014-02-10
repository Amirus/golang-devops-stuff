// Copyright 2014 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"encoding/json"
	"github.com/globocom/tsuru/api"
	"io"
	"io/ioutil"
	"net/http"
)

func init() {
	api.RegisterAdminHandler("/node/add", "POST", api.Handler(addNodeHandler))
	api.RegisterAdminHandler("/node/remove", "DELETE", api.Handler(removeNodeHandler))
}

// AddNodeHandler calls scheduler.Register registering a node into it.
func addNodeHandler(w http.ResponseWriter, r *http.Request) error {
	params, err := unmarshal(r.Body)
	if err != nil {
		return err
	}
	return dockerCluster().Register(params)
}

func removeNodeHandler(w http.ResponseWriter, r *http.Request) error {
	params, err := unmarshal(r.Body)
	if err != nil {
		return err
	}
	return dockerCluster().Unregister(params)
}

func unmarshal(body io.ReadCloser) (map[string]string, error) {
	b, err := ioutil.ReadAll(body)
	if err != nil {
		return nil, err
	}
	params := map[string]string{}
	err = json.Unmarshal(b, &params)
	if err != nil {
		return nil, err
	}
	return params, nil
}
