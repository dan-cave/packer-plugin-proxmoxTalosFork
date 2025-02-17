// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package proxmoxclone

import (
	proxmoxapi "github.com/Telmate/proxmox-api-go/proxmox"
	"github.com/hashicorp/hcl/v2/hcldec"
	proxmox "github.com/dan-cave/packer-plugin-proxmoxTalosFork/builder/proxmox/common"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"

	"context"
	"fmt"
)

// The unique id for the builder
const BuilderID = "proxmox.clone"

type Builder struct {
	config Config
}

// Builder implements packersdk.Builder
var _ packersdk.Builder = &Builder{}

func (b *Builder) ConfigSpec() hcldec.ObjectSpec { return b.config.FlatMapstructure().HCL2Spec() }

func (b *Builder) Prepare(raws ...interface{}) ([]string, []string, error) {
	return b.config.Prepare(raws...)
}

func (b *Builder) Run(ctx context.Context, ui packersdk.Ui, hook packersdk.Hook) (packersdk.Artifact, error) {
	state := new(multistep.BasicStateBag)
	state.Put("clone-config", &b.config)

	preSteps := []multistep.Step{
		&StepSshKeyPair{
			Debug:        b.config.PackerDebug,
			DebugKeyPath: fmt.Sprintf("%s.pem", b.config.PackerBuildName),
		},
	}
	postSteps := []multistep.Step{}

	sb := proxmox.NewSharedBuilder(BuilderID, b.config.Config, preSteps, postSteps, &cloneVMCreator{})
	return sb.Run(ctx, ui, hook, state)
}

type cloneVMCreator struct{}

func (*cloneVMCreator) Create(vmRef *proxmoxapi.VmRef, config proxmoxapi.ConfigQemu, state multistep.StateBag) error {
	client := state.Get("proxmoxClient").(*proxmoxapi.Client)
	c := state.Get("clone-config").(*Config)
	comm := state.Get("config").(*proxmox.Config).Comm

	fullClone := 1
	if c.FullClone.False() {
		fullClone = 0
	}
	config.FullClone = &fullClone

	// cloud-init options
	config.CIuser = comm.SSHUsername
	config.Sshkeys = string(comm.SSHPublicKey)
	config.Nameserver = c.Nameserver
	config.Searchdomain = c.Searchdomain
	IpconfigMap := make(map[int]interface{})
	for idx := range c.Ipconfigs {
		if c.Ipconfigs[idx] != (cloudInitIpconfig{}) {
			IpconfigMap[idx] = c.Ipconfigs[idx].String()
		}
	}
	config.Ipconfig = IpconfigMap

	var sourceVmr *proxmoxapi.VmRef
	if c.CloneVM != "" {
		sourceVmrs, err := client.GetVmRefsByName(c.CloneVM)
		if err != nil {
			return err
		}

		// prefer source Vm located on same node
		sourceVmr = sourceVmrs[0]
		for _, candVmr := range sourceVmrs {
			if candVmr.Node() == vmRef.Node() {
				sourceVmr = candVmr
			}
		}
	} else if c.CloneVMID != 0 {
		sourceVmr = proxmoxapi.NewVmRef(c.CloneVMID)
		err := client.CheckVmRef(sourceVmr)
		if err != nil {
			return err
		}
	}

	err := config.CloneVm(sourceVmr, vmRef, client)
	if err != nil {
		return err
	}
	err = config.UpdateConfig(vmRef, client)
	if err != nil {
		return err
	}
	return nil
}
