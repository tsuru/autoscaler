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
	"io"
	"os"

	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
	"k8s.io/autoscaler/cluster-autoscaler/config"
	"k8s.io/autoscaler/cluster-autoscaler/utils/errors"
	klog "k8s.io/klog/v2"
)

const (
	// GPULabel is the label added to nodes with GPU resource.
	GPULabel = "cloudstack.apache.org/gpu-node"
)

var _ cloudprovider.CloudProvider = &csCloudProvider{}
var _ cloudprovider.NodeGroup = &csNodeGroup{}

type csCloudProvider struct {
	manager         *cloudstackManager
	resourceLimiter *cloudprovider.ResourceLimiter
}

// Name returns name of the cloud provider.
func (cs *csCloudProvider) Name() string {
	return cloudprovider.GloboCloudstackProviderName
}

// NodeGroups returns all node groups configured for this cloud provider.
func (cs *csCloudProvider) NodeGroups() []cloudprovider.NodeGroup {
	var nodeGroups []cloudprovider.NodeGroup
	for i := range cs.manager.nodeGroups {
		nodeGroups = append(nodeGroups, &cs.manager.nodeGroups[i])
	}
	return nodeGroups
}

// NodeGroupForNode returns the node group for the given node, nil if the node
// should not be processed by cluster autoscaler, or non-nil error if such
// occurred. Must be implemented.
func (cs *csCloudProvider) NodeGroupForNode(node *apiv1.Node) (cloudprovider.NodeGroup, error) {
	for i, nodeGroup := range cs.manager.nodeGroups {
		for _, vm := range nodeGroup.vms {
			if nodeGroup.providerID(vm.Id) == node.Spec.ProviderID {
				return &cs.manager.nodeGroups[i], nil
			}
		}
	}
	return nil, nil
}

// Pricing returns pricing model for this cloud provider or error if not available.
// Implementation optional.
func (cs *csCloudProvider) Pricing() (cloudprovider.PricingModel, errors.AutoscalerError) {
	return nil, cloudprovider.ErrNotImplemented
}

// GetAvailableMachineTypes get all machine types that can be requested from the cloud provider.
// Implementation optional.
func (cs *csCloudProvider) GetAvailableMachineTypes() ([]string, error) {
	return []string{}, nil
}

// NewNodeGroup builds a theoretical node group based on the node definition provided. The node group is not automatically
// created on the cloud provider side. The node group is not returned by NodeGroups() until it is created.
// Implementation optional.
func (cs *csCloudProvider) NewNodeGroup(machineType string, labels map[string]string, systemLabels map[string]string,
	taints []apiv1.Taint, extraResources map[string]resource.Quantity) (cloudprovider.NodeGroup, error) {
	return nil, cloudprovider.ErrNotImplemented
}

// GetResourceLimiter returns struct containing limits (max, min) for resources (cores, memory etc.).
func (cs *csCloudProvider) GetResourceLimiter() (*cloudprovider.ResourceLimiter, error) {
	return cs.resourceLimiter, nil
}

// GPULabel returns the label added to nodes with GPU resource.
func (cs *csCloudProvider) GPULabel() string {
	return GPULabel
}

// GetAvailableGPUTypes return all available GPU types cloud provider supports.
func (cs *csCloudProvider) GetAvailableGPUTypes() map[string]struct{} {
	return nil
}

// Cleanup cleans up open resources before the cloud provider is destroyed, i.e. go routines etc.
func (cs *csCloudProvider) Cleanup() error {
	return nil
}

// Refresh is called before every main loop and can be used to dynamically update cloud provider state.
// In particular the list of node groups returned by NodeGroups can change as a result of CloudProvider.Refresh().
func (cs *csCloudProvider) Refresh() error {
	klog.V(4).Info("Refreshing node group cache")
	return cs.manager.Refresh()
}

func newCloudstackCloudProvider(manager *cloudstackManager, rl *cloudprovider.ResourceLimiter) (*csCloudProvider, error) {
	if err := manager.Refresh(); err != nil {
		return nil, err
	}

	return &csCloudProvider{
		manager:         manager,
		resourceLimiter: rl,
	}, nil
}

// BuildCloudstack builds the Cloudstack cloud provider.
func BuildCloudstack(opts config.AutoscalingOptions, do cloudprovider.NodeGroupDiscoveryOptions, rl *cloudprovider.ResourceLimiter) cloudprovider.CloudProvider {
	var configFile io.ReadCloser
	if opts.CloudConfig != "" {
		var err error
		configFile, err = os.Open(opts.CloudConfig)
		if err != nil {
			klog.Fatalf("Couldn't open cloud provider configuration %s: %#v", opts.CloudConfig, err)
		}
		defer configFile.Close()
	}

	manager, err := newManager(configFile, do)
	if err != nil {
		klog.Fatalf("Failed to create Cloudstack manager: %v", err)
	}

	provider, err := newCloudstackCloudProvider(manager, rl)
	if err != nil {
		klog.Fatalf("Failed to create Cloudstack cloud provider: %v", err)
	}

	return provider
}
