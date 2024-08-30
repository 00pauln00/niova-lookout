package applications

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

type NiovaClient struct {
	Uuid   uuid.UUID
	EPInfo CtlIfOut
}

func (n *NiovaClient) GetAppType() string {
	return "niovaClient"
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

func (n *NiovaClient) Parse(map[string]string, http.ResponseWriter, *http.Request) {
	logrus.Info("niovaClient Parse... unimplemented")
}

func (n *NiovaClient) SetCtlIfOut(CtlIfOut) {
	logrus.Info("niovaClient SetCtlIfOut... unimplemented")
}

func (n *NiovaClient) SetMembership(map[string]bool) {
	panic("unimplemented")
}

func (n *NiovaClient) SetUUID(uuid uuid.UUID) {
	n.Uuid = uuid
}

func (n *NiovaClient) GetAppDetectInfo(b bool) (string, EPcmdType) {
	panic("unimplemented")
}
