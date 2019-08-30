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

package globocloudstack

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xanzy/go-cloudstack/v2/cloudstack"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
)

func Test_csCloudProvider_Name(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		cs := &csCloudProvider{}
		assert.Equal(t, "globo-cloudstack", cs.Name())
	})
}

func Test_csCloudProvider_NodeGroups(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		cs := &csCloudProvider{
			manager: &cloudstackManager{
				nodeGroups: []csNodeGroup{
					{vmProfile: vmProfile{asp: cloudstack.AutoScaleVmProfile{Id: "asp1"}}},
				},
			},
		}
		assert.Equal(t, []cloudprovider.NodeGroup{
			&csNodeGroup{vmProfile: vmProfile{asp: cloudstack.AutoScaleVmProfile{Id: "asp1"}}},
		}, cs.NodeGroups())
	})
}

func Test_csCloudProvider_NodeGroupForNode(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		cs := &csCloudProvider{
			manager: &cloudstackManager{
				nodeGroups: []csNodeGroup{
					{
						vmProfile: vmProfile{
							asp: cloudstack.AutoScaleVmProfile{Id: "asp1"},
						},
						vms: []*cloudstack.VirtualMachine{
							{Id: "xyz"},
						},
					},
					{
						vmProfile: vmProfile{
							asp: cloudstack.AutoScaleVmProfile{Id: "asp2"},
						},
						vms: []*cloudstack.VirtualMachine{
							{Id: "abc"},
						},
					},
				},
			},
		}
		result, err := cs.NodeGroupForNode(&apiv1.Node{
			Spec: apiv1.NodeSpec{ProviderID: "abc"},
		})
		require.NoError(t, err)
		assert.Equal(t, &csNodeGroup{
			vmProfile: vmProfile{
				asp: cloudstack.AutoScaleVmProfile{Id: "asp2"},
			},
			vms: []*cloudstack.VirtualMachine{
				{Id: "abc"},
			},
		}, result)
	})

	t.Run("not found", func(t *testing.T) {
		cs := &csCloudProvider{
			manager: &cloudstackManager{
				nodeGroups: []csNodeGroup{
					{
						vmProfile: vmProfile{
							asp: cloudstack.AutoScaleVmProfile{Id: "asp1"},
						},
						vms: []*cloudstack.VirtualMachine{
							{Id: "xyz"},
						},
					},
					{
						vmProfile: vmProfile{
							asp: cloudstack.AutoScaleVmProfile{Id: "asp2"},
						},
						vms: []*cloudstack.VirtualMachine{
							{Id: "abc"},
						},
					},
				},
			},
		}
		result, err := cs.NodeGroupForNode(&apiv1.Node{
			Spec: apiv1.NodeSpec{ProviderID: "aaa"},
		})
		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("with prefix", func(t *testing.T) {
		cs := &csCloudProvider{
			manager: &cloudstackManager{
				nodeGroups: []csNodeGroup{
					{
						vmProfile: vmProfile{
							asp: cloudstack.AutoScaleVmProfile{Id: "asp1"},
							aspMetadata: map[string]string{
								autoScaleProfileMetadataProviderIDPrefix: "someprefix://test/",
							},
						},
						vms: []*cloudstack.VirtualMachine{
							{Id: "xyz"},
						},
					},
					{
						vmProfile: vmProfile{
							asp: cloudstack.AutoScaleVmProfile{Id: "asp2"},
							aspMetadata: map[string]string{
								autoScaleProfileMetadataProviderIDPrefix: "someprefix://test/",
							},
						},
						vms: []*cloudstack.VirtualMachine{
							{Id: "abc"},
						},
					},
				},
			},
		}
		result, err := cs.NodeGroupForNode(&apiv1.Node{
			Spec: apiv1.NodeSpec{ProviderID: "someprefix://test/abc"},
		})
		require.NoError(t, err)
		assert.Equal(t, &csNodeGroup{
			vmProfile: vmProfile{
				asp: cloudstack.AutoScaleVmProfile{Id: "asp2"},
				aspMetadata: map[string]string{
					autoScaleProfileMetadataProviderIDPrefix: "someprefix://test/",
				},
			},
			vms: []*cloudstack.VirtualMachine{
				{Id: "abc"},
			},
		}, result)
	})
}
