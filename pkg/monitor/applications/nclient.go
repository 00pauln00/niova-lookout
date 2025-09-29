package applications

import (
	"fmt"
	"net/http"

	pm "github.com/00pauln00/niova-lookout/pkg/prometheusHandler"
	"github.com/google/uuid"
)

type Nclient struct {
	Uuid   uuid.UUID
	EPInfo CtlIfOut
}

type NclientInfo struct {
	VdevUUID            string    `json:"vdev-uuid"`
	Status              string    `json:"status"`
	BuildId             string    `json:"build-id"`
	QueueDepth          uint64    `json:"queue-depth" type:"gauge" metric:"nclient_queue_depth"`
	VblksRpRead         uint64    `json:"vblks-rp-read" type:"counter" metric:"nclient_vblks_rp_read"`
	VblksRpWrite        uint64    `json:"vblks-rp-write" type:"counter" metric:"nclient_vblks_rp_write"`
	VblksEcRead         uint64    `json:"vblks-ec-read" type:"counter" metric:"nclient_vblks_ec_read"`
	VblksEcWrite        uint64    `json:"vblks-ec-write" type:"counter" metric:"nclient_vblks_ec_write"`
	VblksS3Read         uint64    `json:"vblks-s3-read" type:"counter" metric:"nclient_vblks_s3_read"`
	VblksHoleRead       uint64    `json:"vblks-hole-read" type:"counter" metric:"nclient_vblks_hole_read"`
	VblksRpRedirectRead uint64    `json:"vblks-rp-redirect-read" type:"counter" metric:"nclient_vblks_rp_redirect_read"`
	RpReadSize          Histogram `json:"rp-read-size" type:"histogram" metric:"nclient_rp_read_size"`
	RpWriteSize         Histogram `json:"rp-write-size" type:"histogram" metric:"nclient_rp_write_size"`
	EcReadSize          Histogram `json:"ec-read-size" type:"histogram" metric:"nclient_ec_read_size"`
	EcWriteSize         Histogram `json:"ec-write-size" type:"histogram" metric:"nclient_ec_write_size"`
	RpReadLat           Histogram `json:"rp-read-lat" type:"histogram" metric:"nclient_rp_read_lat"`
	RpWriteLat          Histogram `json:"rp-write-lat" type:"histogram" metric:"nclient_rp_write_lat"`
	EcReadLat           Histogram `json:"ec-read-lat" type:"histogram" metric:"nclient_ec_read_lat"`
	EcWriteLat          Histogram `json:"ec-write-lat" type:"histogram" metric:"nclient_ec_write_lat"`
}

func (n *Nclient) GetAppName() string {
	return "NCLIENT"
}

func (n *Nclient) GetCtlIfOut() CtlIfOut {
	return n.EPInfo
}

func (n *Nclient) GetMembership() map[string]bool {
	return nil
}

func (n *Nclient) GetUUID() uuid.UUID {
	return n.Uuid
}

func (n *Nclient) GetAltName() string {
	return ""
}

func (n *Nclient) Parse(labels map[string]string, w http.ResponseWriter,
	r *http.Request) {
	var out string
	labels["NCLIENT_UUID"] = n.GetUUID().String()
	labels["TYPE"] = n.GetAppName()

	if len(n.EPInfo.Nclient.VdevUUID) > 0 {
		out += pm.GenericPromDataParser(*n.EPInfo.Nclient, labels)
		out += pm.GenericPromDataParser(n.EPInfo.NiorqMgr[0], labels)
		out += pm.GenericPromDataParser(*n.EPInfo.SysInfo, labels)
	}

	for _, task := range n.EPInfo.Tasks {
		// load labels with buffer set node data
		labels["NAME"] = task.Type
		// Parse each buffer set node info
		out += pm.GenericPromDataParser(task, labels)
	}

	fmt.Fprintf(w, "%s", out)
}

func (n *Nclient) SetCtlIfOut(c CtlIfOut) {
	n.EPInfo.Nclient = c.Nclient
	n.EPInfo.SysInfo = c.SysInfo
	n.EPInfo.NiorqMgr = c.NiorqMgr
	n.EPInfo.Tasks = c.Tasks
}

func (n *Nclient) SetMembership(map[string]bool) {
	return
}

func (n *Nclient) SetUUID(uuid uuid.UUID) {
	n.Uuid = uuid
}

func (n *Nclient) GetAppDetectInfo(b bool) (string, EPcmdType) {
	return "GET /.*/.*/.*", NCLIENTInfoOp
}

func (n *Nclient) LoadSystemInfo(labels map[string]string) map[string]string {
	labels["NODE_NAME"] = n.EPInfo.SysInfo.UtsNodename
	return labels
}

func (n *Nclient) IsMonitoringEnabled() bool {
	return true
}
