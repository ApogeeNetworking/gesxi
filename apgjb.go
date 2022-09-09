package apgjb

import (
	"context"
	"fmt"
	"os"

	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// Jumpbox ...
type Jumpbox struct {
	EsxHostIp  string
	EsxiClient *esxClient
	ctx        context.Context
}

// NewJumpbox ...
func NewJumpbox(host, user, pass string) *Jumpbox {
	ctx := context.Background()
	uri := fmt.Sprintf("https://%s/sdk", host)
	client := newEsxClient(ctx, uri, user, pass)

	return &Jumpbox{ctx: ctx, EsxiClient: client, EsxHostIp: host}
}

func (j *Jumpbox) Login() error {
	return j.EsxiClient.Login(j.ctx, j.EsxiClient.Userinfo)
}

func (j *Jumpbox) Logout() error {
	return j.EsxiClient.Logout(j.ctx)
}

func (j *Jumpbox) getView(v string) (*view.ContainerView, error) {
	m := view.NewManager(j.EsxiClient.Client)
	return m.CreateContainerView(
		j.ctx,
		j.EsxiClient.ServiceContent.RootFolder,
		[]string{v},
		true,
	)
}

func (j *Jumpbox) GetHosts() ([]mo.HostSystem, error) {
	v, err := j.getView("HostSystem")
	if err != nil {
		return nil, err
	}
	defer v.Destroy(j.ctx)
	var hosts []mo.HostSystem
	err = v.Retrieve(j.ctx, []string{"HostSystem"}, nil, &hosts)
	if err != nil {
		return nil, err
	}
	return hosts, nil
}

func (j *Jumpbox) GetDatacenter() (mo.Datacenter, error) {
	v, err := j.getView("Datacenter")
	if err != nil {
		return mo.Datacenter{}, err
	}
	defer v.Destroy(j.ctx)
	var dc mo.Datacenter
	err = v.Retrieve(j.ctx, []string{"Datacenter"}, nil, &dc)
	if err != nil {
		return dc, err
	}
	return dc, nil
}

func (j *Jumpbox) GetDatastore() (mo.Datastore, error) {
	v, err := j.getView("Datastore")
	if err != nil {
		return mo.Datastore{}, err
	}
	defer v.Destroy(j.ctx)
	var dss mo.Datastore
	err = v.Retrieve(j.ctx, []string{"Datastore"}, nil, &dss)
	if err != nil {
		return mo.Datastore{}, err
	}
	return dss, nil
}

func (j *Jumpbox) GetRsrcPool() (mo.ResourcePool, error) {
	var rsrcPool mo.ResourcePool
	view, err := j.getView("ResourcePool")
	if err != nil {
		return rsrcPool, err
	}
	defer view.Destroy(j.ctx)
	if err = view.Retrieve(j.ctx, []string{"ResourcePool"}, nil, &rsrcPool); err != nil {
		return rsrcPool, err
	}
	return rsrcPool, nil
}

type MkDirParams struct {
	PathName string
	DcRef    *types.ManagedObjectReference
}

func (j *Jumpbox) MkDir(p MkDirParams) error {
	_, err := methods.MakeDirectory(j.ctx, j.EsxiClient.Client, &types.MakeDirectory{
		This:       j.EsxiClient.ServiceContent.FileManager.Reference(),
		Name:       fmt.Sprintf("[datastore1] %s", p.PathName),
		Datacenter: p.DcRef,
	})
	if err != nil {
		return err
	}
	return nil
}

type CpFileParams struct {
	// Datacenter Name
	DcName string
	// Datastore Name
	DsName string
	// File Path
	LocalFilePath string
	// File Name
	FileName string
	// Remote Dir (Datastore Folder)
	DatastoreDir string
}

func (j *Jumpbox) CpFileToDatastore(p CpFileParams) error {
	file, err := os.Open(fmt.Sprintf("%s/%s", p.LocalFilePath, p.FileName))
	if err != nil {
		return err
	}
	httpClient := newHttpService(j.EsxHostIp, &j.EsxiClient.Jar)
	url := fmt.Sprintf("%s/%s/%s", httpClient.BaseURL, p.DatastoreDir, p.FileName)

	req, err := httpClient.GenerateRequest("PUT", url, file)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	q := req.URL.Query()
	q.Add("dsName", p.DsName)
	q.Add("dcPath", p.DcName)
	req.URL.RawQuery = q.Encode()
	res, err := httpClient.MakeRequest(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return nil
}

// GetVmsWithTickets ...
func (j *Jumpbox) GetVmsWithTickets() []ApgVM {
	v, _ := j.getView("VirtualMachine")
	defer v.Destroy(j.ctx)
	var vmMos []mo.VirtualMachine
	v.Retrieve(j.ctx, []string{"VirtualMachine"}, []string{"summary"}, &vmMos)

	var vms []ApgVM

	for _, vmMo := range vmMos {
		vm := object.NewVirtualMachine(j.EsxiClient.Client, vmMo.Reference())
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

func (j *Jumpbox) GetVms() ([]mo.VirtualMachine, error) {
	view, err := j.getView("VirtualMachine")
	if err != nil {
		return nil, err
	}
	defer view.Destroy(j.ctx)
	var vms []mo.VirtualMachine
	if err = view.Retrieve(j.ctx, []string{"VirtualMachine"}, nil, &vms); err != nil {
		return nil, err
	}
	return vms, nil
}

func (j *Jumpbox) GetVmByUuid(uuid string) (mo.VirtualMachine, error) {
	var vm mo.VirtualMachine
	searchIdx := j.EsxiClient.ServiceContent.SearchIndex
	resp, err := methods.FindByUuid(j.ctx, j.EsxiClient.Client, &types.FindByUuid{
		This:     *searchIdx,
		Uuid:     uuid,
		VmSearch: true,
	})
	if err != nil {
		return vm, err
	}
	view, err := j.getView("VirtualMachine")
	if err != nil {
		return vm, err
	}
	defer view.Destroy(j.ctx)
	if err = view.Properties(j.ctx, *resp.Returnval, nil, &vm); err != nil {
		return vm, err
	}
	return vm, nil
}

func (j *Jumpbox) CreateVm() error {
	rsrcPool, err := j.GetRsrcPool()
	if err != nil {
		return err
	}
	ds, err := j.GetDatastore()
	if err != nil {
		return err
	}
	dsName := ds.Name
	dc, _ := j.GetDatacenter()
	vmCfgSpec := types.VirtualMachineConfigSpec{
		Annotation: "sample",
		MemoryMB:   16000,
		Name:       "MyVm",
		NumCPUs:    4,
		Files: &types.VirtualMachineFileInfo{
			VmPathName: fmt.Sprintf("[%s]", dsName),
		},
	}
	_, err = methods.CreateVM_Task(j.ctx, j.EsxiClient.Client, &types.CreateVM_Task{
		This:   dc.VmFolder,
		Config: vmCfgSpec,
		Pool:   rsrcPool.Reference(),
	})
	if err != nil {
		return err
	}
	return nil
}

// Create VM
// Add Disk to VM
// Add Nic to VM
// Add CDROM and Mount ISO to It
// PowerOn Vm

func (j *Jumpbox) GetNetworks() {
	v, _ := j.getView("Network")
	defer v.Destroy(j.ctx)
	var networks []mo.Network
	v.Retrieve(j.ctx, []string{"Network"}, nil, &networks)
	for _, n := range networks {
		fmt.Println(n.Name)
	}
}

type AddPgParams struct {
	// A Reference to the HostNetworkSystem
	// host.ConfigManager.NetworkSystem.Reference()
	HostNetSystemRef types.ManagedObjectReference
	PgName           string
	PgVlanId         int
	VswitchName      string
	Security         NetSec
}

type NetSec struct {
	AllowPromiscuous bool
	AllowMacChange   bool
	ForgedXmits      bool
}

// AddPG adds a PortGroup to an Existing vSwitch
func (j *Jumpbox) AddPG(p AddPgParams) error {
	policy := types.HostNetworkPolicy{}
	if p.Security.AllowPromiscuous {
		*policy.Security.AllowPromiscuous = p.Security.AllowPromiscuous
	}
	if p.Security.AllowMacChange {
		*policy.Security.MacChanges = p.Security.AllowMacChange
	}
	if p.Security.ForgedXmits {
		*policy.Security.ForgedTransmits = p.Security.ForgedXmits
	}
	_, err := methods.AddPortGroup(j.ctx, j.EsxiClient.Client, &types.AddPortGroup{
		This: p.HostNetSystemRef,
		Portgrp: types.HostPortGroupSpec{
			Name:        p.PgName,
			VlanId:      int32(p.PgVlanId),
			VswitchName: p.VswitchName,
			Policy:      policy,
		},
	})
	if err != nil {
		return err
	}
	return nil
}

type VswitchOp struct {
	Name     string
	ChangeOp types.HostConfigChangeOperation
	Specs    *types.HostVirtualSwitchSpec
}

type VswitchPostParams struct {
	HostNetSystemRef types.ManagedObjectReference
	Vswitch          VswitchOp
	ChangMode        types.HostConfigChangeMode
}

func (j *Jumpbox) VswitchPost(p VswitchPostParams) error {
	_, err := methods.UpdateNetworkConfig(j.ctx, j.EsxiClient.Client, &types.UpdateNetworkConfig{
		This: p.HostNetSystemRef,
		Config: types.HostNetworkConfig{
			Vswitch: []types.HostVirtualSwitchConfig{{
				ChangeOperation: string(p.Vswitch.ChangeOp),
				Name:            p.Vswitch.Name,
				Spec:            p.Vswitch.Specs,
			}},
		},
		ChangeMode: string(p.ChangMode),
	})
	if err != nil {
		return err
	}
	return nil
}
