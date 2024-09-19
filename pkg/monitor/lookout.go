package monitor

import (
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

//var HttpPort int

type LookoutHandler struct {
	PromPath  string
	HttpPort  int
	run       bool
	CTLPath   string
	Statb     syscall.Stat_t
	Epc       *EPContainer
	EpWatcher *fsnotify.Watcher
}

func (h *LookoutHandler) monitor() error {
	var err error = nil

	//Question: when would run become false? as is we never set it to false
	for h.run == true {
		var tmp_stb syscall.Stat_t
		var sleepTime time.Duration
		err = syscall.Stat(h.CTLPath, &tmp_stb)
		if err != nil {
			logrus.Errorf("syscall.Stat('%s'): %s", h.CTLPath, err)
			break
		}

		if tmp_stb.Mtim != h.Statb.Mtim {
			h.Statb = tmp_stb
			h.scan()
		}

		h.Epc.LivenessCheck()

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

func (h *LookoutHandler) writePromPath() error {
	f, err := os.OpenFile(h.PromPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, err = f.WriteString(strconv.Itoa(h.HttpPort))
	if err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}

	return nil
}

func (h *LookoutHandler) epOutputWatcher() {
	for {
		select {
		case event := <-h.EpWatcher.Events:

			if event.Op == fsnotify.Create {
				h.processInotifyEvent(&event)
			}

			// watch for errors
		case err := <-h.EpWatcher.Errors:
			logrus.Error("EpWatcher pipe: ", err)

		}
	}
}

func (h *LookoutHandler) processInotifyEvent(event *fsnotify.Event) {
	splitPath := strings.Split(event.Name, "/")
	cmpstr := splitPath[len(splitPath)-1]
	uuid, err := uuid.Parse(splitPath[len(splitPath)-3])
	logrus.Tracef("cmpstr: %s, uuid: %s\n", cmpstr, uuid.String())
	if err != nil {
		logrus.Error("uuid.Parse(): ", err)
		return
	}

	//temp file exclusion
	if strings.Contains(cmpstr, ".") {
		logrus.Trace("Skipping temp file")
		return
	}

	h.Epc.HandleHttpQuery(cmpstr, uuid)
	h.Epc.ProcessEndpoint(cmpstr, uuid, event)
}

func (h *LookoutHandler) scan() {
	files, err := ioutil.ReadDir(h.CTLPath)
	if err != nil {
		logrus.Fatal(err)
	}

	for _, file := range files {
		// Need to support removal of stale items
		if uuid, err := uuid.Parse(file.Name()); err == nil {
			if (h.Epc.MonitorUUID == uuid.String()) || (h.Epc.MonitorUUID == "*") {
				h.tryAdd(uuid)
			}
		}
	}
}

func (h *LookoutHandler) tryAdd(uuid uuid.UUID) {
	lns := h.Epc.Lookup(uuid)
	if lns == nil {
		logrus.Trace("Adding new endpoint, UUID: ", uuid)
		newlns := NcsiEP{
			Uuid:        uuid,
			Path:        h.CTLPath + "/" + uuid.String(),
			LastReport:  time.Now(),
			LastClear:   time.Now(),
			Alive:       true,
			pendingCmds: make(map[string]*epCommand),
		}
		newlns.GetAppType()
		newlns.App.SetUUID(uuid)
		newlns.NiovaSvcType = newlns.App.GetAppName()

		if err := h.EpWatcher.Add(newlns.Path + "/output"); err != nil {
			logrus.Fatal("Watcher.Add() failed:", err)
		}

		h.Epc.UpdateEpMap(uuid, &newlns)
		logrus.Debugf("added: UUID=%s, Path=%s, Alive=%t, NiovaSvcType=%s\n", newlns.Uuid, newlns.Path, newlns.Alive, newlns.NiovaSvcType)
	}
}

func (h *LookoutHandler) init() error {
	// Check the provided endpoint root path
	err := syscall.Stat(h.CTLPath, &h.Statb)
	if err != nil {
		return err
	}

	// Set path (Xxx still need to check if this is a directory or not)

	err = h.Epc.InitializeEpMap()
	if err != nil {
		return err
	}

	h.EpWatcher, err = fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	h.run = true

	go h.epOutputWatcher()

	h.scan()

	return nil
}

func (h *LookoutHandler) Start() error {
	var err error
	h.Epc.HttpQuery = make(map[string](chan []byte))

	err = h.writePromPath()
	if err != nil {
		return err
	}
	//Setup lookout
	logrus.Info("Initializing Lookout")
	err = h.init()
	if err != nil {
		logrus.Debug("Lookout Init - ", err)
		return err
	}

	//Start monitoring
	logrus.Info("Starting Monitor")
	err = h.monitor()
	if err != nil {
		logrus.Debug("Lookout Monitor - ", err)
		return err
	}

	return nil
}
