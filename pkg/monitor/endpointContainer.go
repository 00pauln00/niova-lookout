package monitor

import (
	"encoding/json"
	//	"strings"
	"sync"
	"syscall"
	"time"

	//	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

type EPContainer struct {
	MonitorUUID      string
	SerfMembershipCB func() map[string]bool
	epMap            map[uuid.UUID]*NcsiEP
	mutex            sync.Mutex
	HttpQuery        map[string](chan []byte)
}

func (epc *EPContainer) GetList() map[uuid.UUID]*NcsiEP {
	epc.mutex.Lock()
	defer epc.mutex.Unlock()
	return epc.epMap
}

func (epc *EPContainer) MarkAlive(serviceUUID string) error {
	serviceID, err := uuid.Parse(serviceUUID)
	if err != nil {
		return err
	}
	service, ok := epc.epMap[serviceID]
	if ok && (service.State == EPstateInit || service.State == EPstateDown) {
		panic("This code path is broken")
		service.pendingCmds = make(map[uuid.UUID]*epCommand)

		service.ChangeState(EPstateRunning)
		service.LastReport = time.Now()
	}
	return nil
}

func (epc *EPContainer) RefreshEndpoints() {
	for _, ep := range epc.epMap {
		// only check liveness for local EPs
		ep.RemoveStaleFiles()
		err := ep.Detect()
		if err != nil {
			log.Error(err)
		}
	}
}

// func (epc *EPContainer) HandleHttpQuery(cmpstr string, uuid uuid.UUID) {
// 	if strings.Contains(cmpstr, "HTTP") {
// 		var output []byte
// 		if ep := epc.epMap[uuid]; ep != nil {
// 			err := ep.Complete(cmpstr, &output)
// 			if err != nil {
// 				output = []byte(err.Error())
// 			}
// 		}

// 		if channel, ok := epc.HttpQuery[cmpstr]; ok {
// 			channel <- output
// 		}
// 		return
// 	}
// }

func (epc *EPContainer) ProcessEndpoint(epUuid uuid.UUID, cmdUuid uuid.UUID) {

	if ep := epc.epMap[epUuid]; ep != nil {
		//		var output []byte
		err := ep.Complete(cmdUuid, nil)

		log.Debugf("ep=%s cmd=%s err=%s",
			epUuid.String(), cmdUuid.String(), err)
	}
}

func (epc *EPContainer) Lookup(node uuid.UUID) *NcsiEP {
	epc.mutex.Lock()
	defer epc.mutex.Unlock()
	return epc.epMap[node]
}

func (epc *EPContainer) TakeSnapshot() map[uuid.UUID]*NcsiEP {
	nodeMap := make(map[uuid.UUID]*NcsiEP)
	epc.mutex.Lock()
	defer epc.mutex.Unlock()
	for k, v := range epc.epMap {
		nodeMap[k] = v
	}
	return nodeMap
}

func (epc *EPContainer) UpdateEpMap(uuid uuid.UUID, newlns *NcsiEP) {
	epc.mutex.Lock()
	defer epc.mutex.Unlock()

	epc.epMap[uuid] = newlns
}

func (epc *EPContainer) JsonMarshal() []byte {
	var jsonData []byte
	epc.mutex.Lock()
	jsonData, err := json.MarshalIndent(epc.epMap, "", "\t")
	epc.mutex.Unlock()

	if err != nil {
		return nil
	}

	return jsonData
}

func (epc *EPContainer) JsonMarshalUUID(uuid uuid.UUID) []byte {
	var jsonData []byte
	var err error

	ep := epc.Lookup(uuid)

	if ep != nil {
		jsonData, err = json.MarshalIndent(ep, "", "\t")
	} else {
		// Return an empty set if the item does not exist
		jsonData = []byte("{}")
	}

	if err != nil {
		return nil
	}

	return jsonData
}

func (epc *EPContainer) InitializeEpMap() error {
	epc.epMap = make(map[uuid.UUID]*NcsiEP)
	if epc.epMap == nil {
		return syscall.ENOMEM
	}
	return nil
}
