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
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/xanzy/go-cloudstack/v2/cloudstack"
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
)

func Test_newManager(t *testing.T) {
	cli := &fakeClient{}
	newCloudstackClient = func(cfg csConfig) cloudstackClient {
		return cli
	}

	tests := []struct {
		configData    string
		do            cloudprovider.NodeGroupDiscoveryOptions
		expectedError string
		expectedMgr   *cloudstackManager
		envs          map[string]string
	}{
		{expectedError: `unexpected.*`},
		{configData: `{}`, expectedError: `api key is required`},
		{configData: `{
			"api_key": "k1"
		}`, expectedError: `api secret is required`},
		{configData: `{
			"api_key": "k1",
			"api_secret": "s1"
		}`, expectedError: `URL is required`},
		{configData: `{
			"api_key": "k1",
			"api_secret": "s1",
			"url": "u1"
		}`, expectedError: `auto discovery configuration is required`},
		{configData: `{
			"api_key": "k1",
			"api_secret": "s1",
			"url": "u1"
		}`, do: cloudprovider.NodeGroupDiscoveryOptions{
			NodeGroupAutoDiscoverySpecs: []string{"invalid:a=b"},
		}, expectedError: `unsupported discoverer specified: invalid`},
		{configData: `{
			"api_key": "k1",
			"url": "u1"
		}`, envs: map[string]string{
			"CLOUDSTACK_API_SECRET": "mysecret1",
		}, do: cloudprovider.NodeGroupDiscoveryOptions{
			NodeGroupAutoDiscoverySpecs: []string{"label:a=b"},
		}, expectedMgr: &cloudstackManager{
			config: csConfig{
				APIKey:    "k1",
				APISecret: "mysecret1",
				URL:       "u1",
			},
			labelConfig: []cloudprovider.LabelAutoDiscoveryConfig{
				{Selector: map[string]string{"a": "b"}},
			},
			client: cli,
			projects: &projectCache{
				client: cli,
				maxAge: defaultProjectRefreshInterval,
			},
			scaler: &csScaler{
				client: cli,
			},
		}},
		{configData: `{
			"api_key": "k1",
			"api_secret": "s1",
			"url": "u1"
		}`, do: cloudprovider.NodeGroupDiscoveryOptions{
			NodeGroupAutoDiscoverySpecs: []string{"label:a=b"},
		}, expectedMgr: &cloudstackManager{
			config: csConfig{
				APIKey:    "k1",
				APISecret: "s1",
				URL:       "u1",
			},
			labelConfig: []cloudprovider.LabelAutoDiscoveryConfig{
				{Selector: map[string]string{"a": "b"}},
			},
			client: cli,
			projects: &projectCache{
				client: cli,
				maxAge: defaultProjectRefreshInterval,
			},
			scaler: &csScaler{
				client: cli,
			},
		}},
		{configData: `{
			"api_key": "k1",
			"api_secret": "s1",
			"url": "u1",
			"insecure": true,
			"use_projects": true,
			"expunge_vms": true,
			"project_refresh_interval": "24h"
		}`, do: cloudprovider.NodeGroupDiscoveryOptions{
			NodeGroupAutoDiscoverySpecs: []string{"label:a=b"},
		}, expectedMgr: &cloudstackManager{
			config: csConfig{
				APIKey:                 "k1",
				APISecret:              "s1",
				URL:                    "u1",
				InsecureSkipVerify:     true,
				UseProjects:            true,
				ExpungeVMs:             true,
				ProjectRefreshInterval: "24h",
			},
			labelConfig: []cloudprovider.LabelAutoDiscoveryConfig{
				{Selector: map[string]string{"a": "b"}},
			},
			client: cli,
			projects: &projectCache{
				client:      cli,
				maxAge:      24 * time.Hour,
				useProjects: true,
			},
			scaler: &csScaler{
				client:  cli,
				expunge: true,
			},
		}},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			for k, v := range tt.envs {
				os.Setenv(k, v)
				defer os.Unsetenv(k)
			}
			mgr, err := newManager(strings.NewReader(tt.configData), tt.do)
			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Regexp(t, tt.expectedError, err.Error())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expectedMgr, mgr)
		})
	}
}

func Test_cloudstackManager_Refresh(t *testing.T) {
	cli := &fakeClient{}

	cli.On("ListProjects", mock.Anything).Return(&cloudstack.ListProjectsResponse{
		Count: 1,
		Projects: []*cloudstack.Project{
			{Id: "pj1"},
		},
	}, nil)

	params := cloudstack.ListAutoScaleVmProfilesParams{}
	cli.On("ListAutoScaleVmProfiles", &params).Return(&cloudstack.ListAutoScaleVmProfilesResponse{
		Count: 0,
	}, nil)

	params2 := cloudstack.ListAutoScaleVmProfilesParams{}
	params2.SetProjectid("pj1")
	cli.On("ListAutoScaleVmProfiles", &params2).Return(&cloudstack.ListAutoScaleVmProfilesResponse{
		Count: 1,
		AutoScaleVmProfiles: []*cloudstack.AutoScaleVmProfile{
			{Id: "asp1", Serviceofferingid: "offering1", Zoneid: "zone1"},
		},
	}, nil)

	cli.On("ListResourceDetails", mock.Anything).Return(&cloudstack.ListResourceDetailsResponse{
		Count: 1,
		ResourceDetails: []*cloudstack.ResourceDetail{
			{Key: "a", Value: "b"},
			{Key: "nodeGroupName", Value: "ng1"},
			{Key: "minNodes", Value: "0"},
			{Key: "maxNodes", Value: "10"},
			{Key: "targetNodes", Value: "1"},
		},
	}, nil)

	var listParams cloudstack.ListVirtualMachinesParams
	listParams.SetTags(map[string]string{"nodeGroupName": "ng1"})
	cli.On("ListVirtualMachines", &listParams).Return(&cloudstack.ListVirtualMachinesResponse{
		Count: 1,
		VirtualMachines: []*cloudstack.VirtualMachine{
			{Id: "vm1"},
		},
	}, nil)
	cli.On("GetServiceOfferingByID", "offering1").Return(&cloudstack.ServiceOffering{
		Name: "offering1name",
	}, 1, nil)
	cli.On("GetZoneByID", "zone1").Return(&cloudstack.Zone{
		Name: "zone1name",
	}, 1, nil)

	t.Run("ok", func(t *testing.T) {
		manager := &cloudstackManager{
			client: cli,
			projects: &projectCache{
				client:      cli,
				maxAge:      time.Hour,
				useProjects: true,
			},
			scaler: &csScaler{
				client: cli,
			},
			labelConfig: []cloudprovider.LabelAutoDiscoveryConfig{
				{Selector: map[string]string{"a": "b"}},
			},
		}
		err := manager.Refresh()
		require.NoError(t, err)
		cli.AssertExpectations(t)
		assert.Equal(t, []csNodeGroup{
			{
				manager: manager,
				vmProfile: vmProfile{
					asp: cloudstack.AutoScaleVmProfile{
						Id: "asp1", Serviceofferingid: "offering1", Zoneid: "zone1",
					},
					aspMetadata: map[string]string{
						"a":             "b",
						"nodeGroupName": "ng1",
						"minNodes":      "0",
						"maxNodes":      "10",
						"targetNodes":   "1",
					},
					offering: cloudstack.ServiceOffering{
						Name: "offering1name",
					},
					zone: cloudstack.Zone{
						Name: "zone1name",
					},
				},
				vms: []*cloudstack.VirtualMachine{
					{Id: "vm1"},
				},
			},
		}, manager.nodeGroups)
	})

	t.Run("conflict error", func(t *testing.T) {
		cli := &fakeClient{}

		params := cloudstack.ListAutoScaleVmProfilesParams{}
		cli.On("ListAutoScaleVmProfiles", &params).Return(&cloudstack.ListAutoScaleVmProfilesResponse{
			Count: 2,
			AutoScaleVmProfiles: []*cloudstack.AutoScaleVmProfile{
				{Id: "asp1", Serviceofferingid: "offering1", Zoneid: "zone1"},
				{Id: "asp2", Serviceofferingid: "offering1", Zoneid: "zone1"},
			},
		}, nil).Once()

		cli.On("ListResourceDetails", mock.Anything).Return(&cloudstack.ListResourceDetailsResponse{
			Count: 1,
			ResourceDetails: []*cloudstack.ResourceDetail{
				{Key: "a", Value: "b"},
				{Key: "nodeGroupName", Value: "ng1"},
				{Key: "minNodes", Value: "0"},
				{Key: "maxNodes", Value: "10"},
				{Key: "targetNodes", Value: "1"},
			},
		}, nil).Twice()

		cli.On("GetServiceOfferingByID", "offering1").Return(&cloudstack.ServiceOffering{
			Name: "offering1name",
		}, 1, nil).Once()

		cli.On("GetZoneByID", "zone1").Return(&cloudstack.Zone{
			Name: "zone1name",
		}, 1, nil).Once()

		var listParams cloudstack.ListVirtualMachinesParams
		listParams.SetTags(map[string]string{"nodeGroupName": "ng1"})
		cli.On("ListVirtualMachines", &listParams).Return(&cloudstack.ListVirtualMachinesResponse{
			Count: 1,
			VirtualMachines: []*cloudstack.VirtualMachine{
				{Id: "vm1"},
			},
		}, nil).Once()

		manager := &cloudstackManager{
			client: cli,
			projects: &projectCache{
				client: cli,
				maxAge: time.Hour,
			},
			scaler: &csScaler{
				client: cli,
			},
			labelConfig: []cloudprovider.LabelAutoDiscoveryConfig{
				{Selector: map[string]string{"a": "b"}},
			},
		}
		err := manager.Refresh()
		require.Error(t, err)
		assert.Regexp(t, `more than one AutoScaleVMProfile with the nodeGroupName "ng1", ids: asp2 and asp1`, err.Error())
		cli.AssertExpectations(t)
	})
}
