package apgjb

import (
	"context"
	"fmt"

	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
)

// Jumpbox ...
type Jumpbox struct {
	esxiClient *esxClient
	ctx        context.Context
}

// NewJumpbox ...
func NewJumpbox(host, user, pass string) *Jumpbox {
	ctx := context.Background()
	uri := fmt.Sprintf("https://%s/sdk", host)
	return &Jumpbox{
		esxiClient: newEsxClient(ctx, uri, user, pass),
		ctx:        ctx,
	}
}

// GetVmsWithTickets ...
func (j *Jumpbox) GetVmsWithTickets() []ApgVM {
	defer j.esxiClient.Logout(j.ctx)
	vman := view.NewManager(j.esxiClient.Client)

	cview, _ := vman.CreateContainerView(
		j.ctx,
		j.esxiClient.ServiceContent.RootFolder,
		[]string{"VirtualMachine"},
		true,
	)
	var vmMos []mo.VirtualMachine
	cview.Retrieve(j.ctx, []string{"VirtualMachine"}, []string{"summary"}, &vmMos)

	var vms []ApgVM

	for _, vmMo := range vmMos {
		vm := object.NewVirtualMachine(j.esxiClient.Client, vmMo.Reference())
		vmTicket, _ := vm.AcquireTicket(j.ctx, "mks")
		apgVM := ApgVM{
			UUID:         vmMo.Summary.Config.Uuid,
			InstanceUUID: vmMo.Summary.Config.InstanceUuid,
			Name:         vmMo.Summary.Config.Name,
			Memory:       vmMo.Summary.Config.MemorySizeMB,
			NumberOfCPUs: vmMo.Summary.Config.NumCpu,
		}
		apgVM.TicketInfo.ID = vmTicket.Ticket
		apgVM.TicketInfo.Port = vmTicket.Port
		apgVM.TicketInfo.CfgFile = vmTicket.CfgFile
		apgVM.TicketInfo.SSLThumbprint = vmTicket.SslThumbprint
		vms = append(vms, apgVM)
	}
	return vms
}
