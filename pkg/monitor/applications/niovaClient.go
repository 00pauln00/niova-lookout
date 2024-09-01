package applications

import (
	"fmt"
	"net/http"

	"github.com/00pauln00/niova-lookout/pkg/prometheusHandler"
	"github.com/google/uuid"
)

type NiovaClient struct {
	Uuid   uuid.UUID
	EPInfo CtlIfOut
}

type NiovaClientInfo struct {
	VdevUUID            string         `json:"vdev-uuid"`
	Status              string         `json:"status"`
	QueueDepth          int            `json:"queue-depth" type:"gauge" metric:"queue_depth"`
	VblksRpRead         int            `json:"vblks-rp-read" type:"counter" metric:"vblks_rp_read"`
	VblksRpWrite        int            `json:"vblks-rp-write" type:"counter" metric:"vblks_rp_write"`
	VblksEcRead         int            `json:"vblks-ec-read" type:"counter" metric:"vblks_ec_read"`
	VblksEcWrite        int            `json:"vblks-ec-write" type:"counter" metric:"vblks_ec_write"`
	VblksS3Read         int            `json:"vblks-s3-read" type:"counter" metric:"vblks_s3_read"`
	VblksHoleRead       int            `json:"vblks-hole-read" type:"counter" metric:"vblks_hole_read"`
	VblksRpRedirectRead int            `json:"vblks-rp-redirect-read" type:"counter" metric:"vblks_rp_redirect_read"`
	RpReadSize          map[string]int `json:"rp-read-size" type:"histogram" metric:"rp_read_size"`
	RpWriteSize         map[string]int `json:"rp-write-size" type:"histogram" metric:"rp_write_size"`
	EcReadSize          map[string]int `json:"ec-read-size" type:"histogram" metric:"ec_read_size"`
	EcWriteSize         map[string]int `json:"ec-write-size" type:"histogram" metric:"ec_write_size"`
	RpReadLat           map[string]int `json:"rp-read-lat" type:"histogram" metric:"rp_read_lat"`
	RpWriteLat          map[string]int `json:"rp-write-lat" type:"histogram" metric:"rp_write_lat"`
	EcReadLat           map[string]int `json:"ec-read-lat" type:"histogram" metric:"ec_read_lat"`
	EcWriteLat          map[string]int `json:"ec-write-lat" type:"histogram" metric:"ec_write_lat"`
}

func (n *NiovaClient) GetAppType() string {
	return "NCLIENT"
}

func (n *NiovaClient) GetCtlIfOut() CtlIfOut {
	return n.EPInfo
}

func (n *NiovaClient) GetMembership() map[string]bool {
	panic("unimplemented")
}

func (n *NiovaClient) GetUUID() uuid.UUID {
	return n.Uuid
}

func (n *NiovaClient) Parse(labelMap map[string]string, w http.ResponseWriter, r *http.Request) {
	var output string
	labelMap["NCLIENT_UUID"] = n.GetUUID().String()
	labelMap["TYPE"] = n.GetAppType()
	if condirion := len(n.EPInfo.NiovaClientInformation) == 0; !condirion {
		output += prometheusHandler.GenericPromDataParser(n.EPInfo.NiovaClientInformation[0], labelMap)
		output += prometheusHandler.GenericPromDataParser(n.EPInfo.SysInfo, labelMap)
	}
	fmt.Fprintf(w, "%s", output)
}

func (n *NiovaClient) SetCtlIfOut(c CtlIfOut) {
	n.EPInfo.NiovaClientInformation = c.NiovaClientInformation
	n.EPInfo.SysInfo = c.SysInfo
}

func (n *NiovaClient) SetMembership(map[string]bool) {
	panic("unimplemented")
}

func (n *NiovaClient) SetUUID(uuid uuid.UUID) {
	n.Uuid = uuid
}

func (n *NiovaClient) GetAppDetectInfo(b bool) (string, EPcmdType) {
	return "GET /.*/.*/.*", CustomOp
}
