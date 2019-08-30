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
	"github.com/stretchr/testify/mock"
	"github.com/xanzy/go-cloudstack/v2/cloudstack"
)

type fakeClient struct {
	mock.Mock
}

func (f *fakeClient) DestroyVirtualMachine(params *cloudstack.DestroyVirtualMachineParams) (*cloudstack.DestroyVirtualMachineResponse, error) {
	args := f.Called(params)
	return args.Get(0).(*cloudstack.DestroyVirtualMachineResponse), args.Error(1)
}

func (f *fakeClient) DeployVirtualMachine(params *cloudstack.DeployVirtualMachineParams) (*cloudstack.DeployVirtualMachineResponse, error) {
	args := f.Called(params)
	return args.Get(0).(*cloudstack.DeployVirtualMachineResponse), args.Error(1)
}

func (f *fakeClient) CreateTags(params *cloudstack.CreateTagsParams) (*cloudstack.CreateTagsResponse, error) {
	args := f.Called(params)
	return args.Get(0).(*cloudstack.CreateTagsResponse), args.Error(1)
}

func (f *fakeClient) ListProjects(params *cloudstack.ListProjectsParams) (*cloudstack.ListProjectsResponse, error) {
	args := f.Called(params)
	return args.Get(0).(*cloudstack.ListProjectsResponse), args.Error(1)
}

func (f *fakeClient) ListAutoScaleVmProfiles(params *cloudstack.ListAutoScaleVmProfilesParams) (*cloudstack.ListAutoScaleVmProfilesResponse, error) {
	args := f.Called(params)
	return args.Get(0).(*cloudstack.ListAutoScaleVmProfilesResponse), args.Error(1)
}

func (f *fakeClient) ListResourceDetails(params *cloudstack.ListResourceDetailsParams) (*cloudstack.ListResourceDetailsResponse, error) {
	args := f.Called(params)
	return args.Get(0).(*cloudstack.ListResourceDetailsResponse), args.Error(1)
}

func (f *fakeClient) ListVirtualMachines(params *cloudstack.ListVirtualMachinesParams) (*cloudstack.ListVirtualMachinesResponse, error) {
	args := f.Called(params)
	return args.Get(0).(*cloudstack.ListVirtualMachinesResponse), args.Error(1)
}

func (f *fakeClient) GetServiceOfferingByID(params string, opts ...cloudstack.OptionFunc) (*cloudstack.ServiceOffering, int, error) {
	args := f.Called(params)
	return args.Get(0).(*cloudstack.ServiceOffering), args.Int(1), args.Error(2)
}

func (f *fakeClient) GetZoneByID(params string, opts ...cloudstack.OptionFunc) (*cloudstack.Zone, int, error) {
	args := f.Called(params)
	return args.Get(0).(*cloudstack.Zone), args.Int(1), args.Error(2)
}

func (f *fakeClient) AddResourceDetail(params *cloudstack.AddResourceDetailParams) (*cloudstack.AddResourceDetailResponse, error) {
	args := f.Called(params)
	return args.Get(0).(*cloudstack.AddResourceDetailResponse), args.Error(1)
}
