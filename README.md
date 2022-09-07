# Apogee ESXi Integration 

### Basic Operations
1. Instantiate Client
1. Login Client
1. Logout Client
1. Get HostSystem using Api Wrapper
```go
esxClient := apgjb.NewJumpbox("esx host/ip", "user", "password")
if err := esxClient.Login(); err != nil {
    // Do something besides moving beyond this line
}
defer esxClient.Logout()
// Connecting to a Single ESXi Host
var host mo.HostSystem
hosts, err := esxClient.GetHosts()
if err != nil || len(hosts) != 1 {
    // Do Something
}
host = host[0]
// Get HostNetworkSystem Reference
hostNetSysRef := host.ConfigManager.NetworkSystem.Reference()
```

### AddPG (add PortGroup)
1. Get HostNetworkSystemReference (host.ConfigManager.NetworkSystem.Reference())
1. Create AddPgParams struct
```go
params := apgjb.AddPgParams{
    HostNetSystemRef: ref,
    PgName:           "Typically VlanID Specific",
    PgVlanId:         int(vlanId),
    VswitchName:      "vSwitchToApplyPGTo",
}
```
3. Use Params to call appropriate Method
```go
err := esxApi.AddPG(params)
```

### AddVswitch with Physical NIC
1. Get HostNetworkSystemReference (again)
1. Create VswitchPostParams and Call VswitchPost Method
```go
params := apgjb.VswitchPostParams{
    // From Step 1
    HostNetSystemRef: ref,
    Vswitch: apgjb.VswitchOp{
        // Add and/or Modify at the End (ENUM)
        ChangeOp: types.HostConfigChangeOperationAdd,
        Specs: &types.HostVirtualSwitchSpec{
            // 1024 is the Max (typically Default?)
            NumPorts: 1024,
            // Bind PNIC to VSwitch
            Bridge: &types.HostVirtualSwitchBondBridge{
                NicDevice: []string{"vmnic1"},
            },
        },
    },
    // Replace or Modify at the end (ENUM)
    ChangeMode: types.HostConfigChangeModeModify,
}
err := esxApi.VswitchPost(params)
```