package apgjb

// ApgVM ...
type ApgVM struct {
	UUID         string `json:"uuid"`
	InstanceUUID string `json:"instanceUuid"`
	Name         string `json:"vmName"`
	Memory       int32  `json:"memory"`
	NumberOfCPUs int32  `json:"numberOfCpus"`
	TicketInfo   struct {
		ID            string `json:"id"`
		CfgFile       string `json:"cfgFile"`
		Port          int32  `json:"port"`
		SSLThumbprint string `json:"sslThumbPrint"`
	} `json:"ticket"`
}
