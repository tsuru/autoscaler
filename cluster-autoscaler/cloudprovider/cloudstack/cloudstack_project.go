/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cloudstack

import (
	"errors"
	"time"

	"github.com/xanzy/go-cloudstack/v2/cloudstack"
)

// projectCache is the structure responsible for keeping an in-memory cache of
// existing projects and lazily updating the cache on some interval. It exists
// because a listProjects call to cloudstack is potentially really slow (think
// minutes) and doing it on every refresh would not be feasible.
type projectCache struct {
	projects    []*cloudstack.Project
	client      projectCloudstackClient
	maxAge      time.Duration
	lastUpdated time.Time
	useProjects bool
}

type projectCloudstackClient interface {
	ListProjects(*cloudstack.ListProjectsParams) (*cloudstack.ListProjectsResponse, error)
}

func newProjectCache(client projectCloudstackClient, useProjects bool, maxAge time.Duration) (*projectCache, error) {
	if maxAge <= 0 {
		return nil, errors.New("max projects age cannot be <= 0")
	}
	pc := projectCache{
		client:      client,
		maxAge:      maxAge,
		useProjects: useProjects,
	}
	return &pc, nil
}

func (pc *projectCache) refresh() error {
	if !pc.useProjects || time.Since(pc.lastUpdated) <= pc.maxAge {
		return nil
	}
	var params cloudstack.ListProjectsParams
	projects, err := pc.client.ListProjects(&params)
	if err != nil {
		return err
	}
	pc.lastUpdated = time.Now()
	pc.projects = projects.Projects
	return nil
}

func (pc *projectCache) forEach(fn func(projectID string) error) error {
	err := pc.refresh()
	if err != nil {
		return err
	}
	for _, project := range append([]*cloudstack.Project{{}}, pc.projects...) {
		err := fn(project.Id)
		if err != nil {
			return err
		}
	}
	return nil
}
