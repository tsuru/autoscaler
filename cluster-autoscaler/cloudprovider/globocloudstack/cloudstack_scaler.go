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
	"math/rand"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xanzy/go-cloudstack/v2/cloudstack"
	klog "k8s.io/klog/v2"
)

type csScaler struct {
	client  scalerCloudstackClient
	expunge bool
}

type scalerCloudstackClient interface {
	DestroyVirtualMachine(*cloudstack.DestroyVirtualMachineParams) (*cloudstack.DestroyVirtualMachineResponse, error)
	DeployVirtualMachine(*cloudstack.DeployVirtualMachineParams) (*cloudstack.DeployVirtualMachineResponse, error)
	CreateTags(*cloudstack.CreateTagsParams) (*cloudstack.CreateTagsResponse, error)
}

func newCsScaler(client scalerCloudstackClient, expunge bool) (*csScaler, error) {
	rand.Seed(time.Now().UnixNano())
	return &csScaler{
		client:  client,
		expunge: expunge,
	}, nil
}

func (s *csScaler) destroyVM(vmID string) error {
	var params cloudstack.DestroyVirtualMachineParams
	params.SetId(vmID)
	params.SetExpunge(s.expunge)
	_, err := s.client.DestroyVirtualMachine(&params)
	if isCSErrorNotFound(err, vmID) {
		klog.V(3).Infof("Tried to destroy cloudstack VM %v but it wasn't found, error ignored", vmID)
		return nil
	}
	return err
}

func (s *csScaler) createVM(deploy cloudstack.DeployVirtualMachineParams, tags cloudstack.CreateTagsParams) (err error) {
	vm, err := s.client.DeployVirtualMachine(&deploy)
	if err != nil {
		if vm != nil && vm.Id != "" {
			destroyErr := s.destroyVM(vm.Id)
			if destroyErr != nil {
				err = fmt.Errorf("unable to destroy cloudstack VM after error creating: %v - original error: %v", destroyErr, err)
			}
		}
		return err
	}
	defer func() {
		if err == nil {
			return
		}
		destroyErr := s.destroyVM(vm.Id)
		if destroyErr != nil {
			err = fmt.Errorf("unable to destroy cloudstack VM after tagging error: %v - original error: %v", destroyErr, err)
		}
	}()
	tags.SetResourceids([]string{vm.Id})
	tags.SetResourcetype(resourceTypeVirtualMachine)
	_, err = s.client.CreateTags(&tags)
	return err
}

func (s *csScaler) scaleUp(vmp vmProfile, count int) error {
	errCh := make(chan error, count)
	wg := sync.WaitGroup{}
	for i := 0; i < count; i++ {
		tagsParams := createVMTagsParams(vmp)
		deployParams := createDeployVMParams(vmp)
		deployParams.SetName(s.randomName(vmp.Id()))
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- s.createVM(deployParams, tagsParams)
		}()
	}
	wg.Wait()
	close(errCh)
	var errorMsgs []string
	for err := range errCh {
		if err != nil {
			errorMsgs = append(errorMsgs, err.Error())
		}
	}
	if len(errorMsgs) > 0 {
		return fmt.Errorf("error creating VMs: %v", strings.Join(errorMsgs, " - "))
	}
	return nil
}

func (s *csScaler) randomName(base string) string {
	nodeName := fmt.Sprintf("%s-%d", base, rand.Uint64())
	if len(nodeName) > 63 {
		nodeName = nodeName[:63]
	}
	return nodeName
}

func createDeployVMParams(vmp vmProfile) cloudstack.DeployVirtualMachineParams {
	asp := vmp.asp
	var params cloudstack.DeployVirtualMachineParams
	if projID := vmp.projectID(); projID != "" {
		params.SetProjectid(projID)
	}
	params.SetServiceofferingid(asp.Serviceofferingid)
	params.SetZoneid(asp.Zoneid)
	params.SetTemplateid(asp.Templateid)
	if values, err := url.ParseQuery(asp.Otherdeployparams); err == nil {
		setOtherParams(values, &params)
	}
	if userdata, isSet := vmp.userdata(); isSet {
		params.SetUserdata(userdata)
	}
	return params
}

func createVMTagsParams(vmp vmProfile) cloudstack.CreateTagsParams {
	tags := vmp.tags()
	tags[nodeGroupVMTag] = vmp.Id()
	var params cloudstack.CreateTagsParams
	params.SetTags(tags)
	return params
}

func setOtherParams(values url.Values, params *cloudstack.DeployVirtualMachineParams) {
	if v, found := valueGet(values, "account"); found {
		params.SetAccount(v)
	}
	if v, found := valueGet(values, "affinitygroupids"); found {
		vv := strings.Split(v, ",")
		params.SetAffinitygroupids(vv)
	}
	if v, found := valueGet(values, "affinitygroupnames"); found {
		vv := strings.Split(v, ",")
		params.SetAffinitygroupnames(vv)
	}
	if v, found := valueGet(values, "diskofferingid"); found {
		params.SetDiskofferingid(v)
	}
	if v, found := valueGet(values, "displayname"); found {
		params.SetDisplayname(v)
	}
	if v, found := valueGet(values, "hypervisor"); found {
		params.SetHypervisor(v)
	}
	if v, found := valueGet(values, "keyboard"); found {
		params.SetKeyboard(v)
	}
	if v, found := valueGet(values, "keypair"); found {
		params.SetKeypair(v)
	}
	if v, found := valueGet(values, "networkids"); found {
		vv := strings.Split(v, ",")
		params.SetNetworkids(vv)
	}
	if v, found := valueGet(values, "rootdisksize"); found {
		vv, _ := strconv.ParseInt(v, 10, 64)
		params.SetRootdisksize(vv)
	}
	if v, found := valueGet(values, "securitygroupids"); found {
		vv := strings.Split(v, ",")
		params.SetSecuritygroupids(vv)
	}
	if v, found := valueGet(values, "securitygroupnames"); found {
		vv := strings.Split(v, ",")
		params.SetSecuritygroupnames(vv)
	}
	if v, found := valueGet(values, "size"); found {
		vv, _ := strconv.ParseInt(v, 10, 64)
		params.SetSize(vv)
	}
	if v, found := valueGet(values, "userdata"); found {
		params.SetUserdata(v)
	}
}

func valueGet(values url.Values, key string) (string, bool) {
	_, isSet := values[key]
	return values.Get(key), isSet
}

func isCSErrorNotFound(err error, id string) bool {
	const notFoundPattern = "Invalid parameter id value=%s due to incorrect long value format, or entity does not exist"
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), fmt.Sprintf(notFoundPattern, id))
}
