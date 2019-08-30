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
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/xanzy/go-cloudstack/v2/cloudstack"
)

func Test_newCsScaler(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		cli := &fakeClient{}
		scaler, err := newCsScaler(cli, true)
		require.NoError(t, err)
		assert.Equal(t, &csScaler{client: cli, expunge: true}, scaler)
	})
}

func Test_csScaler_destroyVM(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		cli := &fakeClient{}
		cli.On("DestroyVirtualMachine", mock.Anything).Return(&cloudstack.DestroyVirtualMachineResponse{}, nil)
		scaler := csScaler{client: cli}
		err := scaler.destroyVM("myid")
		require.NoError(t, err)
		expected := &cloudstack.DestroyVirtualMachineParams{}
		expected.SetId("myid")
		expected.SetExpunge(false)
		cli.AssertCalled(t, "DestroyVirtualMachine", expected)
	})

	t.Run("with expunge", func(t *testing.T) {
		cli := &fakeClient{}
		cli.On("DestroyVirtualMachine", mock.Anything).Return(&cloudstack.DestroyVirtualMachineResponse{}, nil)
		scaler := csScaler{client: cli, expunge: true}
		err := scaler.destroyVM("myid")
		require.NoError(t, err)
		expected := &cloudstack.DestroyVirtualMachineParams{}
		expected.SetId("myid")
		expected.SetExpunge(true)
		cli.AssertCalled(t, "DestroyVirtualMachine", expected)
	})

	t.Run("with error", func(t *testing.T) {
		cli := &fakeClient{}
		cli.On("DestroyVirtualMachine", mock.Anything).Return((*cloudstack.DestroyVirtualMachineResponse)(nil), errors.New("my err"))
		scaler := csScaler{client: cli}
		err := scaler.destroyVM("myid")
		require.Error(t, err)
		require.Equal(t, "my err", err.Error())
	})

	t.Run("with not found error", func(t *testing.T) {
		cli := &fakeClient{}
		errMsg := "CloudStack API error 431 (CSExceptionErrorCode: 9999): Unable to execute API command destroyvirtualmachine due to invalid value. Invalid parameter id value=myid due to incorrect long value format, or entity does not exist or due to incorrect parameter annotation for the field in api cmd class."
		cli.On("DestroyVirtualMachine", mock.Anything).Return((*cloudstack.DestroyVirtualMachineResponse)(nil), errors.New(errMsg))
		scaler := csScaler{client: cli}
		err := scaler.destroyVM("myid")
		require.NoError(t, err)
	})
}

func Test_csScaler_createVM(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		cli := &fakeClient{}
		cli.On("DeployVirtualMachine", mock.Anything).Return(&cloudstack.DeployVirtualMachineResponse{Id: "vm1-id1"}, nil)
		cli.On("CreateTags", mock.Anything).Return(&cloudstack.CreateTagsResponse{}, nil)
		scaler := csScaler{client: cli}
		deployArgs := cloudstack.DeployVirtualMachineParams{}
		deployArgs.SetDisplayname("vm1")
		tagArgs := cloudstack.CreateTagsParams{}
		tagArgs.SetTags(map[string]string{"a": "b"})

		err := scaler.createVM(deployArgs, tagArgs)
		require.NoError(t, err)

		expectedTagArgs := cloudstack.CreateTagsParams{}
		expectedTagArgs.SetResourceids([]string{"vm1-id1"})
		expectedTagArgs.SetResourcetype("UserVm")
		expectedTagArgs.SetTags(map[string]string{"a": "b"})
		cli.AssertCalled(t, "DeployVirtualMachine", &deployArgs)
		cli.AssertCalled(t, "CreateTags", &expectedTagArgs)
	})

	t.Run("create err", func(t *testing.T) {
		cli := &fakeClient{}
		cli.On("DeployVirtualMachine", mock.Anything).Return((*cloudstack.DeployVirtualMachineResponse)(nil), errors.New("deploy err"))
		scaler := csScaler{client: cli}
		deployArgs := cloudstack.DeployVirtualMachineParams{}
		deployArgs.SetDisplayname("vm1")
		tagArgs := cloudstack.CreateTagsParams{}
		tagArgs.SetTags(map[string]string{"a": "b"})

		err := scaler.createVM(deployArgs, tagArgs)
		require.Equal(t, errors.New("deploy err"), err)

		cli.AssertCalled(t, "DeployVirtualMachine", &deployArgs)
	})

	t.Run("create err with ID (possibly timeout waiting for async job)", func(t *testing.T) {
		cli := &fakeClient{}
		deployArgs := &cloudstack.DeployVirtualMachineParams{}
		deployArgs.SetDisplayname("vm1")
		cli.On("DeployVirtualMachine", deployArgs).Return(&cloudstack.DeployVirtualMachineResponse{Id: "vm1-id1"}, errors.New("deploy timeout err"))
		destroyArgs := &cloudstack.DestroyVirtualMachineParams{}
		destroyArgs.SetId("vm1-id1")
		destroyArgs.SetExpunge(false)
		cli.On("DestroyVirtualMachine", destroyArgs).Return(&cloudstack.DestroyVirtualMachineResponse{}, nil)
		scaler := csScaler{client: cli}

		tagArgs := cloudstack.CreateTagsParams{}
		tagArgs.SetTags(map[string]string{"a": "b"})

		err := scaler.createVM(*deployArgs, tagArgs)
		require.Equal(t, errors.New("deploy timeout err"), err)

		cli.AssertExpectations(t)
	})

	t.Run("create err with ID, error on destroy", func(t *testing.T) {
		cli := &fakeClient{}
		deployArgs := &cloudstack.DeployVirtualMachineParams{}
		deployArgs.SetDisplayname("vm1")
		cli.On("DeployVirtualMachine", deployArgs).Return(&cloudstack.DeployVirtualMachineResponse{Id: "vm1-id1"}, errors.New("deploy timeout err"))
		destroyArgs := &cloudstack.DestroyVirtualMachineParams{}
		destroyArgs.SetId("vm1-id1")
		destroyArgs.SetExpunge(false)
		cli.On("DestroyVirtualMachine", destroyArgs).Return((*cloudstack.DestroyVirtualMachineResponse)(nil), errors.New("destroy err"))
		scaler := csScaler{client: cli}

		tagArgs := cloudstack.CreateTagsParams{}
		tagArgs.SetTags(map[string]string{"a": "b"})

		err := scaler.createVM(*deployArgs, tagArgs)
		require.Equal(t, errors.New("unable to destroy cloudstack VM after error creating: destroy err - original error: deploy timeout err"), err)

		cli.AssertExpectations(t)
	})

	t.Run("rollback tag error", func(t *testing.T) {
		cli := &fakeClient{}
		cli.On("DestroyVirtualMachine", mock.Anything).Return(&cloudstack.DestroyVirtualMachineResponse{}, nil)
		cli.On("DeployVirtualMachine", mock.Anything).Return(&cloudstack.DeployVirtualMachineResponse{Id: "vm1-id1"}, nil)
		cli.On("CreateTags", mock.Anything).Return((*cloudstack.CreateTagsResponse)(nil), errors.New("my err"))
		scaler := csScaler{client: cli}
		deployArgs := cloudstack.DeployVirtualMachineParams{}
		deployArgs.SetDisplayname("vm1")
		tagArgs := cloudstack.CreateTagsParams{}
		tagArgs.SetTags(map[string]string{"a": "b"})

		err := scaler.createVM(deployArgs, tagArgs)
		require.Error(t, err)
		require.Regexp(t, `my err`, err)

		expectedTagArgs := cloudstack.CreateTagsParams{}
		expectedTagArgs.SetResourceids([]string{"vm1-id1"})
		expectedTagArgs.SetResourcetype("UserVm")
		expectedTagArgs.SetTags(map[string]string{"a": "b"})

		expectedDestroyArgs := cloudstack.DestroyVirtualMachineParams{}
		expectedDestroyArgs.SetId("vm1-id1")
		expectedDestroyArgs.SetExpunge(false)

		cli.AssertCalled(t, "DeployVirtualMachine", &deployArgs)
		cli.AssertCalled(t, "CreateTags", &expectedTagArgs)
		cli.AssertCalled(t, "DestroyVirtualMachine", &expectedDestroyArgs)
	})

	t.Run("rollback tag error, error on rollback", func(t *testing.T) {
		cli := &fakeClient{}
		cli.On("DestroyVirtualMachine", mock.Anything).Return((*cloudstack.DestroyVirtualMachineResponse)(nil), errors.New("destroy err"))
		cli.On("DeployVirtualMachine", mock.Anything).Return(&cloudstack.DeployVirtualMachineResponse{Id: "vm1-id1"}, nil)
		cli.On("CreateTags", mock.Anything).Return((*cloudstack.CreateTagsResponse)(nil), errors.New("tag err"))
		scaler := csScaler{client: cli}
		deployArgs := cloudstack.DeployVirtualMachineParams{}
		deployArgs.SetDisplayname("vm1")
		tagArgs := cloudstack.CreateTagsParams{}
		tagArgs.SetTags(map[string]string{"a": "b"})

		err := scaler.createVM(deployArgs, tagArgs)
		require.Error(t, err)
		require.Regexp(t, `unable to destroy cloudstack VM after tagging error: destroy err - original error: tag err`, err)

		expectedTagArgs := cloudstack.CreateTagsParams{}
		expectedTagArgs.SetResourceids([]string{"vm1-id1"})
		expectedTagArgs.SetResourcetype("UserVm")
		expectedTagArgs.SetTags(map[string]string{"a": "b"})

		expectedDestroyArgs := cloudstack.DestroyVirtualMachineParams{}
		expectedDestroyArgs.SetId("vm1-id1")
		expectedDestroyArgs.SetExpunge(false)

		cli.AssertCalled(t, "DeployVirtualMachine", &deployArgs)
		cli.AssertCalled(t, "CreateTags", &expectedTagArgs)
		cli.AssertCalled(t, "DestroyVirtualMachine", &expectedDestroyArgs)
	})

}

func Test_csScaler_scaleUp(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		rand.Seed(0)
		cli := &fakeClient{}
		cli.On("DeployVirtualMachine", mock.Anything).Return(&cloudstack.DeployVirtualMachineResponse{Id: "vm1-id1"}, nil).Twice()
		cli.On("CreateTags", mock.Anything).Return(&cloudstack.CreateTagsResponse{}, nil).Twice()
		scaler := csScaler{client: cli}

		vmp := vmProfile{
			asp: cloudstack.AutoScaleVmProfile{
				Id:                "asp1",
				Serviceofferingid: "so1",
				Templateid:        "tpl1",
				Zoneid:            "zo1",
				Projectid:         "proj1",
				Otherdeployparams: "networkids=net1,net2&rootdisksize=10",
			},
			aspMetadata: map[string]string{
				"nodeGroupName": "ng1",
				"label-ignored": "lbl1",
				"tag-t1":        "v1",
				"tag-t2":        "v2",
				"userdata":      "data",
			},
		}

		err := scaler.scaleUp(vmp, 2)
		require.NoError(t, err)

		cli.AssertExpectations(t)

		expectedTagArgs := cloudstack.CreateTagsParams{}
		expectedTagArgs.SetResourceids([]string{"vm1-id1"})
		expectedTagArgs.SetResourcetype("UserVm")
		expectedTagArgs.SetTags(map[string]string{"nodeGroupName": "ng1", "t1": "v1", "t2": "v2"})
		cli.AssertCalled(t, "CreateTags", &expectedTagArgs)

		deployArgs := cloudstack.DeployVirtualMachineParams{}
		deployArgs.SetName("ng1-8717895732742165505")
		deployArgs.SetServiceofferingid("so1")
		deployArgs.SetTemplateid("tpl1")
		deployArgs.SetZoneid("zo1")
		deployArgs.SetProjectid("proj1")
		deployArgs.SetNetworkids([]string{"net1", "net2"})
		deployArgs.SetRootdisksize(10)
		deployArgs.SetUserdata("data")
		cli.AssertCalled(t, "DeployVirtualMachine", &deployArgs)

		deployArgs.SetName("ng1-2259404117704393152")
		cli.AssertCalled(t, "DeployVirtualMachine", &deployArgs)
	})
}
