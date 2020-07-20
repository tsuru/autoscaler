package cloudstack

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/xanzy/go-cloudstack/v2/cloudstack"
)

type vmProfile struct {
	asp         cloudstack.AutoScaleVmProfile
	aspMetadata map[string]string
	offering    cloudstack.ServiceOffering
	zone        cloudstack.Zone
}

func (p *vmProfile) Id() string {
	if p.aspMetadata == nil {
		return ""
	}
	return p.aspMetadata[autoScaleProfileMetadataName]
}

func (p *vmProfile) maxSize() int {
	if p.aspMetadata == nil {
		return 0
	}
	max, _ := strconv.Atoi(p.aspMetadata[autoScaleProfileMetadataMax])
	return max
}

func (p *vmProfile) minSize() int {
	if p.aspMetadata == nil {
		return 0
	}
	min, _ := strconv.Atoi(p.aspMetadata[autoScaleProfileMetadataMin])
	return min
}

func (p *vmProfile) userdata() (string, bool) {
	if p.aspMetadata == nil {
		return "", false
	}
	v, ok := p.aspMetadata[autoScaleProfileMetadataUserdata]
	return v, ok
}

func (p *vmProfile) providerIDPrefix() string {
	if p.aspMetadata == nil {
		return ""
	}
	return p.aspMetadata[autoScaleProfileMetadataProviderIDPrefix]
}

func (p *vmProfile) projectID() string {
	if p.asp.Projectid != "" {
		return p.asp.Projectid
	}
	// Some cloudstack distributions won't allow creating an AutoScaleProfile
	// with a projectID. This is why we fallback to reading the projectID from
	// the OtherDeployParams field.
	if values, err := url.ParseQuery(p.asp.Otherdeployparams); err == nil {
		return values.Get("projectid")
	}
	return ""
}

func (p *vmProfile) rootDiskSize() int64 {
	if values, err := url.ParseQuery(p.asp.Otherdeployparams); err == nil {
		raw := values.Get("rootdisksize")
		size, _ := strconv.ParseInt(raw, 10, 64)
		return size
	}
	return 0
}

func (p *vmProfile) tags() map[string]string {
	return p.toMap(autoScaleProfileMetadataVMTagPrefix)
}

func (p *vmProfile) labels() map[string]string {
	return p.toMap(autoScaleProfileMetadataNodeLabelPrefix)
}

func (p *vmProfile) toMap(prefix string) map[string]string {
	m := map[string]string{}
	for key, value := range p.aspMetadata {
		if strings.HasPrefix(key, prefix) {
			m[strings.TrimPrefix(key, prefix)] = value
		}
	}
	return m
}
