package monitor

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/00pauln00/niova-lookout/pkg/monitor/applications"
	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
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
)

type NcsiEP struct {
	App          applications.AppIF    `json:"-"`
	Uuid         uuid.UUID             `json:"-"`
	Path         string                `json:"-"`
	Name         string                `json:"name"`
	NiovaSvcType string                `json:"type"`
	Port         int                   `json:"port"`
	LastReport   time.Time             `json:"-"`
	LastRequest  time.Time             `json:"-"`
	LastClear    time.Time             `json:"-"`
	Alive        bool                  `json:"responsive"`
	EPInfo       applications.CtlIfOut `json:"ep_info"` //May need to change this to a pointer
	pendingCmds  map[string]*epCommand `json:"-"`
	Mutex        sync.Mutex            `json:"-"`
}

type epCommand struct {
	ep      *NcsiEP
	cmd     string
	fn      string
	outJSON []byte
	err     error
	op      applications.EPcmdType
}

func (cmd *epCommand) getOutFnam() string {
	return cmd.ep.epRoot() + "/output/" + cmd.fn
}

func (cmd *epCommand) getInFnam() string {
	return cmd.ep.epRoot() + "/input/" + cmd.fn
}

func (cmd *epCommand) getCmdBuf() []byte {
	return []byte(cmd.cmd)
}

func (cmd *epCommand) getOutJSON() []byte {
	return []byte(cmd.outJSON)
}

func (cmd *epCommand) checkOutFile() error {
	var tmp_stb syscall.Stat_t
	if err := syscall.Stat(cmd.getOutFnam(), &tmp_stb); err != nil {
		return err
	}

	if tmp_stb.Size > maxOutFileSize {
		return syscall.E2BIG
	}

	return nil
}

func (cmd *epCommand) loadOutfile() {
	if cmd.err = cmd.checkOutFile(); cmd.err != nil {
		return
	}

	// Try to read the file
	cmd.outJSON, cmd.err = ioutil.ReadFile(cmd.getOutFnam())
	if cmd.err != nil {
		logrus.Errorf("checkOutFile(): %s getOutFnam %s",
			cmd.err, cmd.getOutFnam())
	}
	return
}

// Makes a 'unique' filename for the command and adds it to the map
func (cmd *epCommand) prep() {
	if cmd.fn == "" {
		cmd.fn = "testncsiep_" +
			strconv.FormatInt(int64(os.Getpid()), 10) + "_" +
			strconv.FormatInt(int64(time.Now().Nanosecond()), 10)
	}
	cmd.fn = "lookout_" + cmd.fn
	cmd.cmd = cmd.cmd + "\nOUTFILE /" + cmd.fn + "\n"

	// Add the cmd into the endpoint's pending cmd map
	cmd.ep.addCmd(cmd)
}

func (cmd *epCommand) write() {
	cmd.err = ioutil.WriteFile(cmd.getInFnam(), cmd.getCmdBuf(), 0644)
	if cmd.err != nil {
		logrus.Errorf("ioutil.WriteFile(): %s", cmd.err)
		return
	}
	cmd.ep.LastRequest = time.Now()

	if logrus.IsLevelEnabled(logrus.DebugLevel) {
		logrus.Debugf("ev-submit: uuid=%s %s: %s",
			cmd.ep.Uuid, cmd.getInFnam(), cmd.cmd)

	} else if logrus.IsLevelEnabled(logrus.InfoLevel) {
		logrus.Infof("ev-submit: uuid=%s %s",
			cmd.ep.Uuid, cmd.getInFnam())
	}
}

func (cmd *epCommand) submit() {
	if err := cmd.ep.mayQueueCmd(); err == false {
		logrus.Error("Too many pending commands for endpoint: ",
			cmd.ep.Uuid)
		return
	}
	cmd.prep()
	cmd.write()
}

func (ep *NcsiEP) mayQueueCmd() bool {
	logrus.Debugf("uuid=%s pending=%d max=%d",
		ep.Uuid, len(ep.pendingCmds), maxPendingCmdsEP)

	// Enforces a max queue depth of 1 for internal scheduling logic,
	// while still allowing externally-triggered commands (e.g., via /v1/)
	// to be processed.
	if len(ep.pendingCmds) > 0 {
		logrus.Debugf("ep %s has %d pending cmds",
			ep.Uuid, len(ep.pendingCmds))

		if len(ep.pendingCmds) > 0 {
			if time.Since(ep.LastRequest) > time.Second*EPtimeoutSec {
				logrus.Debugf("ep %s has stale cmds (%f seconds), removing them from the queue",
					ep.Uuid, EPtimeoutSec)

				for x := range ep.pendingCmds {
					logrus.Info("remove cmd: ", ep.Uuid, x)
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
	_, exists := cmd.ep.pendingCmds[cmd.fn]
	if exists == false {
		cmd.ep.pendingCmds[cmd.fn] = cmd
	}
	cmd.ep.Mutex.Unlock()

	if exists == true {
		return syscall.EEXIST
	}

	return nil
}

func (ep *NcsiEP) removeCmd(cmdName string) *epCommand {
	ep.Mutex.Lock()
	cmd, ok := ep.pendingCmds[cmdName]
	if ok {
		delete(ep.pendingCmds, cmdName)
	}
	ep.Mutex.Unlock()

	return cmd
}

func (ep *NcsiEP) epRoot() string {
	return ep.Path
}

func (ep *NcsiEP) CtlCustomQuery(customCMD string, ID string) error {
	logrus.Infof("Custom query for endpoint %s: %s", ep.Uuid, customCMD)
	cmd := epCommand{ep: ep, cmd: customCMD, op: applications.CustomOp, fn: ID}
	cmd.submit()
	return cmd.err
}

func (ep *NcsiEP) update(ctlData applications.CtlIfOut) {
	ep.App.SetCtlIfOut(ctlData)
	ep.EPInfo = ep.App.GetCtlIfOut()
	ep.LastReport = time.Now()
	if !ep.Alive {
		ep.Alive = true
	}
}

func (ep *NcsiEP) Complete(cmdName string, output *[]byte) error {
	cmd := ep.removeCmd(cmdName)
	if cmd == nil {
		return syscall.ENOENT
	}
	cmd.loadOutfile()
	if cmd.err != nil {
		return cmd.err
	}
	//Add here to break for custom command
	if cmd.op == applications.CustomOp {
		logrus.Debug("Custom command identified: ", cmdName)
		*output = cmd.getOutJSON()
		return nil
	}

	var err error
	var ctlifout applications.CtlIfOut
	if err = json.Unmarshal(cmd.getOutJSON(), &ctlifout); err != nil {
		if ute, ok := err.(*json.UnmarshalTypeError); ok {
			logrus.Errorf("UnmarshalTypeError %v - %v - %v\n",
				ute.Value, ute.Type, ute.Offset)
		} else {
			logrus.Errorf("Other error: %s\n", err)
			logrus.Errorf("Contents: %s\n", string(cmd.getOutJSON()))
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
		if strings.Contains(file.Name(), "lookout") {
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
			logrus.Debugf("Endpoint %s timed out\n", ep.Uuid)
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
	logrus.Info("GetAppType for: ", ep.Uuid)

	cmd := epCommand{
		ep:  ep,
		cmd: "GET /.*",
		op:  applications.IdentifyOp,
		fn:  "lookout_identify",
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logrus.Fatal("Failed to create watcher:", err)
	}
	defer watcher.Close()

	outputDir := cmd.ep.epRoot() + "/output"
	err = watcher.Add(outputDir)
	if err != nil {
		if errors.Is(err, syscall.ENOSPC) {
			logrus.Error("Failed to add directory to watcher due to no space left on device:", err)
		} else {
			logrus.Fatal("Failed to add directory to watcher:", err)
		}
	}

	done := make(chan bool)
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					logrus.Trace("Watcher events channel closed")
					return
				}
				logrus.Debug("Event received:", event)
				if (event.Op&fsnotify.Write == fsnotify.Write ||
					event.Op&fsnotify.Create == fsnotify.Create) && event.Name == outputDir+"/"+cmd.fn {
					logrus.Trace("File modified:", event.Name)
					done <- true
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					logrus.Trace("Watcher errors channel closed")
					return
				}
				logrus.Error("Watcher error:", err)
			}
		}
	}()

	cmd.submit()

	select {
	case <-done:
		logrus.Debug("File write detected")
	case <-time.After(1 * time.Second): // Timeout after 1 seconds
		logrus.Warn("Timeout waiting for file write")
	}

	c := ep.removeCmd(cmd.fn)
	if c == nil {
		logrus.Error("removeCmd returned nil")
		return
	}

	c.loadOutfile()
	output := c.getOutJSON()
	ep.App, err = applications.DetermineApp(output)
	if err != nil {
		logrus.Error("DetermineApp for ", ep.Uuid, " failed:", err)
	}
	logrus.Info("App type for ", ep.Uuid, " determined: ", ep.App.GetAppName())
}
