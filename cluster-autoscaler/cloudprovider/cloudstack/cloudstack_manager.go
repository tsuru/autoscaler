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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/xanzy/go-cloudstack/v2/cloudstack"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
	kubeletapis "k8s.io/kubernetes/pkg/kubelet/apis"
)

const (
	defaultProjectRefreshInterval = 30 * time.Minute

	resourceTypeAutoScaleVmProfile = "AutoScaleVmProfile"
	resourceTypeVirtualMachine     = "UserVm"

	autoDiscovererTypeLabel = "label"

	autoScaleProfileMetadataName             = "nodeGroupName"
	autoScaleProfileMetadataMin              = "minNodes"
	autoScaleProfileMetadataMax              = "maxNodes"
	autoScaleProfileMetadataUserdata         = "userdata"
	autoScaleProfileMetadataProviderIDPrefix = "providerIDPrefix"

	autoScaleProfileMetadataNodeLabelPrefix = "label-"
	autoScaleProfileMetadataVMTagPrefix     = "tag-"

	nodeGroupVMTag = autoScaleProfileMetadataName
)

var requiredAutoScaleProfileMetadata = []string{
	autoScaleProfileMetadataName,
	autoScaleProfileMetadataMin,
	autoScaleProfileMetadataMax,
}

type cloudstackManager struct {
	config      csConfig
	client      cloudstackClient
	nodeGroups  []csNodeGroup
	projects    *projectCache
	labelConfig []labelAutoDiscoveryConfig
	scaler      *csScaler
}

type cloudstackClient interface {
	projectCloudstackClient
	scalerCloudstackClient
	ListAutoScaleVmProfiles(*cloudstack.ListAutoScaleVmProfilesParams) (*cloudstack.ListAutoScaleVmProfilesResponse, error)
	ListResourceDetails(*cloudstack.ListResourceDetailsParams) (*cloudstack.ListResourceDetailsResponse, error)
	ListVirtualMachines(*cloudstack.ListVirtualMachinesParams) (*cloudstack.ListVirtualMachinesResponse, error)
	GetServiceOfferingByID(string, ...cloudstack.OptionFunc) (*cloudstack.ServiceOffering, int, error)
	GetZoneByID(string, ...cloudstack.OptionFunc) (*cloudstack.Zone, int, error)
	AddResourceDetail(*cloudstack.AddResourceDetailParams) (*cloudstack.AddResourceDetailResponse, error)
}

type aggregatedClient struct {
	*cloudstack.AutoScaleService
	*cloudstack.ResourcemetadataService
	*cloudstack.VirtualMachineService
	*cloudstack.ServiceOfferingService
	*cloudstack.ZoneService
	*cloudstack.ProjectService
	*cloudstack.ResourcetagsService
}

type csConfig struct {
	// APIKey is the key associated with the user account.
	APIKey string `json:"api_key"`

	// APISecret is the secret associated with the user account.
	APISecret string `json:"api_secret"`

	// InsecureSkipVerify points to Cloudstack API.
	InsecureSkipVerify bool `json:"insecure"`

	// UseProjects controls if node groups should be searched on all projects.
	UseProjects bool `json:"use_projects"`

	// ProjectRefreshInterval controls the refresh interval for existing projects list.
	ProjectRefreshInterval string `json:"project_refresh_interval"`

	// ExpungeVMs controls if the expunge flag should be set on delete.
	ExpungeVMs bool `json:"expunge_vms"`

	// URL points to Cloudstack API.
	URL string `json:"url"`
}

var newCloudstackClient = func(cfg csConfig) cloudstackClient {
	cs := cloudstack.NewAsyncClient(cfg.URL, cfg.APIKey, cfg.APISecret, !cfg.InsecureSkipVerify)

	return aggregatedClient{
		AutoScaleService:        cs.AutoScale,
		ResourcemetadataService: cs.Resourcemetadata,
		VirtualMachineService:   cs.VirtualMachine,
		ServiceOfferingService:  cs.ServiceOffering,
		ZoneService:             cs.Zone,
		ProjectService:          cs.Project,
		ResourcetagsService:     cs.Resourcetags,
	}
}

func newManager(configReader io.Reader, do cloudprovider.NodeGroupDiscoveryOptions) (*cloudstackManager, error) {
	cfg, err := loadConfig(configReader)
	if err != nil {
		return nil, err
	}

	if cfg.APIKey == "" {
		return nil, errors.New("api key is required")
	}
	if cfg.APISecret == "" {
		return nil, errors.New("api secret is required")
	}
	if cfg.URL == "" {
		return nil, errors.New("URL is required")
	}

	projectRefreshInterval := defaultProjectRefreshInterval
	if cfg.ProjectRefreshInterval != "" {
		var err error
		projectRefreshInterval, err = time.ParseDuration(cfg.ProjectRefreshInterval)
		if err != nil {
			return nil, err
		}
	}

	if !do.AutoDiscoverySpecified() {
		return nil, errors.New("auto discovery configuration is required")
	}

	labelConfig, err := parseLabelAutoDiscoverySpecs(do)
	if err != nil {
		return nil, err
	}

	cli := newCloudstackClient(cfg)

	projects, err := newProjectCache(cli, cfg.UseProjects, projectRefreshInterval)
	if err != nil {
		return nil, err
	}

	scaler, err := newCsScaler(cli, cfg.ExpungeVMs)
	if err != nil {
		return nil, err
	}

	return &cloudstackManager{
		client:      cli,
		config:      cfg,
		projects:    projects,
		labelConfig: labelConfig,
		scaler:      scaler,
	}, nil
}

func loadConfig(configReader io.Reader) (csConfig, error) {
	cfg := csConfig{}

	if configReader != nil {
		body, err := ioutil.ReadAll(configReader)
		if err != nil {
			return cfg, err
		}
		err = json.Unmarshal(body, &cfg)
		if err != nil {
			return cfg, err
		}
	}

	if v, ok := os.LookupEnv("CLOUDSTACK_API_KEY"); ok {
		cfg.APIKey = v
	}
	if v, ok := os.LookupEnv("CLOUDSTACK_API_SECRET"); ok {
		cfg.APISecret = v
	}
	if v, ok := os.LookupEnv("CLOUDSTACK_INSECURE"); ok {
		cfg.InsecureSkipVerify, _ = strconv.ParseBool(v)
	}
	if v, ok := os.LookupEnv("CLOUDSTACK_USE_PROJECTS"); ok {
		cfg.UseProjects, _ = strconv.ParseBool(v)
	}
	if v, ok := os.LookupEnv("CLOUDSTACK_PROJECT_REFRESH_INTERVAL"); ok {
		cfg.ProjectRefreshInterval = v
	}
	if v, ok := os.LookupEnv("CLOUDSTACK_EXPUNGE_VMS"); ok {
		cfg.ExpungeVMs, _ = strconv.ParseBool(v)
	}
	if v, ok := os.LookupEnv("CLOUDSTACK_URL"); ok {
		cfg.URL = v
	}

	return cfg, nil
}

func (m *cloudstackManager) Refresh() error {
	var nodeGroups []csNodeGroup
	registeredIds := make(map[string]string)
	err := m.projects.forEach(func(projectID string) error {
		var params cloudstack.ListAutoScaleVmProfilesParams
		if projectID != "" {
			params.SetProjectid(projectID)
		}
		asps, err := m.client.ListAutoScaleVmProfiles(&params)
		if err != nil {
			return err
		}
		for _, asp := range asps.AutoScaleVmProfiles {
			var metaParams cloudstack.ListResourceDetailsParams
			metaParams.SetResourcetype(resourceTypeAutoScaleVmProfile)
			metaParams.SetResourceid(asp.Id)
			details, err := m.client.ListResourceDetails(&metaParams)
			if err != nil {
				return err
			}
			metadata := resourceDetailsToMetadata(details.ResourceDetails)
			if m.validASP(metadata) {
				ng := csNodeGroup{
					vmProfile: vmProfile{
						asp:         *asp,
						aspMetadata: metadata,
					},
					manager: m,
				}
				if existingASPID, ok := registeredIds[ng.Id()]; ok {
					return fmt.Errorf("more than one AutoScaleVMProfile with the nodeGroupName %q, ids: %v and %v", ng.Id(), asp.Id, existingASPID)
				}
				registeredIds[ng.Id()] = asp.Id
				err = m.refreshNodeGroupVms(&ng)
				if err != nil {
					return err
				}
				nodeGroups = append(nodeGroups, ng)
			}
		}
		return nil
	})

	m.nodeGroups = nodeGroups

	return err
}

func (m *cloudstackManager) refreshNodeGroupVms(ng *csNodeGroup) error {
	var params cloudstack.ListVirtualMachinesParams
	if projID := ng.vmProfile.projectID(); projID != "" {
		params.SetProjectid(projID)
	}
	params.SetTags(map[string]string{
		nodeGroupVMTag: ng.Id(),
	})

	vms, err := m.client.ListVirtualMachines(&params)
	if err != nil {
		return err
	}
	ng.vms = vms.VirtualMachines

	offering, _, err := m.client.GetServiceOfferingByID(ng.vmProfile.asp.Serviceofferingid)
	if err != nil {
		return err
	}
	ng.vmProfile.offering = *offering

	zone, _, err := m.client.GetZoneByID(ng.vmProfile.asp.Zoneid)
	if err != nil {
		return err
	}
	ng.vmProfile.zone = *zone
	return nil
}

func (m *cloudstackManager) validASP(metadata map[string]string) bool {
	for _, requiredKey := range requiredAutoScaleProfileMetadata {
		if _, ok := metadata[requiredKey]; !ok {
			return false
		}
	}
	return matchesLabelConfigs(metadata, m.labelConfig)
}

func (m *cloudstackManager) scaleUp(nodeGroup *csNodeGroup, toAddCount int) error {
	err := m.scaler.scaleUp(nodeGroup.vmProfile, toAddCount)
	if err != nil {
		return err
	}
	return m.refreshNodeGroupVms(nodeGroup)
}

func (m *cloudstackManager) buildNode(nodeGroup *csNodeGroup) (*apiv1.Node, error) {
	node := apiv1.Node{}
	nodeName := m.scaler.randomName(nodeGroup.Id())

	node.ObjectMeta = metav1.ObjectMeta{
		Name:     nodeName,
		SelfLink: fmt.Sprintf("/api/v1/nodes/%s", nodeName),
		Labels:   map[string]string{},
	}

	node.Status = apiv1.NodeStatus{
		Capacity: apiv1.ResourceList{},
	}

	node.Status.Capacity[apiv1.ResourcePods] = *resource.NewQuantity(110, resource.DecimalSI)
	node.Status.Capacity[apiv1.ResourceCPU] = *resource.NewQuantity(int64(nodeGroup.vmProfile.offering.Cpunumber), resource.DecimalSI)
	node.Status.Capacity[apiv1.ResourceMemory] = *resource.NewQuantity(int64(nodeGroup.vmProfile.offering.Memory)*1000*1000, resource.DecimalSI)
	rootDiskSize := nodeGroup.vmProfile.rootDiskSize()
	if rootDiskSize > 0 {
		node.Status.Capacity[apiv1.ResourceEphemeralStorage] = *resource.NewQuantity(rootDiskSize*1024*1024*1024, resource.DecimalSI)
	}
	node.Status.Allocatable = node.Status.Capacity

	node.Labels = cloudprovider.JoinStringMaps(node.Labels, nodeGroup.vmProfile.labels())
	node.Labels = cloudprovider.JoinStringMaps(node.Labels, buildGenericLabels(nodeGroup))

	node.Status.Conditions = cloudprovider.BuildReadyConditions()
	return &node, nil
}

func buildGenericLabels(nodeGroup *csNodeGroup) map[string]string {
	result := make(map[string]string)
	result[kubeletapis.LabelArch] = cloudprovider.DefaultArch
	result[kubeletapis.LabelOS] = cloudprovider.DefaultOS
	result[apiv1.LabelInstanceType] = nodeGroup.vmProfile.offering.Name
	result[apiv1.LabelZoneRegion] = nodeGroup.vmProfile.zone.Name
	result[apiv1.LabelZoneFailureDomain] = nodeGroup.vmProfile.zone.Name
	return result
}

func resourceDetailsToMetadata(details []*cloudstack.ResourceDetail) map[string]string {
	metadata := map[string]string{}
	for _, item := range details {
		metadata[item.Key] = item.Value
	}
	return metadata
}

func matchesLabelConfigs(metadata map[string]string, labels []labelAutoDiscoveryConfig) bool {
	for _, labelSet := range labels {
		if matchesSelector(metadata, labelSet.Selector) {
			return true
		}
	}
	return false
}

func matchesSelector(existing map[string]string, wanted map[string]string) bool {
	for wantedKey, wantedValue := range wanted {
		existingValue, ok := existing[wantedKey]
		if !ok {
			return false
		}
		if wantedValue != "" && existingValue != wantedValue {
			return false
		}
	}
	return true
}

type labelAutoDiscoveryConfig struct {
	Selector map[string]string
}

func parseLabelAutoDiscoverySpecs(o cloudprovider.NodeGroupDiscoveryOptions) ([]labelAutoDiscoveryConfig, error) {
	cfgs := make([]labelAutoDiscoveryConfig, len(o.NodeGroupAutoDiscoverySpecs))
	var err error
	for i, spec := range o.NodeGroupAutoDiscoverySpecs {
		cfgs[i], err = parseLabelAutoDiscoverySpec(spec)
		if err != nil {
			return nil, err
		}
	}
	return cfgs, nil
}

func parseLabelAutoDiscoverySpec(spec string) (labelAutoDiscoveryConfig, error) {
	cfg := labelAutoDiscoveryConfig{
		Selector: make(map[string]string),
	}

	tokens := strings.Split(spec, ":")
	if len(tokens) != 2 {
		return cfg, fmt.Errorf("spec \"%s\" should be discoverer:key=value,key=value", spec)
	}
	discoverer := tokens[0]
	if discoverer != autoDiscovererTypeLabel {
		return cfg, fmt.Errorf("unsupported discoverer specified: %s", discoverer)
	}

	for _, arg := range strings.Split(tokens[1], ",") {
		kv := strings.Split(arg, "=")
		if len(kv) != 2 {
			return cfg, fmt.Errorf("invalid key=value pair %s", kv)
		}
		k, v := kv[0], kv[1]
		if k == "" || v == "" {
			return cfg, fmt.Errorf("empty value not allowed in key=value tag pairs")
		}
		cfg.Selector[k] = v
	}
	return cfg, nil
}
