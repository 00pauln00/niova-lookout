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
	if err != nil {
		return
	}

	//temp file exclusion
	if strings.Contains(cmpstr, ".") {
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
			Uuid:         uuid,
			Path:         h.CTLPath + "/" + uuid.String(),
			Name:         "r-a4e1",
			NiovaSvcType: "raft",
			LastReport:   time.Now(),
			LastClear:    time.Now(),
			Alive:        true,
			pendingCmds:  make(map[string]*epCommand),
		}
		newlns.GetAppType()
		//TODO: populate the app type, determine what needs put in App and what needs put in NcsiEP
		newlns.App.SetUUID(uuid)

		if err := h.EpWatcher.Add(newlns.Path + "/output"); err != nil {
			logrus.Fatal("Watcher.Add() failed:", err)
		}

		h.Epc.UpdateEpMap(uuid, &newlns)
		logrus.Debugf("added: %+v\n", newlns)
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
	err = h.init()
	if err != nil {
		logrus.Debug("Lookout Init - ", err)
		return err
	}

	//Start monitoring
	err = h.monitor()
	if err != nil {
		logrus.Debug("Lookout Monitor - ", err)
		return err
	}

	return nil
}
