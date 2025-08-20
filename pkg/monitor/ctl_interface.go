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

	"github.com/google/uuid"

	"github.com/00pauln00/niova-lookout/pkg/monitor/applications"
	"github.com/00pauln00/niova-lookout/pkg/xlog"
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

type Epstate int

const (
	EPstateUnknown Epstate = iota
	EPstateInit
	EPstateHasIdentity
	EPstateRunning
	EPstateDown
	EPstateRemoving
	EPstateAny
)

var OutfileTtlMinutes = 5 * time.Minute

func (s Epstate) String() string {
	switch s {
	case EPstateInit:
		return "init"
	case EPstateHasIdentity:
		return "identified"
	case EPstateRunning:
		return "running"
	case EPstateDown:
		return "down"
	case EPstateRemoving:
		return "removing"
	case EPstateAny:
		return "any"
	default:
		return "unknown"
	}
}

type NcsiEP struct {
	App          applications.AppIF       `json:"-"`
	Uuid         uuid.UUID                `json:"-"`
	Name         string                   `json:"name"`
	NiovaSvcType string                   `json:"type"`
	Port         int                      `json:"port"`
	LastReport   time.Time                `json:"-"`
	State        Epstate                  `json:"state"`
	EPInfo       applications.CtlIfOut    `json:"ep_info"` //May need to change this to a pointer
	pendingCmds  map[uuid.UUID]*epCommand `json:"-"`
	Mutex        sync.Mutex               `json:"-"`
	lh           *LookoutHandler          `json:"-"`
	lsofGen      uint64                   `json:"-"`
	watched      bool                     `json:"-"`
}

type epCommand struct {
	ep      *NcsiEP
	cmd     string
	id      uuid.UUID
	outJSON []byte
	err     error
	op      applications.EPcmdType
}

type EpPath string

const (
	EP_PATH_ROOT   EpPath = ""
	EP_PATH_INPUT  EpPath = "/input/"
	EP_PATH_OUTPUT EpPath = "/output/"
)

func (c *epCommand) getOutFnam() string {
	return c.ep.Xpath(EP_PATH_OUTPUT) + LookoutPrefixStr + c.id.String()
}

func (c *epCommand) getInFnam() string {
	return c.ep.Xpath(EP_PATH_INPUT) + LookoutPrefixStr + c.id.String()
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
		c.ep.Log(xlog.ERROR, "checkOutFile(): %v", c.err)
		return
	}

	// Try to read the file
	c.outJSON, c.err = ioutil.ReadFile(c.getOutFnam())
	if c.err != nil {
		xlog.Errorf("checkOutFile(): %s getOutFnam %s",
			c.err, c.getOutFnam())
	}
	return
}

// Makes a 'unique' filename for the command and adds it to the map
func (c *epCommand) prep() {
	c.id = uuid.New()
	c.cmd = c.cmd + "\nOUTFILE /" + LookoutPrefixStr + c.id.String() + "\n"

	// Add the c into the endpoint's pending epc map
	c.ep.addCmd(c)
}

func (c *epCommand) write() {
}

func (c *epCommand) submit() {
	if err := c.ep.mayQueueCmd(); err == false {
		c.ep.Log(xlog.INFO, "mayQueueCmd(): too many pending cmds")
		return
	}
	c.prep()

	c.err = ioutil.WriteFile(c.getInFnam(), c.getCmdBuf(), 0644)
	if c.err != nil {
		xlog.Errorf("ioutil.WriteFile(): %s", c.err)
		return
	}

	if xlog.IsLevelEnabled(xlog.DEBUG) {
		c.ep.Log(xlog.DEBUG, "uuid=%s %s: %s",
			c.ep.Uuid.String(), c.getInFnam(), c.cmd)

	} else if xlog.IsLevelEnabled(xlog.INFO) {
		c.ep.Log(xlog.INFO, "uuid=%s %s",
			c.ep.Uuid.String(), c.getInFnam())
	}
}

func isWatchedState(s Epstate) bool {
	switch s {
	case EPstateRemoving:
		return false
	case EPstateUnknown:
		return false
	case EPstateAny:
		return false
	default:
		break
	}

	return true
}

func (ep *NcsiEP) ensureWatchedState() {
	if ep.watched != isWatchedState(ep.State) {
		ep.LogWithDepth(xlog.FATAL, 1,
			"invalid state for being watched")
	}
}

// Called when upon a state is *changing*.  ChangeState() is the only allowed
// caller.
func (ep *NcsiEP) watchCtl(newState Epstate) {

	iws := isWatchedState(newState)

	if iws == ep.watched {
		return
	}

	var err error
	var action string

	if iws {
		action = "Add"
		err = ep.lh.EpWatcher.Add(ep.Xpath(EP_PATH_OUTPUT))
	} else {
		action = "Remove"
		err = ep.lh.EpWatcher.Remove(ep.Xpath(EP_PATH_OUTPUT))
	}

	if err != nil {
		ep.Log(xlog.ERROR, "Watcher.%s() failed: %v",
			action, err)
	}
	ep.watched = iws

	ep.Log(xlog.INFO, "watch state changed -> %s", action)
}

func (ep *NcsiEP) ChangeState(s Epstate) {
	if s == EPstateAny {
		ep.LogWithDepth(xlog.FATAL, 1,
			"%s is not a settable state", s.String())
	}

	if s != ep.State {
		old := ep.State

		ep.ensureWatchedState()
		ep.watchCtl(s)

		ep.State = s

		switch s {
		case EPstateRemoving:
			ep.flushCmds()
		case EPstateDown:
			ep.flushCmds()
		case EPstateInit:
			ep.LastReport = time.Now()
		case EPstateRunning:
			ep.LastReport = time.Now()
		case EPstateHasIdentity:
			ep.LastReport = time.Now()
		}

		ep.LogWithDepth(xlog.WARN, 1, "old-state=%s", old.String())
	}
}

func (ep *NcsiEP) Xpath(p EpPath) string {
	path := ep.lh.CTLPath + "/" + ep.Uuid.String() + string(p)

	ep.LogWithDepth(xlog.DEBUG, 1, "%s", path)

	return path
}

func (ep *NcsiEP) LsofGenUpdate(gen uint64) {
	if ep.lsofGen > gen {
		ep.Log(xlog.FATAL, "ep_gen is > %d", gen)

	} else if ep.lsofGen == gen {
		return
	}

	ep.lsofGen = gen
	ep.LogWithDepth(xlog.INFO, 1, "update lsofGen")
}

func (ep *NcsiEP) flushCmds() {
	for x := range ep.pendingCmds {
		ep.removeCmd(x)
	}

}

func (ep *NcsiEP) mayQueueCmd() bool {
	ep.Log(xlog.DEBUG, "")

	// Enforces a max queue depth of 1 for internal scheduling logic,
	// while still allowing externally-triggered commands (e.g., via /v1/)
	// to be processed.
	if len(ep.pendingCmds) > 0 {
		if time.Since(ep.LastReport) > time.Second*EPtimeoutSec {
			xlog.Debugf("ep %s has stale cmds (%f seconds), removing them from the queue",
				ep.Uuid, EPtimeoutSec)

			ep.flushCmds()

			if ep.State == EPstateInit {
				ep.ChangeState(EPstateRemoving)
			} else {
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
		ep.Log(xlog.INFO, "removed cmd %s", cmdUUID.String())
	}
	ep.Mutex.Unlock()

	return c
}

// func (ep *NcsiEP) CtlCustomQuery(customCMD string, ID string) error {
// 	xlog.Infof("Custom query for endpoint %s: %s", ep.Uuid, customCMD)
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

func (ep *NcsiEP) LogWithDepth(level int, depth int, format string,
	args ...interface{}) {
	// Common prefix fields
	prefixFormat := "%s@%s s=%s pc=%d age=%s lg=%d"
	prefixArgs := []interface{}{
		ep.App.GetAppName(),
		ep.Uuid.String(),
		ep.State.String(),
		len(ep.pendingCmds),
		time.Since(ep.LastReport).Truncate(time.Millisecond),
		ep.lsofGen,
	}

	var combinedFmt string
	var combinedArgs []interface{}

	// If caller passed a format, combine it
	if format != "" {
		combinedFmt = prefixFormat + " :: " + format

		if len(args) > 0 {
			combinedArgs = append(prefixArgs, args...)
		} else {
			combinedArgs = prefixArgs
		}
	} else {
		combinedFmt = prefixFormat
		combinedArgs = prefixArgs
	}

	xlog.WithDepth(level, 1+depth, combinedFmt, combinedArgs...)
}

func (ep *NcsiEP) Log(level int, format string, args ...interface{}) {
	ep.LogWithDepth(level, 1, format, args...)
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

	if ep.State == EPstateDown {
		ep.ChangeState(EPstateRunning)
	}

	var err error

	switch c.op {
	case applications.SystemInfoOp:
		fallthrough
	case applications.NISDInfoOp:
		var err error
		var ctlifout applications.CtlIfOut
		if err = json.Unmarshal(c.getOutJSON(), &ctlifout); err != nil {
			if ute, ok := err.(*json.UnmarshalTypeError); ok {
				xlog.Errorf("UnmarshalTypeError %v - %v - %v\n",
					ute.Value, ute.Type, ute.Offset)
			} else {
				xlog.Errorf("Other error: %s\n", err)
				xlog.Errorf("Contents: %s\n",
					string(c.getOutJSON()))
			}
			return err
		}
		ep.update(ctlifout)

	case applications.CustomOp:
		//Add here to break for custom command
		xlog.Debug("Custom command identified: ",
			cmdUuid.String())
		*output = c.getOutJSON()
		return nil

	case applications.IdentifyOp:
		ep.App, err = applications.DetermineApp(c.getOutJSON())

		if err == nil {
			ep.App.SetUUID(ep.Uuid)
			ep.ChangeState(EPstateHasIdentity)
			ep.queryApp()
		} else {
			xlog.Errorf("DetermineApp() uuid=%s err=%v",
				ep.Uuid.String(), err)
		}
	default:
	}

	return nil
}

func (ep *NcsiEP) removeFiles(folder string) {
	files, err := ioutil.ReadDir(folder)
	if err != nil {
		return
	}

	now := time.Now()

	for _, file := range files {
		if !strings.Contains(file.Name(), LookoutPrefixStr) {
			continue
		}

		fullPath := folder + file.Name()
		info, statErr := os.Stat(fullPath)

		if statErr != nil {
			if os.IsNotExist(statErr) {
				xlog.Debugf("file already missing: %s",
					fullPath)
			} else {
				xlog.Warnf("os.State() %s: %v",
					fullPath, statErr)
			}
			continue
		}

		expire := info.ModTime().Add(OutfileTtlMinutes)
		if now.After(expire) {
			xerr := os.Remove(folder + file.Name())

			if xerr != nil {
				xlog.Warnf("os.Remove() %s: %v",
					folder+file.Name(), xerr)
			} else {
				xlog.Debugf("os.Remove() %s (age=%v): %v",
					folder+file.Name(), now.Sub(expire),
					xerr)
			}
		}
	}
}

func (ep *NcsiEP) RemoveStaleFiles() {
	if isWatchedState(ep.State) {
		ep.removeFiles(ep.Xpath(EP_PATH_INPUT))
		ep.removeFiles(ep.Xpath(EP_PATH_OUTPUT))
	}
}

// Called every sleep time (default 20 seconds) to check if the endpoint is alive
func (ep *NcsiEP) Detect() error {
	var err error

	switch ep.State {
	case EPstateInit:
		ep.Log(xlog.INFO, "")
		c := epCommand{
			ep:  ep,
			cmd: "GET /.*",
			op:  applications.IdentifyOp,
			id:  uuid.New(),
		}
		c.submit()

	case EPstateHasIdentity:
		err = ep.queryApp()

	case EPstateRunning:
		if ep.App == nil {
			return errors.New("Running ep has nil app")
		}

		err = ep.queryApp()
		if time.Since(ep.LastReport) > time.Second*EPtimeoutSec {
			xlog.Debugf("Endpoint %s timed out\n", ep.Uuid)
			if ep.State == EPstateRunning {
				ep.ChangeState(EPstateDown)
			}
		}
	case EPstateDown:
		//see if app came back up every 60 seconds
		if time.Since(ep.LastReport) > time.Second*EPtimeoutSec {
			err = ep.queryApp()
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
