package applications

import (
	"github.com/google/uuid"
)

type SystemInfo struct {
	CurrentTime             Time      `json:"current_time"`
	StartTime               Time      `json:"start_time"`
	Pid                     int       `json:"pid"`
	UUID                    uuid.UUID `json:"uuid"`
	CtlInterfacePath        string    `json:"ctl_interface_path"`
	CommandLine             string    `json:"command_line"`
	UtsNodename             string    `json:"uts.nodename"`
	UtsSysname              string    `json:"uts.sysname"`
	UtsRelease              string    `json:"uts.release"`
	UtsVersion              string    `json:"uts.version"`
	UtsMachine              string    `json:"uts.machine"`
	RusageUserCPUTimeUsed   float64   `json:"rusage.user_cpu_time_used" type:"gauge" metric:"SYSINFO_user_cpu_time"`
	RusageSystemCPUTimeUsed float64   `json:"rusage.system_cpu_time_used" type:"gauge" metric:"SYSINFO_sys_cpu_time"`
	RusageMaxRss            int       `json:"rusage.max_rss" type:"counter" metric:"SYSINFO_max_rss"`
	RusageMinFault          int       `json:"rusage.min_fault" type:"counter" metric:"SYSINFO_min_fault"`
	RusageMajFault          int       `json:"rusage.maj_fault" type:"counter" metric:"SYSINFO_maj_fault"`
	RusageInBlock           int       `json:"rusage.in_block" type:"counter" metric:"SYSINFO_in_block_usage"`
	RusageOutBlock          int       `json:"rusage.out_block" type:"counter" metric:"SYSINFO_out_block_usage"`
	RusageVolCtsw           int       `json:"rusage.vol_ctsw" type:"gauge" metric:"SYSINFO_vol_ctsw"`
	RusageInvolCtsw         int       `json:"rusage.invol_ctsw" type:"gauge" metric:"SYSINFO_in_vol_ctsw"`
}

func LoadSystemInfo(labelMap map[string]string, sysInfo SystemInfo) map[string]string {
	labelMap["NODE_NAME"] = sysInfo.UtsNodename
	return labelMap
}
