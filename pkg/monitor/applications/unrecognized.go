package applications

import (
	"net/http"

	"github.com/google/uuid"
)

type Unrecognized struct {
	Uuid   uuid.UUID
	EPInfo CtlIfOut
}

func (u *Unrecognized) GetAppType() string {
	return "unrecognized"
}

func (u *Unrecognized) GetCtlIfOut() CtlIfOut {
	panic("unimplemented")
}

func (u *Unrecognized) GetMembership() map[string]bool {
	panic("unimplemented")
}

func (u *Unrecognized) GetUUID() uuid.UUID {
	return u.Uuid
}

func (u *Unrecognized) Parse(map[string]string, http.ResponseWriter, *http.Request) {
	return
}

func (u *Unrecognized) SetCtlIfOut(CtlIfOut) {
	panic("unimplemented")
}

func (u *Unrecognized) SetMembership(map[string]bool) {
	panic("unimplemented")
}

func (u *Unrecognized) SetUUID(uuid uuid.UUID) {
	u.Uuid = uuid
}

func (u *Unrecognized) GetAppDetectInfo(b bool) (string, EPcmdType) {
	return "GET /.*/.*/.*", CustomOp
}
