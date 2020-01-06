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
	"fmt"
	"strings"

	"github.com/xanzy/go-cloudstack/v2/cloudstack"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
	"k8s.io/autoscaler/cluster-autoscaler/config"
	klog "k8s.io/klog/v2"
	schedulerframework "k8s.io/kubernetes/pkg/scheduler/framework"
)

type csNodeGroup struct {
	manager   *cloudstackManager
	vmProfile vmProfile
	vms       []*cloudstack.VirtualMachine
}

// MaxSize returns maximum size of the node group.
func (g *csNodeGroup) MaxSize() int {
	return g.vmProfile.maxSize()
}

// MinSize returns minimum size of the node group.
func (g *csNodeGroup) MinSize() int {
	return g.vmProfile.minSize()
}

// TargetSize returns the current target size of the node group. It is possible that the
// number of nodes in Kubernetes is different at the moment but should be equal
// to Size() once everything stabilizes (new nodes finish startup and registration or
// removed nodes are deleted completely). Implementation required.
func (g *csNodeGroup) TargetSize() (int, error) {
	targetSize := len(g.vms)
	minSize := g.MinSize()

	if targetSize < minSize {
		err := g.manager.scaleUp(g, minSize-targetSize)
		if err != nil {
			klog.Errorf("failed to scale-up group %q to min-size: %v", g.vmProfile.Id(), err)
		}
		targetSize = len(g.vms)
	}

	return targetSize, nil
}

// IncreaseSize increases the size of the node group. To delete a node you need
// to explicitly name it and use DeleteNode. This function should wait until
// node group size is updated. Implementation required.
func (g *csNodeGroup) IncreaseSize(delta int) error {
	if delta <= 0 {
		return fmt.Errorf("delta must be positive, have: %d", delta)
	}
	currentSize, err := g.TargetSize()
	if err != nil {
		return err
	}

	targetSize := currentSize + delta

	if targetSize > g.MaxSize() {
		return fmt.Errorf("size increase is too large. current: %d desired: %d max: %d",
			currentSize, targetSize, g.MaxSize())
	}

	return g.manager.scaleUp(g, delta)
}

// DeleteNodes deletes nodes from this node group. Error is returned either on
// failure or if the given node doesn't belong to this node group. This function
// should wait until node group size is updated. Implementation required.
func (g *csNodeGroup) DeleteNodes(nodes []*apiv1.Node) error {
	for _, n := range nodes {
		vm, err := g.vmForNode(n)
		if err != nil {
			return err
		}
		err = g.manager.scaler.destroyVM(vm.Id)
		if err != nil {
			return err
		}
		g.removeVM(vm.Id)
	}
	return nil
}

// DecreaseTargetSize decreases the target size of the node group. This function
// doesn't permit to delete any existing node and can be used only to reduce the
// request for new nodes that have not been yet fulfilled. Delta should be negative.
// It is assumed that cloud provider will not delete the existing nodes when there
// is an option to just decrease the target. Implementation required.
func (g *csNodeGroup) DecreaseTargetSize(delta int) error {
	return nil
}

// Id returns an unique identifier of the node group.
func (g *csNodeGroup) Id() string {
	return g.vmProfile.Id()
}

// Debug returns a string containing all information regarding this node group.
func (g *csNodeGroup) Debug() string {
	return fmt.Sprintf("vmProfile: %#v", g.vmProfile)
}

// Nodes returns a list of all nodes that belong to this node group.
// It is required that Instance objects returned by this method have Id field set.
// Other fields are optional.
func (g *csNodeGroup) Nodes() ([]cloudprovider.Instance, error) {
	var instances []cloudprovider.Instance
	for _, vm := range g.vms {
		instances = append(instances, cloudprovider.Instance{
			Id:     g.providerID(vm.Id),
			Status: toInstanceStatus(vm.State),
		})
	}
	return instances, nil
}

// TemplateNodeInfo returns a schedulernodeinfo.NodeInfo structure of an empty
// (as if just started) node. This will be used in scale-up simulations to
// predict what would a new node look like if a node group was expanded. The returned
// NodeInfo is expected to have a fully populated Node object, with all of the labels,
// capacity and allocatable information as well as all pods that are started on
// the node by default, using manifest (most likely only kube-proxy). Implementation optional.
func (g *csNodeGroup) TemplateNodeInfo() (*schedulerframework.NodeInfo, error) {
	node, err := g.manager.buildNode(g)
	if err != nil {
		return nil, err
	}

	nodeInfo := schedulerframework.NewNodeInfo(cloudprovider.BuildKubeProxy(g.Id()))
	nodeInfo.SetNode(node)
	return nodeInfo, nil
}

// Exist checks if the node group really exists on the cloud provider side. Allows to tell the
// theoretical node group from the real one. Implementation required.
func (g *csNodeGroup) Exist() bool {
	return true
}

// Create creates the node group on the cloud provider side. Implementation optional.
func (g *csNodeGroup) Create() (cloudprovider.NodeGroup, error) {
	return nil, cloudprovider.ErrNotImplemented
}

// Delete deletes the node group on the cloud provider side.
// This will be executed only for autoprovisioned node groups, once their size drops to 0.
// Implementation optional.
func (g *csNodeGroup) Delete() error {
	return cloudprovider.ErrNotImplemented
}

// Autoprovisioned returns true if the node group is autoprovisioned. An autoprovisioned group
// was created by CA and can be deleted when scaled to 0.
func (g *csNodeGroup) Autoprovisioned() bool {
	return false
}

func (g *csNodeGroup) removeVM(vmID string) {
	for i := 0; i < len(g.vms); i++ {
		if g.vms[i].Id == vmID {
			g.vms[i] = g.vms[len(g.vms)-1]
			g.vms = g.vms[:len(g.vms)-1]
			return
		}
	}
}

func (g *csNodeGroup) vmForNode(node *apiv1.Node) (*cloudstack.VirtualMachine, error) {
	vmID := strings.TrimPrefix(node.Spec.ProviderID, g.vmProfile.providerIDPrefix())
	for _, vm := range g.vms {
		if vm.Id == vmID {
			return vm, nil
		}
	}
	return nil, fmt.Errorf("node (%v, %v) not found in nodeGroup %v", node.Name, node.Spec.ProviderID, g.Id())
}

func (g *csNodeGroup) providerID(vmID string) string {
	return g.vmProfile.providerIDPrefix() + vmID
}

func (g *csNodeGroup) GetOptions(defaults config.NodeGroupAutoscalingOptions) (*config.NodeGroupAutoscalingOptions, error) {
	return nil, cloudprovider.ErrNotImplemented
}

func toInstanceStatus(csState string) *cloudprovider.InstanceStatus {
	// Possible states from https://github.com/apache/cloudstack/blob/87c43501608a1df72a2f01ed17a522233e6617b0/api/src/main/java/com/cloud/vm/VirtualMachine.java#L45

	var state cloudprovider.InstanceState
	var err *cloudprovider.InstanceErrorInfo

	switch csState {
	case "Starting", "Migrating":
		state = cloudprovider.InstanceCreating
	case "Running":
		state = cloudprovider.InstanceRunning
	case "Stopping", "Stopped", "Destroyed", "Expunging", "Shutdowned":
		state = cloudprovider.InstanceDeleting
	default:
		err = &cloudprovider.InstanceErrorInfo{
			ErrorClass:   cloudprovider.OtherErrorClass,
			ErrorCode:    "",
			ErrorMessage: fmt.Sprintf("unexpected vm state: %v", csState),
		}
	}

	return &cloudprovider.InstanceStatus{
		State:     state,
		ErrorInfo: err,
	}
}
