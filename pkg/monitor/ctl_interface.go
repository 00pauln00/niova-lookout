package monitor // niova control interface

import (
	//	"math/rand"
	"encoding/json"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/00pauln00/niova-lookout/pkg/monitor/applications"
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
	Uuid         uuid.UUID             `json:"-"`
	Path         string                `json:"-"`
	Name         string                `json:"name"`
	NiovaSvcType string                `json:"type"`
	Port         int                   `json:"port"`
	LastReport   time.Time             `json:"-"`
	LastClear    time.Time             `json:"-"`
	Alive        bool                  `json:"responsive"`
	EPInfo       applications.CtlIfOut `json:"ep_info"`
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

func msleep() {
	C.usleep(1000)
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
		logrus.Errorf("checkOutFile(): %s getOutFnam %s", cmd.err, cmd.getOutFnam())
	}
	return
}

// Makes a 'unique' filename for the command and adds it to the map
func (cmd *epCommand) prep() {
	if cmd.fn == "" {
		cmd.fn = "lookout_ncsiep_" + strconv.FormatInt(int64(os.Getpid()), 10) +
			"_" + strconv.FormatInt(int64(time.Now().Nanosecond()), 10)
	}
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
}

func (cmd *epCommand) submit() {
	if err := cmd.ep.mayQueueCmd(); err == false {
		return
	}
	cmd.prep()
	cmd.write()
}

func (ep *NcsiEP) mayQueueCmd() bool {
	if len(ep.pendingCmds) < maxPendingCmdsEP {
		return true
	}
	return false
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
	cmd := epCommand{ep: ep, cmd: customCMD, op: applications.Custom, fn: ID}
	cmd.submit()
	return cmd.err
}

func (ep *NcsiEP) update(ctlData *applications.CtlIfOut, app applications.Application) {
	ep.EPInfo = app.UpdateCtlIfOut(ctlData)
	ep.LastReport = time.Now()
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
	if cmd.op == applications.Custom {
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
	app := applications.CreateAppByOp(ctlifout, cmd.op)
	ep.update(&ctlifout, app)

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

func (ep *NcsiEP) Remove() {
	//Remove stale ctl files
	input_path := ep.Path + "/input/"
	ep.removeFiles(input_path)
	//output files
	output_path := ep.Path + "/output/"
	ep.removeFiles(output_path)
}

func (ep *NcsiEP) Detect(appType string) error {
	if ep.Alive {
		var err error
		//test
		cmdStr, op := applications.Detect(appType, false)
		cmd := epCommand{ep: ep, cmd: cmdStr, op: op}
		cmd.submit()
		if cmd.err != nil && cmd.op == applications.SystemInfoOp {
			cmdStr, op = applications.Detect(appType, true)
			cmd = epCommand{ep: ep, cmd: cmdStr, op: op}
			cmd.submit()
			return cmd.err
		}

		if time.Since(ep.LastReport) > time.Second*EPtimeoutSec {
			ep.Alive = false
		}
		return err
	}
	return nil
}

// is this unused?
func (ep *NcsiEP) Check() error {
	return nil
}
