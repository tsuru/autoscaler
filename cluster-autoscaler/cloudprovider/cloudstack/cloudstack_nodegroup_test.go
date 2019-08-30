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
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/xanzy/go-cloudstack/v2/cloudstack"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
	kubeletapis "k8s.io/kubernetes/pkg/kubelet/apis"
)

func Test_csNodeGroup_MaxSize(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		g := &csNodeGroup{
			vmProfile: vmProfile{
				aspMetadata: map[string]string{
					"maxNodes": "10",
				},
			},
		}
		assert.Equal(t, 10, g.MaxSize())
	})
}

func Test_csNodeGroup_MinSize(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		g := &csNodeGroup{
			vmProfile: vmProfile{
				aspMetadata: map[string]string{
					"minNodes": "2",
				},
			},
		}
		assert.Equal(t, 2, g.MinSize())
	})
}

func Test_csNodeGroup_TargetSize(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		g := &csNodeGroup{
			vmProfile: vmProfile{},
			vms: []*cloudstack.VirtualMachine{
				{Id: "xyz"},
				{Id: "abc"},
				{Id: "efg"},
			},
		}
		count, err := g.TargetSize()
		require.NoError(t, err)
		assert.Equal(t, 3, count)
	})

	t.Run("empty", func(t *testing.T) {
		g := &csNodeGroup{
			vmProfile: vmProfile{},
		}
		count, err := g.TargetSize()
		require.NoError(t, err)
		assert.Equal(t, 0, count)
	})

	t.Run("increase target to min", func(t *testing.T) {
		cli := &fakeClient{}
		cli.On("DeployVirtualMachine", mock.Anything).Return(&cloudstack.DeployVirtualMachineResponse{Id: "vm1-id1"}, nil).Once()
		cli.On("CreateTags", mock.Anything).Return(&cloudstack.CreateTagsResponse{}, nil).Once()

		var listParams cloudstack.ListVirtualMachinesParams
		listParams.SetTags(map[string]string{"nodeGroupName": "ng1"})
		cli.On("ListVirtualMachines", &listParams).Return(&cloudstack.ListVirtualMachinesResponse{
			Count: 2,
			VirtualMachines: []*cloudstack.VirtualMachine{
				{Id: "xyz"},
				{Id: "vm1-id1"},
			},
		}, nil).Once()
		cli.On("GetServiceOfferingByID", "offering1").Return(&cloudstack.ServiceOffering{
			Name: "offering1name",
		}, 1, nil)
		cli.On("GetZoneByID", "zone1").Return(&cloudstack.Zone{
			Name: "zone1name",
		}, 1, nil)

		g := &csNodeGroup{
			manager: &cloudstackManager{
				client: cli,
				scaler: &csScaler{
					client: cli,
				},
			},
			vmProfile: vmProfile{
				asp: cloudstack.AutoScaleVmProfile{
					Id:                "asp1",
					Serviceofferingid: "offering1",
					Zoneid:            "zone1",
				},
				aspMetadata: map[string]string{
					"minNodes":      "2",
					"nodeGroupName": "ng1",
				},
			},
			vms: []*cloudstack.VirtualMachine{
				{Id: "xyz"},
			},
		}
		count, err := g.TargetSize()
		require.NoError(t, err)
		assert.Equal(t, 2, count)

		cli.AssertExpectations(t)
	})
}

func Test_csNodeGroup_IncreaseSize(t *testing.T) {
	cli := &fakeClient{}
	cli.On("DeployVirtualMachine", mock.Anything).Return(&cloudstack.DeployVirtualMachineResponse{Id: "vm1-id1"}, nil).Twice()
	cli.On("CreateTags", mock.Anything).Return(&cloudstack.CreateTagsResponse{}, nil).Twice()

	var listParams cloudstack.ListVirtualMachinesParams
	listParams.SetTags(map[string]string{"nodeGroupName": "ng1"})
	cli.On("ListVirtualMachines", &listParams).Return(&cloudstack.ListVirtualMachinesResponse{
		Count: 1,
		VirtualMachines: []*cloudstack.VirtualMachine{
			{Id: "vm1-id1"},
			{Id: "vm2-id2"},
		},
	}, nil).Once()
	cli.On("GetServiceOfferingByID", "offering1").Return(&cloudstack.ServiceOffering{
		Name: "offering1name",
	}, 1, nil)
	cli.On("GetZoneByID", "zone1").Return(&cloudstack.Zone{
		Name: "zone1name",
	}, 1, nil)

	t.Run("ok", func(t *testing.T) {
		g := &csNodeGroup{
			manager: &cloudstackManager{
				client: cli,
				scaler: &csScaler{
					client: cli,
				},
			},
			vmProfile: vmProfile{
				asp: cloudstack.AutoScaleVmProfile{
					Id:                "asp1",
					Serviceofferingid: "offering1",
					Zoneid:            "zone1",
				},
				aspMetadata: map[string]string{
					"nodeGroupName": "ng1",
					"maxNodes":      "10",
				},
			},
		}
		g.manager.nodeGroups = []csNodeGroup{*g}
		err := g.IncreaseSize(2)
		require.NoError(t, err)

		count, err := g.TargetSize()
		require.NoError(t, err)
		assert.Equal(t, 2, count)

		cli.AssertExpectations(t)
	})
}

func Test_csNodeGroup_DeleteNodes(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		cli := &fakeClient{}
		cli.On("DestroyVirtualMachine", mock.Anything).Return(&cloudstack.DestroyVirtualMachineResponse{}, nil)

		g := &csNodeGroup{
			manager: &cloudstackManager{
				client: cli,
				scaler: &csScaler{
					client: cli,
				},
			},
			vms: []*cloudstack.VirtualMachine{
				{Id: "xyz"},
				{Id: "abc"},
			},
			vmProfile: vmProfile{
				asp: cloudstack.AutoScaleVmProfile{Id: "asp1"},
				aspMetadata: map[string]string{
					"maxNodes": "10",
				},
			},
		}
		g.manager.nodeGroups = []csNodeGroup{*g}
		err := g.DeleteNodes([]*apiv1.Node{{Spec: apiv1.NodeSpec{ProviderID: "abc"}}})
		require.NoError(t, err)

		cli.AssertExpectations(t)

		assert.Equal(t, []*cloudstack.VirtualMachine{{Id: "xyz"}}, g.vms)
		target, err := g.TargetSize()
		require.NoError(t, err)
		assert.Equal(t, 1, target)
	})

	t.Run("with prefix", func(t *testing.T) {
		cli := &fakeClient{}
		cli.On("DestroyVirtualMachine", mock.Anything).Return(&cloudstack.DestroyVirtualMachineResponse{}, nil)

		g := &csNodeGroup{
			manager: &cloudstackManager{
				client: cli,
				scaler: &csScaler{
					client: cli,
				},
			},
			vms: []*cloudstack.VirtualMachine{
				{Id: "xyz"},
				{Id: "abc"},
			},
			vmProfile: vmProfile{
				asp: cloudstack.AutoScaleVmProfile{Id: "asp1"},
				aspMetadata: map[string]string{
					"maxNodes":         "10",
					"targetNodes":      "2",
					"providerIDPrefix": "x://",
				},
			},
		}
		g.manager.nodeGroups = []csNodeGroup{*g}
		err := g.DeleteNodes([]*apiv1.Node{{Spec: apiv1.NodeSpec{ProviderID: "x://abc"}}})
		require.NoError(t, err)

		cli.AssertExpectations(t)

		assert.Equal(t, []*cloudstack.VirtualMachine{{Id: "xyz"}}, g.vms)
		target, err := g.TargetSize()
		require.NoError(t, err)
		assert.Equal(t, 1, target)
	})

	t.Run("not found", func(t *testing.T) {
		cli := &fakeClient{}
		g := &csNodeGroup{
			manager: &cloudstackManager{
				client: cli,
				scaler: &csScaler{
					client: cli,
				},
			},
			vms: []*cloudstack.VirtualMachine{
				{Id: "xyz"},
				{Id: "abc"},
			},
			vmProfile: vmProfile{
				asp: cloudstack.AutoScaleVmProfile{Id: "asp1"},
				aspMetadata: map[string]string{
					"nodeGroupName": "ng1",
					"maxNodes":      "10",
					"targetNodes":   "2",
				},
			},
		}
		g.manager.nodeGroups = []csNodeGroup{*g}
		err := g.DeleteNodes([]*apiv1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "n1"}, Spec: apiv1.NodeSpec{ProviderID: "qwerty"}}})
		require.Error(t, err)
		assert.Equal(t, "node (n1, qwerty) not found in nodeGroup ng1", err.Error())
		assert.Equal(t, []*cloudstack.VirtualMachine{{Id: "xyz"}, {Id: "abc"}}, g.vms)
	})
}

func Test_csNodeGroup_Id(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		g := &csNodeGroup{
			vmProfile: vmProfile{
				asp: cloudstack.AutoScaleVmProfile{Id: "asp1"},
				aspMetadata: map[string]string{
					"nodeGroupName": "ng1",
				},
			},
		}
		assert.Equal(t, "ng1", g.Id())
	})
}

func Test_csNodeGroup_Nodes(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		g := &csNodeGroup{
			vms: []*cloudstack.VirtualMachine{
				{Id: "xyz", State: "Running"},
				{Id: "abc", State: "Stopped"},
				{Id: "qwe", State: "Starting"},
				{Id: "zzz", State: "Unknown"},
			},
			vmProfile: vmProfile{
				asp: cloudstack.AutoScaleVmProfile{Id: "asp1"},
			},
		}
		nodes, err := g.Nodes()
		require.NoError(t, err)
		assert.Equal(t, []cloudprovider.Instance{
			{Id: "xyz", Status: &cloudprovider.InstanceStatus{State: cloudprovider.InstanceRunning}},
			{Id: "abc", Status: &cloudprovider.InstanceStatus{State: cloudprovider.InstanceDeleting}},
			{Id: "qwe", Status: &cloudprovider.InstanceStatus{State: cloudprovider.InstanceCreating}},
			{Id: "zzz", Status: &cloudprovider.InstanceStatus{ErrorInfo: &cloudprovider.InstanceErrorInfo{
				ErrorClass:   cloudprovider.OtherErrorClass,
				ErrorCode:    "",
				ErrorMessage: "unexpected vm state: Unknown",
			}}},
		}, nodes)
	})
}

func Test_csNodeGroup_TemplateNodeInfo(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		g := &csNodeGroup{
			manager: &cloudstackManager{
				scaler: &csScaler{},
			},
			vmProfile: vmProfile{
				asp: cloudstack.AutoScaleVmProfile{
					Id: "asp1", Serviceofferingid: "offering1", Zoneid: "zone1",
					Otherdeployparams: "rootdisksize=12",
				},
				aspMetadata: map[string]string{
					"a":            "b",
					"minNodes":     "0",
					"maxNodes":     "10",
					"label-label1": "value1",
					"label-label2": "value2",
				},
				offering: cloudstack.ServiceOffering{
					Name:      "offering1name",
					Cpunumber: 2,
					Memory:    16384,
				},
				zone: cloudstack.Zone{
					Name: "zone1name",
				},
			},
			vms: []*cloudstack.VirtualMachine{
				{Id: "vm1"},
			},
		}
		g.manager.nodeGroups = []csNodeGroup{*g}
		rand.Seed(0)
		nodeInfo, err := g.TemplateNodeInfo()
		require.NoError(t, err)

		rand.Seed(0)
		name := g.manager.scaler.randomName(g.Id())
		expectedNode := &apiv1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:     name,
				SelfLink: fmt.Sprintf("/api/v1/nodes/%s", name),
				Labels: map[string]string{
					kubeletapis.LabelArch:        cloudprovider.DefaultArch,
					kubeletapis.LabelOS:          cloudprovider.DefaultOS,
					apiv1.LabelInstanceType:      "offering1name",
					apiv1.LabelZoneRegion:        "zone1name",
					apiv1.LabelZoneFailureDomain: "zone1name",
					"label1":                     "value1",
					"label2":                     "value2",
				},
			},
			Status: apiv1.NodeStatus{
				Capacity: apiv1.ResourceList{
					apiv1.ResourcePods:             *resource.NewQuantity(110, resource.DecimalSI),
					apiv1.ResourceCPU:              *resource.NewQuantity(2, resource.DecimalSI),
					apiv1.ResourceMemory:           *resource.NewQuantity(16384*1000*1000, resource.DecimalSI),
					apiv1.ResourceEphemeralStorage: *resource.NewQuantity(12*1024*1024*1024, resource.DecimalSI),
				},
			},
		}
		expectedNode.Status.Allocatable = expectedNode.Status.Capacity

		node := nodeInfo.Node()
		node.Status.Conditions = nil
		assert.Equal(t, expectedNode, node)
	})
}

func Test_csNodeGroup_Exist(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		g := &csNodeGroup{}
		assert.True(t, g.Exist())
	})
}

func Test_csNodeGroup_Autoprovisioned(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		g := &csNodeGroup{}
		assert.False(t, g.Autoprovisioned())
	})
}
