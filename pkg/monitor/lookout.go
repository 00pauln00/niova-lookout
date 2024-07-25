package monitor

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

//var HttpPort int

type EPContainer struct {
	MonitorUUID      string
	CTLPath          string
	PromPath         string
	AppType          string
	EnableHttp       bool
	SerfMembershipCB func() map[string]bool
	Statb            syscall.Stat_t
	EpWatcher        *fsnotify.Watcher
	EpMap            map[uuid.UUID]*NcsiEP
	Mutex            sync.Mutex
	run              bool
	HttpQuery        map[string](chan []byte)
	PortRange        []uint16
	PromPort         int
	RetPort          *int
	HttpPort         int
}

func (epc *EPContainer) tryAdd(uuid uuid.UUID) {
	lns := epc.EpMap[uuid]
	if lns == nil {
		newlns := NcsiEP{
			Uuid:         uuid,
			Path:         epc.CTLPath + "/" + uuid.String(),
			Name:         "r-a4e1",
			NiovaSvcType: "raft",
			LastReport:   time.Now(),
			LastClear:    time.Now(),
			Alive:        true,
			pendingCmds:  make(map[string]*epCommand),
		}

		if err := epc.EpWatcher.Add(newlns.Path + "/output"); err != nil {
			logrus.Fatal("Watcher.Add() failed:", err)
		}

		// serialize with readers in httpd context, this is the only
		// writer thread so the lookup above does not require a lock
		epc.Mutex.Lock()
		epc.EpMap[uuid] = &newlns
		epc.Mutex.Unlock()
		logrus.Debugf("added: %+v\n", newlns)
	}
}

func (epc *EPContainer) scan() {
	files, err := ioutil.ReadDir(epc.CTLPath)
	if err != nil {
		logrus.Fatal(err)
	}

	for _, file := range files {
		// Need to support removal of stale items
		if uuid, err := uuid.Parse(file.Name()); err == nil {
			if (epc.MonitorUUID == uuid.String()) || (epc.MonitorUUID == "*") {
				epc.tryAdd(uuid)
			}
		}
	}
}

// is this being called anywhere?
func (epc *EPContainer) CheckLiveness(peerUuid string, timeout time.Duration) bool {
	uuid, err := uuid.Parse(peerUuid)
	if err != nil {
		return false
	}
	timer := time.NewTimer(timeout)
	for {
		select {
		case <-timer.C:
			return false
		default:
			if epc.EpMap[uuid] == nil {
				time.Sleep(1 * time.Second)
			} else {
				timer.Stop()
				return epc.EpMap[uuid].Alive
			}
		}
	}
}

func (epc *EPContainer) monitor() error {
	var err error = nil

	for epc.run == true {
		var tmp_stb syscall.Stat_t
		var sleepTime time.Duration
		err = syscall.Stat(epc.CTLPath, &tmp_stb)
		if err != nil {
			logrus.Errorf("syscall.Stat('%s'): %s", epc.CTLPath, err)
			break
		}

		if tmp_stb.Mtim != epc.Statb.Mtim {
			epc.Statb = tmp_stb
			epc.scan()
		}

		// Query for liveness
		for _, ep := range epc.EpMap {
			ep.Remove()
			// what about the error that is returned?
			ep.Detect(epc.AppType)
		}

		sleepTimeStr := os.Getenv("LOOKOUT_SLEEP")
		logrus.Debug("Sleep time: ", sleepTimeStr)
		sleepTime, err = time.ParseDuration(sleepTimeStr)
		if err != nil {
			sleepTime = 5 * time.Second
			logrus.Debug("Bad environment variable - Defaulting to standard value")
		}
		time.Sleep(sleepTime)
	}

	return err
}

func (epc *EPContainer) JsonMarshalUUID(uuid uuid.UUID) []byte {
	var jsonData []byte
	var err error

	epc.Mutex.Lock()
	ep := epc.EpMap[uuid]
	epc.Mutex.Unlock()
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

func (epc *EPContainer) JsonMarshal() []byte {
	var jsonData []byte

	epc.Mutex.Lock()
	jsonData, err := json.MarshalIndent(epc.EpMap, "", "\t")
	epc.Mutex.Unlock()

	if err != nil {
		return nil
	}

	return jsonData
}

func (epc *EPContainer) processInotifyEvent(event *fsnotify.Event) {
	splitPath := strings.Split(event.Name, "/")
	cmpstr := splitPath[len(splitPath)-1]
	uuid, err := uuid.Parse(splitPath[len(splitPath)-3])
	if err != nil {
		return
	}

	//temp file exclusion
	if strings.Contains(cmpstr, ".") {
		return
	}

	//Check if its for HTTP
	if strings.Contains(cmpstr, "HTTP") {
		var output []byte
		if ep := epc.EpMap[uuid]; ep != nil {
			err := ep.Complete(cmpstr, &output)
			if err != nil {
				output = []byte(err.Error())
			}
		}

		if channel, ok := epc.HttpQuery[cmpstr]; ok {
			channel <- output
		}
		return
	}

	if ep := epc.EpMap[uuid]; ep != nil {
		var output []byte
		err := ep.Complete(cmpstr, &output)
		if err != nil {
			logrus.Debug("processInotifyEvent()", err, event.Name)
		}
	}
}

func (epc *EPContainer) epOutputWatcher() {
	for {
		select {
		case event := <-epc.EpWatcher.Events:

			if event.Op == fsnotify.Create {
				epc.processInotifyEvent(&event)
			}

			// watch for errors
		case err := <-epc.EpWatcher.Errors:
			logrus.Error("EpWatcher pipe: ", err)

		}
	}
}

func (epc *EPContainer) init() error {
	// Check the provided endpoint root path
	err := syscall.Stat(epc.CTLPath, &epc.Statb)
	if err != nil {
		return err
	}

	// Set path (Xxx still need to check if this is a directory or not)

	// Create the map
	epc.EpMap = make(map[uuid.UUID]*NcsiEP)
	if epc.EpMap == nil {
		return syscall.ENOMEM
	}

	epc.EpWatcher, err = fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	epc.run = true

	go epc.epOutputWatcher()

	epc.scan()

	return nil
}

func (epc *EPContainer) GetList() map[uuid.UUID]*NcsiEP {
	epc.Mutex.Lock()
	defer epc.Mutex.Unlock()
	return epc.EpMap
}

func (epc *EPContainer) MarkAlive(serviceUUID string) error {
	serviceID, err := uuid.Parse(serviceUUID)
	if err != nil {
		return err
	}
	service, ok := epc.EpMap[serviceID]
	if ok && service.Alive {
		service.pendingCmds = make(map[string]*epCommand)
		service.Alive = true
		service.LastReport = time.Now()
	}
	return nil
}

func (epc *EPContainer) writePromPath() error {
	f, err := os.OpenFile(epc.PromPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, err = f.WriteString(strconv.Itoa(epc.HttpPort))
	if err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}

	return nil
}

func (epc *EPContainer) Start() error {
	var err error
	epc.HttpQuery = make(map[string](chan []byte))

	err = epc.writePromPath()
	if err != nil {
		return err
	}
	//Setup lookout
	err = epc.init()
	if err != nil {
		logrus.Debug("Lookout Init - ", err)
		return err
	}

	//Start monitoring
	err = epc.monitor()
	if err != nil {
		logrus.Debug("Lookout Monitor - ", err)
		return err
	}

	return nil
}
