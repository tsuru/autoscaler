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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/xanzy/go-cloudstack/v2/cloudstack"
)

func Test_newProjectCache(t *testing.T) {
	tests := []struct {
		useProjects bool
		maxAge      time.Duration
		mockErr     error
		expectedErr string
	}{
		{expectedErr: `max projects age cannot be <= 0`},
		{maxAge: time.Minute},
		{useProjects: true, maxAge: time.Minute},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			cli := fakeClient{}
			pc, err := newProjectCache(&cli, tt.useProjects, tt.maxAge)
			if tt.expectedErr != "" {
				require.Error(t, err)
				assert.Regexp(t, tt.expectedErr, err.Error())
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.maxAge, pc.maxAge)
				assert.Equal(t, tt.useProjects, pc.useProjects)
			}
		})
	}
}

func Test_projectCache_refresh(t *testing.T) {
	t.Run("no projects", func(t *testing.T) {
		pc := projectCache{}
		err := pc.refresh()
		assert.NoError(t, err)
		assert.Equal(t, projectCache{}, pc)
	})

	tests := []struct {
		mockErr          error
		expectedProjects []*cloudstack.Project
	}{
		{expectedProjects: []*cloudstack.Project{
			{Id: "pj1"},
		}},
		{mockErr: errors.New("list err")},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			cli := fakeClient{}
			cli.On("ListProjects", mock.Anything).Return(&cloudstack.ListProjectsResponse{
				Count: 1,
				Projects: []*cloudstack.Project{
					{Id: "pj1"},
				},
			}, tt.mockErr)
			pc := projectCache{client: &cli, maxAge: 300 * time.Millisecond, useProjects: true}

			err := pc.refresh()
			if tt.mockErr != nil {
				require.Error(t, err)
				assert.Equal(t, tt.mockErr, err)
				cli.AssertNumberOfCalls(t, "ListProjects", 1)

				err = pc.refresh()
				require.Error(t, err)
				assert.Equal(t, tt.mockErr, err)
				cli.AssertNumberOfCalls(t, "ListProjects", 2)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedProjects, pc.projects)
			assert.False(t, pc.lastUpdated.IsZero())
			cli.AssertNumberOfCalls(t, "ListProjects", 1)

			lastUpdated := pc.lastUpdated

			err = pc.refresh()
			require.NoError(t, err)
			assert.Equal(t, tt.expectedProjects, pc.projects)
			cli.AssertNumberOfCalls(t, "ListProjects", 1)
			assert.Equal(t, lastUpdated, pc.lastUpdated)

			time.Sleep(310 * time.Millisecond)

			err = pc.refresh()
			require.NoError(t, err)
			assert.Equal(t, tt.expectedProjects, pc.projects)
			cli.AssertNumberOfCalls(t, "ListProjects", 2)
			assert.NotEqual(t, lastUpdated, pc.lastUpdated)
		})
	}
}

func Test_projectCache_forEach(t *testing.T) {
	var projIDs []string
	eachFn := func(projID string) error {
		projIDs = append(projIDs, projID)
		return nil
	}

	t.Run("no projects", func(t *testing.T) {
		pc := projectCache{}
		err := pc.forEach(eachFn)
		assert.NoError(t, err)
		assert.Equal(t, []string{""}, projIDs)
	})

	tests := []struct {
		eachFn             func(projID string) error
		mockErr            error
		expectedProjectIDs []string
		expectedErr        string
	}{

		{expectedProjectIDs: []string{"", "pj1"}, eachFn: eachFn},
		{mockErr: errors.New("list err"), expectedErr: `list err`},
		{eachFn: func(string) error { return errors.New("myerr") }, expectedErr: `myerr`},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			projIDs = nil
			cli := fakeClient{}
			cli.On("ListProjects", mock.Anything).Return(&cloudstack.ListProjectsResponse{
				Count: 1,
				Projects: []*cloudstack.Project{
					{Id: "pj1"},
				},
			}, tt.mockErr)
			pc := projectCache{client: &cli, maxAge: 300 * time.Millisecond, useProjects: true}

			err := pc.forEach(tt.eachFn)

			if tt.expectedErr != "" {
				require.Error(t, err)
				assert.Regexp(t, tt.expectedErr, err.Error())
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedProjectIDs, projIDs)
			assert.False(t, pc.lastUpdated.IsZero())
			cli.AssertNumberOfCalls(t, "ListProjects", 1)

			lastUpdated := pc.lastUpdated

			projIDs = nil
			err = pc.forEach(tt.eachFn)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedProjectIDs, projIDs)
			cli.AssertNumberOfCalls(t, "ListProjects", 1)
			assert.Equal(t, lastUpdated, pc.lastUpdated)
		})
	}
}
