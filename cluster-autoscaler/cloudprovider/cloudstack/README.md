# Cluster Autoscaler for Cloudstack

The cluster autoscaler for cloudstack relies on `AutoScaleVmProfile` resources
on cloudstack to represent autoscaling node groups. On their own
`AutoScaleVmProfile` objects do not have all necessary information for
representing a node group, for this reason this provider relies on some
specific metadata associated with each `AutoScaleVmProfile` to work.

## Autodiscovery

Setting up autodiscovery is required using the format:

```
--node-group-auto-discovery "label:<metadata key>=<metadata value>"
```

This will select `AutoScaleVmProfile` instances containing the specified
metadata to be used as node groups.

## Configuration

A JSON file with the required configuration to interact with cloudstack is
required. It must be supplied to the cluster-autoscaler using the
`--cloud-config` flag.

Config | Description | Type | Required | Default
------ | ----------- | ---- | -------- | -------
api_key | API Key used to connect to Cloudstack. | string | true |
api_secret | API Secret used to connect to Cloudstack. | string | true |
url | API Secret used to connect to Cloudstack. | string | true |
insecure | Disable https security checks. | bool | false | false
use_projects | If enabled all projects will be scanned looking for `AutoScaleVmProfile` resources. | bool | false | false
project_refresh_interval | If `use_projects` is enabled, this is the refresh interval for listing projects. | duration | false | 30m
expunge_vms | If enabled the `expunge` flag will be set when destroying VMs. | bool | false | false


## Metadata in AutoScaleVmProfile resources

Metadata | Description | Required | Default
-------- | ----------- | -------- | -------
nodeGroupName | A name for the node group. | true |
minNodes | The minimum number of nodes in the node group. | true |
maxNodes | The maximum number of nodes in the node group. | true |
targetNodes | This value will be manipulated directly by the provider according to the autoscaler state. | false | 0
providerIDPrefix | A prefix for the `node.spec.providerID` field set by the cloud provider used in the cluster. | false | ""
userdata | A userdata field set on each VM created for the node group. Defaults to empty. | false | ""
tag-* | Tag values to be set on each VM created for the node group. The tag key will be stripped of the `tag-` prefix and the value will be set as is. | false | []
label-* | Label values that are expected to exist on kubernetes Nodes created by the node group. The label key will be stripped of the `label-` prefix and the value will be set as is. This is used only during the autoscaler simulation of adding new nodes. | false | []

## Example

1. Create a AutoScaleVmProfile:
```
$ cloudmonkey create autoscalevmprofile \
    serviceofferingid=82e3c3d7-6a4a-44f4-b5dd-2a3c3004d3f2 \
    templateid=7151800f-bdaa-419f-b02a-fa973376b083 \
    zoneid=6bfa8876-c138-4a44-b329-74316d8e0384 \
    otherdeployparams='projectid=9532821d-7476-4069-9c77-cabdc21c4c01&displayname=auto-dev-test&networkids=518ee247-accc-4ab7-9c4b-325a995d6185'
```

2. Associate required metadata to AutoScaleVmProfile
```
$ cloudmonkey add resourcedetail resourceid=<autoscalevmprofile id> resourcetype=AutoScaleVmProfile \
    details[0].key=isNodeGroup details[0].value=true \
    details[1].key=minNodes    details[1].value=0 \
    details[2].key=maxNodes    details[2].value=10
```

3. Start cluster autoscaler
```
$ cluster-autoscaler --cloud-provider cloudstack --cloud-config config.json \
    --node-group-auto-discovery "label:isNodeGroup=true"
```

## Why not use `AutoScaleVmGroup` resources?

- A load balancer resource is mandatory to create a AutoScaleVmGroup, this
  requirement doesn't make sense when using it for kubernetes nodes.

- Scale up and scale down policies, associated to conditions and counters, are
  also required when creating a AutoScaleVmGroup. They also don't make sense
  when representing node groups for the cluster autoscaler.

