package applications

import (
	"net/http"

	"github.com/google/uuid"
)

type Unrecognized struct {
	Uuid   uuid.UUID
	EPInfo CtlIfOut
}

func (u *Unrecognized) GetAppName() string {
	return "unrecognized"
}

func (u *Unrecognized) GetCtlIfOut() CtlIfOut {
	return u.EPInfo
}

func (u *Unrecognized) GetMembership() map[string]bool {
	return nil
}

func (u *Unrecognized) GetUUID() uuid.UUID {
	return u.Uuid
}

func (u *Unrecognized) Parse(map[string]string, http.ResponseWriter, *http.Request) {
	return
}

func (u *Unrecognized) SetCtlIfOut(CtlIfOut) {
	return
}

func (u *Unrecognized) SetMembership(map[string]bool) {
	return
}

func (u *Unrecognized) SetUUID(uuid uuid.UUID) {
	u.Uuid = uuid
}

func (u *Unrecognized) GetAppDetectInfo(b bool) (string, EPcmdType) {
	return "GET /.*/.*/.*", CustomOp
}

func (u *Unrecognized) GetAltName() string {
	return ""
}

func (u *Unrecognized) LoadSystemInfo(labelMap map[string]string) map[string]string {
	return labelMap
}
