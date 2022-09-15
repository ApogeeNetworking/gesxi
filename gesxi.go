package gesxi

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	return j.getVmByMo(*resp.Returnval)
}

func (j *Jumpbox) getVmByMo(moRef types.ManagedObjectReference) (mo.VirtualMachine, error) {
	var vm mo.VirtualMachine
	view, err := j.getView("VirtualMachine")
	if err != nil {
		return vm, err
	}
	defer view.Destroy(j.ctx)
	if err = view.Properties(j.ctx, moRef, nil, &vm); err != nil {
		return vm, err
	}
	return vm, nil
}

type CreateVmParams struct {
	Name     string
	NumCpus  int32
	MemoryMB int64
	// VM Notes
	Annotation    string
	DatastoreName string
	DcVmFolder    types.ManagedObjectReference
	RsrcPool      types.ManagedObjectReference
}

func (j *Jumpbox) CreateVm(p CreateVmParams) (mo.VirtualMachine, error) {
	var vm mo.VirtualMachine
	vmCfgSpec := types.VirtualMachineConfigSpec{
		Annotation: p.Annotation,
		MemoryMB:   p.MemoryMB,
		Name:       p.Name,
		NumCPUs:    p.NumCpus,
		Files: &types.VirtualMachineFileInfo{
			VmPathName: fmt.Sprintf("[%s]", p.DatastoreName),
		},
	}
	_, err := methods.CreateVM_Task(j.ctx, j.EsxiClient.Client, &types.CreateVM_Task{
		This:   p.DcVmFolder,
		Config: vmCfgSpec,
		Pool:   p.RsrcPool,
	})
	if err != nil {
		return vm, err
	}
	time.Sleep(500 * time.Millisecond)
	vms, err := j.GetVms()
	if err != nil {
		return vm, err
	}
	for _, v := range vms {
		if v.Name == p.Name {
			vm = v
			break
		}
	}
	return vm, nil
}

func (j *Jumpbox) AddDiskToVm(vm mo.VirtualMachine) error {
	var spec types.VirtualMachineConfigSpec = types.VirtualMachineConfigSpec{
		MemoryMB: vm.Config.ToConfigSpec().MemoryMB,
		NumCPUs:  vm.Config.ToConfigSpec().NumCPUs,
	}
	var (
		thinProv      bool  = true
		eagarScrub    bool  = false
		unitNum       int32 = 1
		controllerKey int32
	)
	for _, dev := range vm.Config.Hardware.Device {
		device := types.VirtualDevice(*dev.GetVirtualDevice())
		if device.DeviceInfo.GetDescription().Label == "IDE 0" {
			controllerKey = device.Key
		}
	}
	virtDisk := &types.VirtualDisk{
		VirtualDevice: types.VirtualDevice{
			Backing: &types.VirtualDiskFlatVer2BackingInfo{
				DiskMode:        "independent_persistent",
				ThinProvisioned: &thinProv,
				EagerlyScrub:    &eagarScrub,
				Sharing:         "sharingNone",
				VirtualDeviceFileBackingInfo: types.VirtualDeviceFileBackingInfo{
					FileName:  fmt.Sprintf("[%s] %s", vm.Config.DatastoreUrl[0].Name, vm.Name),
					Datastore: &vm.Datastore[0],
				},
			},
			UnitNumber:    &unitNum,
			ControllerKey: controllerKey,
			Key:           -1000000,
		},
		CapacityInKB: int64(80000),
	}
	diskSpec := types.VirtualDeviceConfigSpec{
		FileOperation: types.VirtualDeviceConfigSpecFileOperationCreate,
		Operation:     types.VirtualDeviceConfigSpecOperationAdd,
		Device:        virtDisk,
	}
	spec.DeviceChange = append(spec.DeviceChange, types.BaseVirtualDeviceConfigSpec(&diskSpec))
	_, err := methods.ReconfigVM_Task(j.ctx, j.EsxiClient.Client, &types.ReconfigVM_Task{
		This: vm.Reference(),
		Spec: spec,
	})
	time.Sleep(500 * time.Millisecond)
	if err != nil {
		return err
	}
	return nil
}

func (j *Jumpbox) AddNicToVm(vm mo.VirtualMachine, netName string) error {
	var (
		wakeOnLan      bool = true
		useAutoDectect bool = false
		netMo          types.ManagedObjectReference
		network        mo.Network
		inPassThruMode bool = false
	)
	var spec types.VirtualMachineConfigSpec = types.VirtualMachineConfigSpec{
		MemoryMB: vm.Config.ToConfigSpec().MemoryMB,
		NumCPUs:  vm.Config.ToConfigSpec().NumCPUs,
	}

	device := types.VirtualE1000{
		VirtualEthernetCard: types.VirtualEthernetCard{
			VirtualDevice: types.VirtualDevice{
				Key: 100,
				DeviceInfo: &types.Description{
					Summary: "vSphere API Test",
				},
				Connectable: &types.VirtualDeviceConnectInfo{
					StartConnected:    true,
					AllowGuestControl: true,
					Connected:         false,
					Status:            "untried",
				},
			},
			AddressType:      "generated",
			WakeOnLanEnabled: &wakeOnLan,
		},
	}
	networks, err := j.GetNetworks()
	for _, net := range networks {
		if net.Name == netName {
			network = net
			netMo = net.Reference()
		}
	}
	if err != nil {
		return err
	}
	switch {
	case netMo.Type == "Network":
		device.VirtualEthernetCard.VirtualDevice.Backing = &types.VirtualEthernetCardNetworkBackingInfo{
			VirtualDeviceDeviceBackingInfo: types.VirtualDeviceDeviceBackingInfo{
				UseAutoDetect: &useAutoDectect,
				DeviceName:    network.Name,
			},
			Network:           &network.Self,
			InPassthroughMode: &inPassThruMode,
		}
	default:
		device.VirtualEthernetCard.VirtualDevice.Backing = types.BaseVirtualDeviceBackingInfo(
			&types.VirtualEthernetCardOpaqueNetworkBackingInfo{
				OpaqueNetworkType: network.Summary.GetNetworkSummary().Network.Type,
				OpaqueNetworkId:   network.Summary.GetNetworkSummary().Name,
			},
		)
	}
	nicSpec := types.VirtualDeviceConfigSpec{
		Operation: types.VirtualDeviceConfigSpecOperationAdd,
		Device:    &device,
	}
	spec.DeviceChange = append(spec.DeviceChange, &nicSpec)
	rcfgVm := &types.ReconfigVM_Task{
		This: vm.Reference(),
		Spec: spec,
	}
	_, err = methods.ReconfigVM_Task(j.ctx, j.EsxiClient.Client, rcfgVm)
	if err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	return nil
}

func (j *Jumpbox) Power(action, appType string, moRef types.ManagedObjectReference) (*types.ManagedObjectReference, error) {
	switch appType {
	case "vm":
		if action == "on" {
			pwrOnTask, err := methods.PowerOnVM_Task(j.ctx, j.EsxiClient.Client, &types.PowerOnVM_Task{
				This: moRef,
			})
			return &pwrOnTask.Returnval, err
		}
		pwrOffTask, err := methods.PowerOffVM_Task(j.ctx, j.EsxiClient.Client, &types.PowerOffVM_Task{
			This: moRef,
		})
		return &pwrOffTask.Returnval, err
	case "vapp":
		if action == "on" {
			pwrOnTask, err := methods.PowerOnVApp_Task(j.ctx, j.EsxiClient.Client, &types.PowerOnVApp_Task{
				This: moRef,
			})
			return &pwrOnTask.Returnval, err
		}
		pwrOffTask, err := methods.PowerOffVApp_Task(j.ctx, j.EsxiClient.Client, &types.PowerOffVApp_Task{
			This: moRef,
		})
		return &pwrOffTask.Returnval, err
	}
	return nil, nil
}

// Create VM
// Add Disk to VM
// Add Nic to VM
// Add CDROM and Mount ISO to It
// PowerOn Vm

func (j *Jumpbox) GetNetworks() ([]mo.Network, error) {
	var networks []mo.Network
	v, err := j.getView("Network")
	if err != nil {
		return networks, err
	}
	defer v.Destroy(j.ctx)
	if err = v.Retrieve(j.ctx, []string{"Network"}, nil, &networks); err != nil {
		return networks, err
	}
	return networks, nil
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

type HandleImportVAppParams struct {
	Ova        OvaInfo
	DcVmFolder types.ManagedObjectReference
	HostSystem types.ManagedObjectReference
	Datastore  types.ManagedObjectReference
	RsrcPool   types.ManagedObjectReference
	NetSys     []mo.Network
	Vm         struct {
		Name             string
		MemoryMB         int64
		NumCpus          int32
		PgNames          []string
		DiskProvisioning string
	}
}

type OvaInfo struct {
	Ovf struct {
		FileName string
		Data     string
	}
	Dir   string
	Disks []string
}

func (j *Jumpbox) HandleOvaExtract(dir, filename string) (OvaInfo, error) {
	ovaInfo, err := j.extractOva(dir, filename)
	if err != nil {
		return ovaInfo, err
	}
	ovaInfo.Dir = dir
	return ovaInfo, nil
}

func (j *Jumpbox) ImportVApp(p HandleImportVAppParams) (types.ManagedObjectReference, error) {
	var mo types.ManagedObjectReference
	// Set OvfNetworkMapping according to PortGroup Names to Add for VM Networking
	var networkMapping []types.OvfNetworkMapping
	for _, net := range p.NetSys {
		for _, pg := range p.Vm.PgNames {
			if net.Name == pg {
				networkMapping = append(networkMapping, types.OvfNetworkMapping{
					Network: net.Reference(),
				})
			}
		}
	}
	cisp := types.OvfCreateImportSpecParams{
		OvfManagerCommonParams: types.OvfManagerCommonParams{
			Locale: "US",
		},
		EntityName:       p.Vm.Name,
		HostSystem:       &p.HostSystem,
		NetworkMapping:   networkMapping,
		DiskProvisioning: p.Vm.DiskProvisioning,
	}
	ovfMo := j.EsxiClient.ServiceContent.OvfManager
	cisr, err := methods.CreateImportSpec(j.ctx, j.EsxiClient.Client, &types.CreateImportSpec{
		This:          *ovfMo,
		OvfDescriptor: p.Ova.Ovf.Data,
		ResourcePool:  p.RsrcPool,
		Datastore:     p.Datastore,
		Cisp:          cisp,
	})
	if err != nil {
		return mo, err
	}
	resp, err := methods.ImportVApp(j.ctx, j.EsxiClient.Client, &types.ImportVApp{
		This:   p.RsrcPool,
		Spec:   cisr.Returnval.ImportSpec,
		Folder: &p.DcVmFolder,
		Host:   &p.HostSystem,
	})
	if err != nil {
		fmt.Println("failed here")
		return mo, err
	}
	time.Sleep(1 * time.Second)
	return resp.Returnval, nil
}

func (j *Jumpbox) HandleVmdkTransfer(uri, dir string, disks []string, lease *mo.HttpNfcLease, p HandleImportVAppParams) error {
	url := strings.Replace(uri, "*", j.EsxHostIp, -1)
	for _, disk := range disks {
		path := fmt.Sprintf("./%s/%s", dir, disk)
		payload, err := os.Open(path)
		if err != nil {
			fmt.Println("error with disk")
			return err
		}
		requestor := newHttpService(j.EsxHostIp, &j.EsxiClient.Jar)
		req, err := requestor.GenerateRequest("POST", url, payload)
		if err != nil {
			fmt.Println("error generating request")
			return err
		}
		req.Header.Add("Content-Type", "application/x-vnd.vmware-streamVmdk")
		resp, err := requestor.MakeRequest(req)
		if err != nil {
			fmt.Println("error in resp")
			return err
		}
		defer resp.Body.Close()
		d, _ := ioutil.ReadAll(resp.Body)
		respText := string(d)
		if strings.Contains(respText, "Cannot POST") {
			dc, _ := j.GetDatacenter()
			ds, _ := j.GetDatastore()
			j.CpFileToDatastore(CpFileParams{
				DcName:        dc.Name,
				DsName:        ds.Name,
				LocalFilePath: dir,
				FileName:      disk,
				DatastoreDir:  fmt.Sprintf("/%s", p.Vm.Name),
			})
		}
	}
	// Close the Lease for the VAppImport
	_, err := methods.HttpNfcLeaseComplete(j.ctx, j.EsxiClient.Client, &types.HttpNfcLeaseComplete{
		This: lease.Self,
	})
	if err != nil {
		return err
	}
	return nil
}

func (j *Jumpbox) HandleLease(moRef types.ManagedObjectReference) (mo.HttpNfcLease, error) {
	var lease mo.HttpNfcLease
	m := view.NewManager(j.EsxiClient.Client)
	err := m.Properties(j.ctx, moRef, nil, &lease)
	if err != nil {
		return lease, err
	}
	leaseState := string(lease.State)
LeaseState:
	for {
		switch leaseState {
		case string(types.HttpNfcLeaseStateInitializing):
			time.Sleep(2 * time.Second)
		case string(types.HttpNfcLeaseStateError):
			err = fmt.Errorf("lease error: %s", string(types.HttpNfcLeaseStateError))
			break LeaseState
		case string(types.HttpNfcLeaseStateReady):
			break LeaseState
		}
		lease, err := j.getLease(moRef)
		if err != nil {
			break
		}
		leaseState = string(lease.State)
	}
	if err != nil {
		return lease, err
	}
	return lease, nil
}

func (j *Jumpbox) getLease(leaseMo types.ManagedObjectReference) (mo.HttpNfcLease, error) {
	var lease mo.HttpNfcLease
	manager := view.NewManager(j.EsxiClient.Client)
	err := manager.Properties(j.ctx, leaseMo, nil, &lease)
	if err != nil {
		fmt.Println("error getting HttpNfcLease Properties")
		return lease, err
	}
	return lease, nil
}

func (j *Jumpbox) extractOva(path, filename string) (OvaInfo, error) {
	var ovaInfo OvaInfo
	f, err := os.Open(fmt.Sprintf("./%s/%s", path, filename))
	if err != nil {
		return ovaInfo, err
	}
	defer f.Close()
	var fr io.ReadCloser = f
	tr := tar.NewReader(fr)
OuterLoop:
	for {
		header, err := tr.Next()
		switch {
		case err == io.EOF:
			break OuterLoop
			// return ovfInfo, nil
		case err != nil:
			break OuterLoop
			// return ovfInfo, err
		case header == nil:
			continue
		}
		target := filepath.Join(fmt.Sprintf("./%s", path), header.Name)
		switch header.Typeflag {
		case tar.TypeDir:
			if _, err := os.Stat(target); err != nil {
				if err := os.Mkdir(target, 0o755); err != nil {
					break
				}
			}
		case tar.TypeReg:
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				break
			}
			defer f.Close()
			if _, err := io.Copy(f, tr); err != nil {
				break OuterLoop
			}
		}
	}
	if err != nil {
		return ovaInfo, err
	}
	dirEntries, err := os.ReadDir("./" + path)
	if err != nil {
		return ovaInfo, err
	}
	for _, entry := range dirEntries {
		switch {
		case strings.Contains(entry.Name(), ".ovf"):
			ovaInfo.Ovf.FileName = entry.Name()
			ovfFile, _ := os.Open(fmt.Sprintf("./%s/%s", path, entry.Name()))
			defer ovfFile.Close()
			d, _ := ioutil.ReadAll(ovfFile)
			ovaInfo.Ovf.Data = string(d)
		case strings.Contains(entry.Name(), ".vmdk") || strings.Contains(entry.Name(), ".iso"):
			ovaInfo.Disks = append(ovaInfo.Disks, entry.Name())
		}
	}
	ovaInfo.Dir = path
	return ovaInfo, nil
}
