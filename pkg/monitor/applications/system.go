package applications

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// TODO: This may have to become an object of the app. the system itself is not an app but the system in which the app is running on.
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

func (s *Syst) LoadSystemInfo(labelMap map[string]string) map[string]string {
	labelMap["NODE_NAME"] = s.EPInfo.SysInfo.UtsNodename
	return labelMap
}

func (s *Syst) GetAppType() string {
	return "SYST"
}

func (s *Syst) GetAppDetectInfo(b bool) (string, EPcmdType) {
	return "GET /system_info/.*", SystemInfoOp
}

func (s *Syst) SetCtlIfOut(c CtlIfOut) {
	s.EPInfo.SysInfo = c.SysInfo
	logrus.Debugf("update-sys %+v \n", c.SysInfo)
}

func (s *Syst) GetCtlIfOut() CtlIfOut {
	return s.EPInfo
}

type Syst struct {
	EPInfo     CtlIfOut
	fromGossip bool
}

func (s *Syst) GetMembership() map[string]bool {
	return nil
}

func (s *Syst) SetMembership(map[string]bool) {
	return
}

func (s *Syst) Parse(map[string]string, http.ResponseWriter, *http.Request) {
	return
}

func (s *Syst) GetUUID() uuid.UUID {
	return uuid.UUID{}
}

func (s *Syst) SetUUID(uuid.UUID) {
	return
}

func (s *Syst) GetAltName() string {
	return ""
}

func (s *Syst) IsMonitoringEnabled() bool {
	return true
}

func (s *Syst) SetFromGossip(fromGossip bool) {
	s.fromGossip = fromGossip
}
func (s *Syst) FromGossip() bool {
	return s.fromGossip
}
