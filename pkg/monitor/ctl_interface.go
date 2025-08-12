package monitor

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/00pauln00/niova-lookout/pkg/monitor/applications"
	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

// #include <unistd.h>
// //#include <errno.h>
// //int usleep(useconds_t usec);
import "C"

const (
	maxPendingCmdsEP  = 32
	maxOutFileSize    = 4 * 1024 * 1024
	outFileTimeoutSec = 2
	outFilePollMsec   = 1
	EPtimeoutSec      = 60.0
	LookoutPrefixStr  = "lkoep_"
)

type NcsiEP struct {
	App          applications.AppIF       `json:"-"`
	Uuid         uuid.UUID                `json:"-"`
	Path         string                   `json:"-"`
	Name         string                   `json:"name"`
	NiovaSvcType string                   `json:"type"`
	Port         int                      `json:"port"`
	LastReport   time.Time                `json:"-"`
	LastRequest  time.Time                `json:"-"`
	LastClear    time.Time                `json:"-"`
	Alive        bool                     `json:"responsive"`
	EPInfo       applications.CtlIfOut    `json:"ep_info"` //May need to change this to a pointer
	pendingCmds  map[uuid.UUID]*epCommand `json:"-"`
	Mutex        sync.Mutex               `json:"-"`
}

type epCommand struct {
	ep      *NcsiEP
	cmd     string
	id      uuid.UUID
	outJSON []byte
	err     error
	op      applications.EPcmdType
}

func (epc *epCommand) getOutFnam() string {
	return epc.ep.epRoot() + "/output/" + LookoutPrefixStr + epc.id.String()
}

func (epc *epCommand) getInFnam() string {
	return epc.ep.epRoot() + "/input/" + LookoutPrefixStr + epc.id.String()
}

func (epc *epCommand) getCmdBuf() []byte {
	return []byte(epc.cmd)
}

func (epc *epCommand) getOutJSON() []byte {
	return []byte(epc.outJSON)
}

func (epc *epCommand) checkOutFile() error {
	var tmp_stb syscall.Stat_t
	if err := syscall.Stat(epc.getOutFnam(), &tmp_stb); err != nil {
		return err
	}

	if tmp_stb.Size > maxOutFileSize {
		return syscall.E2BIG
	}

	return nil
}

func (epc *epCommand) loadOutfile() {
	if epc.err = epc.checkOutFile(); epc.err != nil {
		return
	}

	// Try to read the file
	epc.outJSON, epc.err = ioutil.ReadFile(epc.getOutFnam())
	if epc.err != nil {
		log.Errorf("checkOutFile(): %s getOutFnam %s",
			epc.err, epc.getOutFnam())
	}
	return
}

// Makes a 'unique' filename for the command and adds it to the map
func (epc *epCommand) prep() {
	epc.id = uuid.New()
	epc.cmd = epc.cmd + "\nOUTFILE /" + epc.id.String() + "\n"

	// Add the epc into the endpoint's pending epc map
	epc.ep.addCmd(epc)
}

func (epc *epCommand) write() {
	epc.err = ioutil.WriteFile(epc.getInFnam(), epc.getCmdBuf(), 0644)
	if epc.err != nil {
		log.Errorf("ioutil.WriteFile(): %s", epc.err)
		return
	}
	epc.ep.LastRequest = time.Now()

	if log.IsLevelEnabled(log.DebugLevel) {
		log.Debugf("ev-submit: uuid=%s %s: %s",
			epc.ep.Uuid, epc.getInFnam(), epc.cmd)

	} else if log.IsLevelEnabled(log.InfoLevel) {
		log.Infof("ev-submit: uuid=%s %s",
			epc.ep.Uuid, epc.getInFnam())
	}
}

func (epc *epCommand) submit() {
	if err := epc.ep.mayQueueCmd(); err == false {
		log.Error("Too many pending commands for endpoint: ",
			epc.ep.Uuid)
		return
	}
	epc.prep()
	epc.write()
}

func (ep *NcsiEP) mayQueueCmd() bool {
	log.Debugf("uuid=%s pending=%d max=%d",
		ep.Uuid, len(ep.pendingCmds), maxPendingCmdsEP)

	// Enforces a max queue depth of 1 for internal scheduling logic,
	// while still allowing externally-triggered commands (e.g., via /v1/)
	// to be processed.
	if len(ep.pendingCmds) > 0 {
		log.Debugf("ep %s has %d pending cmds",
			ep.Uuid, len(ep.pendingCmds))

		if len(ep.pendingCmds) > 0 {
			if time.Since(ep.LastRequest) > time.Second*EPtimeoutSec {
				log.Debugf("ep %s has stale cmds (%f seconds), removing them from the queue",
					ep.Uuid, EPtimeoutSec)

				for x := range ep.pendingCmds {
					log.Info("remove cmd: ", ep.Uuid, x)
					ep.removeCmd(x)
				}
				ep.Alive = false
			}
		}
	}
	return len(ep.pendingCmds) == 0
}

func (ep *NcsiEP) addCmd(cmd *epCommand) error {
	// Add the cmd into the endpoint's pending cmd map
	cmd.ep.Mutex.Lock()
	_, exists := cmd.ep.pendingCmds[cmd.id]
	if exists == false {
		cmd.ep.pendingCmds[cmd.id] = cmd
	}
	cmd.ep.Mutex.Unlock()

	if exists == true {
		return syscall.EEXIST
	}

	return nil
}

func (ep *NcsiEP) removeCmd(cmdUUID uuid.UUID) *epCommand {
	ep.Mutex.Lock()
	epc, ok := ep.pendingCmds[cmdUUID]
	if ok {
		delete(ep.pendingCmds, cmdUUID)
	}
	ep.Mutex.Unlock()

	return epc
}

func (ep *NcsiEP) epRoot() string {
	return ep.Path
}

// func (ep *NcsiEP) CtlCustomQuery(customCMD string, ID string) error {
// 	log.Infof("Custom query for endpoint %s: %s", ep.Uuid, customCMD)
// 	cmd := epCommand{ep: ep, cmd: customCMD, op: applications.CustomOp, fn: ID}
// 	cmd.submit()
// 	return cmd.err
// }

func (ep *NcsiEP) update(ctlData applications.CtlIfOut) {
	ep.App.SetCtlIfOut(ctlData)
	ep.EPInfo = ep.App.GetCtlIfOut()
	ep.LastReport = time.Now()
	if !ep.Alive {
		ep.Alive = true
	}
}

func (ep *NcsiEP) Complete(cmdUuid uuid.UUID, output *[]byte) error {
	epc := ep.removeCmd(cmdUuid)
	if epc == nil {
		return syscall.ENOENT
	}

	epc.loadOutfile()
	if epc.err != nil {
		return epc.err
	}

	//Add here to break for custom command
	if epc.op == applications.CustomOp {
		log.Debug("Custom command identified: ", cmdUuid.String())
		*output = epc.getOutJSON()
		return nil
	}

	var err error
	var ctlifout applications.CtlIfOut
	if err = json.Unmarshal(epc.getOutJSON(), &ctlifout); err != nil {
		if ute, ok := err.(*json.UnmarshalTypeError); ok {
			log.Errorf("UnmarshalTypeError %v - %v - %v\n",
				ute.Value, ute.Type, ute.Offset)
		} else {
			log.Errorf("Other error: %s\n", err)
			log.Errorf("Contents: %s\n", string(epc.getOutJSON()))
		}
		return err
	}
	ep.update(ctlifout)

	return nil
}

func (ep *NcsiEP) removeFiles(folder string) {
	files, err := ioutil.ReadDir(folder)
	if err != nil {
		return
	}

	for _, file := range files {
		if strings.Contains(file.Name(), LookoutPrefixStr) {
			checkTime := file.ModTime().Local().Add(time.Hour)
			if time.Now().After(checkTime) {
				os.Remove(folder + file.Name())
			}
		}
	}
}

func (ep *NcsiEP) RemoveStaleFiles() {
	//Remove stale ctl files
	input_path := ep.Path + "/input/"
	ep.removeFiles(input_path)
	//output files
	output_path := ep.Path + "/output/"
	ep.removeFiles(output_path)
}

// Called every sleep time (default 20 seconds) to check if the endpoint is alive
func (ep *NcsiEP) Detect() error {
	var err error
	if ep.App == nil {
		return errors.New("app is nil")
	}
	if ep.Alive {
		err = ep.GetAppInfo()
		if time.Since(ep.LastReport) > time.Second*EPtimeoutSec {
			log.Debugf("Endpoint %s timed out\n", ep.Uuid)
			ep.Alive = false
		}
	} else {
		//see if app came back up every 60 seconds
		if time.Since(ep.LastClear) > time.Second*EPtimeoutSec {
			err = ep.GetAppInfo()
			ep.LastClear = time.Now()
		}
	}
	return err
}

func (ep *NcsiEP) GetAppInfo() error {
	var err error
	cmdStr, op := ep.App.GetAppDetectInfo(false)
	cmd := epCommand{ep: ep, cmd: cmdStr, op: op}
	cmd.submit()
	if cmd.err != nil && cmd.op == applications.SystemInfoOp {
		cmdStr, op = ep.App.GetAppDetectInfo(true)
		cmd = epCommand{ep: ep, cmd: cmdStr, op: op}
		cmd.submit()
		return cmd.err
	}
	ep.Name = ep.App.GetAltName()
	return err
}

func (ep *NcsiEP) IdentifyApplicationType() {
	log.Info("GetAppType for: ", ep.Uuid)

	epc := epCommand{
		ep:  ep,
		cmd: "GET /.*",
		op:  applications.IdentifyOp,
		id:  uuid.New(),
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal("Failed to create watcher:", err)
	}
	defer watcher.Close()

	outputDir := epc.ep.epRoot() + "/output"
	err = watcher.Add(outputDir)
	if err != nil {
		if errors.Is(err, syscall.ENOSPC) {
			log.Error("Failed to add directory to watcher due to no space left on device:", err)
		} else {
			log.Fatal("Failed to add directory to watcher:", err)
		}
	}

	done := make(chan bool)
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					log.Trace("Watcher.Events closed")
					return
				}
				log.Debug("Event received:", event)
				if (event.Op&fsnotify.Write == fsnotify.Write ||
					event.Op&fsnotify.Create == fsnotify.Create) && event.Name == outputDir+"/"+epc.id.String() {
					log.Trace("File modified:", event.Name)
					done <- true
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					log.Info("Watcher.Errors closed")
					return
				}
				log.Error("Watcher error:", err)
			}
		}
	}()

	epc.submit()

	select {
	case <-done:
		log.Debug("File write detected")
	case <-time.After(1 * time.Second): // Timeout after 1 seconds
		log.Warn("Timeout waiting for file write")
	}

	c := ep.removeCmd(epc.id)
	if c == nil {
		log.Error("removeCmd returned nil")
		return
	}

	c.loadOutfile()
	output := c.getOutJSON()
	ep.App, err = applications.DetermineApp(output)
	if err != nil {
		log.Error("DetermineApp for ", ep.Uuid, " failed:", err)
	}
	log.Info("App type for ", ep.Uuid, " determined: ", ep.App.GetAppName())
}
