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

type epstate int

const (
	EPstateUnknown epstate = iota
	EPstateInit
	EPstateRunning
	EPstateDown
	EPstateRemoving
)

var OutfileTtlMinutes = 5 * time.Minute

func (s epstate) String() string {
	switch s {
	case EPstateInit:
		return "init"
	case EPstateRunning:
		return "running"
	case EPstateDown:
		return "down"
	case EPstateRemoving:
		return "removing"
	default:
		return "unknown"
	}
}

type NcsiEP struct {
	App          applications.AppIF       `json:"-"`
	Uuid         uuid.UUID                `json:"-"`
	Path         string                   `json:"-"`
	Name         string                   `json:"name"`
	NiovaSvcType string                   `json:"type"`
	Port         int                      `json:"port"`
	LastReport   time.Time                `json:"-"`
	LastRequest  time.Time                `json:"-"`
	LastClear    time.Time                `json:"-"` //XXX remove me
	State        epstate                  `json:"state"`
	EPInfo       applications.CtlIfOut    `json:"ep_info"` //May need to change this to a pointer
	pendingCmds  map[uuid.UUID]*epCommand `json:"-"`
	Mutex        sync.Mutex               `json:"-"`
}

func (ep *NcsiEP) ChangeState(s epstate) {
	// pc := make([]uintptr, 10) // at least 1 entry needed
	// runtime.Callers(2, pc)
	// f := runtime.FuncForPC(pc[0])
	// file, line := f.FileLine(pc[0])
	// fmt.Printf("%s:%d %s\n", file, line, f.Name())

	log.Warn("ep=%s state from %s to %s",
		ep.Uuid.String(), ep.State.String(), s.String())

	ep.State = s
}

type epCommand struct {
	ep      *NcsiEP
	cmd     string
	id      uuid.UUID
	outJSON []byte
	err     error
	op      applications.EPcmdType
}

func (c *epCommand) getOutFnam() string {
	return c.ep.epRoot() + "/output/" + LookoutPrefixStr + c.id.String()
}

func (c *epCommand) getInFnam() string {
	return c.ep.epRoot() + "/input/" + LookoutPrefixStr + c.id.String()
}

func (c *epCommand) getCmdBuf() []byte {
	return []byte(c.cmd)
}

func (c *epCommand) getOutJSON() []byte {
	return []byte(c.outJSON)
}

func (c *epCommand) checkOutFile() error {
	var tmp_stb syscall.Stat_t
	if err := syscall.Stat(c.getOutFnam(), &tmp_stb); err != nil {
		return err
	}

	if tmp_stb.Size > maxOutFileSize {
		return syscall.E2BIG
	}

	return nil
}

func (c *epCommand) loadOutfile() {
	if c.err = c.checkOutFile(); c.err != nil {
		return
	}

	// Try to read the file
	c.outJSON, c.err = ioutil.ReadFile(c.getOutFnam())
	if c.err != nil {
		log.Errorf("checkOutFile(): %s getOutFnam %s",
			c.err, c.getOutFnam())
	}
	return
}

// Makes a 'unique' filename for the command and adds it to the map
func (c *epCommand) prep() {
	c.id = uuid.New()
	c.cmd = c.cmd + "\nOUTFILE /" + c.id.String() + "\n"

	// Add the c into the endpoint's pending epc map
	c.ep.addCmd(c)
}

func (c *epCommand) write() {
	c.err = ioutil.WriteFile(c.getInFnam(), c.getCmdBuf(), 0644)
	if c.err != nil {
		log.Errorf("ioutil.WriteFile(): %s", c.err)
		return
	}
	c.ep.LastRequest = time.Now()

	if log.IsLevelEnabled(log.DebugLevel) {
		log.Debugf("ev-submit: uuid=%s %s: %s",
			c.ep.Uuid, c.getInFnam(), c.cmd)

	} else if log.IsLevelEnabled(log.InfoLevel) {
		log.Infof("ev-submit: uuid=%s %s",
			c.ep.Uuid, c.getInFnam())
	}
}

func (c *epCommand) submit() {
	if err := c.ep.mayQueueCmd(); err == false {
		log.Error("Too many pending commands for endpoint: ",
			c.ep.Uuid)
		return
	}
	c.prep()
	c.write()
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
				ep.ChangeState(EPstateDown)
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
	c, ok := ep.pendingCmds[cmdUUID]
	if ok {
		delete(ep.pendingCmds, cmdUUID)
	}
	ep.Mutex.Unlock()

	return c
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

	if ep.State != EPstateRunning {
		ep.ChangeState(EPstateRunning)
	}
}

func (ep *NcsiEP) Complete(cmdUuid uuid.UUID, output *[]byte) error {
	c := ep.removeCmd(cmdUuid)
	if c == nil {
		return syscall.ENOENT
	}

	c.loadOutfile()
	if c.err != nil {
		return c.err
	}

	//Add here to break for custom command
	if c.op == applications.CustomOp {
		log.Debug("Custom command identified: ", cmdUuid.String())
		*output = c.getOutJSON()
		return nil
	}

	var err error
	var ctlifout applications.CtlIfOut
	if err = json.Unmarshal(c.getOutJSON(), &ctlifout); err != nil {
		if ute, ok := err.(*json.UnmarshalTypeError); ok {
			log.Errorf("UnmarshalTypeError %v - %v - %v\n",
				ute.Value, ute.Type, ute.Offset)
		} else {
			log.Errorf("Other error: %s\n", err)
			log.Errorf("Contents: %s\n", string(c.getOutJSON()))
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
			checkTime := file.ModTime().Local().Add(OutfileTtlMinutes * time.Minute)
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

	switch ep.State {
	case EPstateInit:
	case EPstateRunning:
		err = ep.queryApp()
		if time.Since(ep.LastReport) > time.Second*EPtimeoutSec {
			log.Debugf("Endpoint %s timed out\n", ep.Uuid)
			if ep.State == EPstateRunning {
				ep.ChangeState(EPstateDown)
			}
		}
	case EPstateDown:
		//see if app came back up every 60 seconds
		if time.Since(ep.LastClear) > time.Second*EPtimeoutSec {
			err = ep.queryApp()
			ep.LastClear = time.Now()
		}
	default:
	}

	return err
}

func (ep *NcsiEP) queryApp() error {
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

	c := epCommand{
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

	outputDir := c.ep.epRoot() + "/output"
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
					event.Op&fsnotify.Create == fsnotify.Create) && event.Name == outputDir+"/"+c.id.String() {
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

	c.submit()

	select {
	case <-done:
		log.Debug("File write detected")
	case <-time.After(1 * time.Second): // Timeout after 1 seconds
		log.Warn("Timeout waiting for file write")
	}

	remove := ep.removeCmd(c.id)
	if remove == nil {
		log.Error("removeCmd returned nil")
		return
	}

	remove.loadOutfile()
	output := remove.getOutJSON()
	ep.App, err = applications.DetermineApp(output)
	if err != nil {
		log.Error("DetermineApp for ", ep.Uuid, " failed:", err)
	}
	log.Info("App type for ", ep.Uuid, " determined: ", ep.App.GetAppName())
}
