package applications

import (
	"bytes"
	"common/serviceDiscovery"
	"encoding/gob"
	"fmt"

	"github.com/00pauln00/niova-lookout/pkg/prometheusHandler"
	"github.com/00pauln00/niova-lookout/pkg/requestResponseLib"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

type Pmdb struct {
	AppType       string
	Cmd           string
	Op            EPcmdType
	EPInfo        CtlIfOut
	RaftRootEntry []RaftInfo
}

type RaftInfo struct {
	RaftUUID                 string    `json:"raft-uuid"`
	PeerUUID                 string    `json:"peer-uuid"`
	VotedForUUID             string    `json:"voted-for-uuid"`
	LeaderUUID               string    `json:"leader-uuid"`
	State                    string    `json:"state"`
	FollowerReason           string    `json:"follower-reason"`
	ClientRequests           string    `json:"client-requests"`
	Term                     int       `json:"term" type:"gauge" metric:"PMDB_term"`
	CommitIdx                int       `json:"commit-idx" type:"gauge" metric:"PMDB_commitIdx"`
	LastApplied              int       `json:"last-applied" type:"gauge" metric:"PMDB_last_applied"`
	LastAppliedCumulativeCrc int       `json:"last-applied-cumulative-crc" type:"gauge" metric:"PMDB_last_applied_cumulative_crc"`
	NewestEntryIdx           int       `json:"newest-entry-idx" type:"gauge" metric:"PMDB_newest_entry_idx"`
	NewestEntryTerm          int       `json:"newest-entry-term" type:"gauge" metric:"PMDB_newest_entry_term"`
	NewestEntryDataSize      int       `json:"newest-entry-data-size" type:"gauge" metric:"PMDB_newest_entry_data_size"`
	NewestEntryCrc           int       `json:"newest-entry-crc" type:"gauge" metric:"PMDB_newest_entry_crc"`
	DevReadLatencyUsec       Histogram `json:"dev-read-latency-usec" type:"histogram" metric:"dev_read_latency_usec"`
	DevWriteLatencyUsec      Histogram `json:"dev-write-latency-usec" type:"histogram" metric:"dev_write_latency_usec"`
	FollowerStats            []struct {
		PeerUUID    string `json:"peer-uuid"`
		LastAckMs   int    `json:"ms-since-last-ack"`
		LastAck     Time   `json:"last-ack"`
		NextIdx     int    `json:"next-idx"`
		PrevIdxTerm int    `json:"prev-idx-term"`
	} `json:"follower-stats,omitempty"`
	CommitLatencyMsec Histogram `json:"commit-latency-msec"`
	ReadLatencyMsec   Histogram `json:"read-latency-msec"`
}

// requestPMDB sends a read request to the storage client and returns the response.
func RequestPMDB(key string) ([]byte, error) {
	request := requestResponseLib.KVRequest{
		Operation: "read",
		Key:       key,
	}
	var requestByte bytes.Buffer
	enc := gob.NewEncoder(&requestByte)
	if err := enc.Encode(request); err != nil {
		return nil, err // Handle the error appropriately
	}
	responseByte, err := (&serviceDiscovery.ServiceDiscoveryHandler{}).Request(requestByte.Bytes(), "", false)
	return responseByte, err
}

// loadPMDBLabelMap updates the provided labelMap with information from a RaftInfo struct.
func LoadPMDBLabelMap(labelMap map[string]string, raftEntry RaftInfo) map[string]string {
	labelMap["STATE"] = raftEntry.State
	labelMap["RAFT_UUID"] = raftEntry.RaftUUID
	labelMap["VOTED_FOR"] = raftEntry.VotedForUUID
	labelMap["FOLLOWER_REASON"] = raftEntry.FollowerReason
	labelMap["CLIENT_REQS"] = raftEntry.ClientRequests

	return labelMap
}

func (p Pmdb) getDetectInfo() (string, EPcmdType) {
	p.GetCmdStr()
	p.GetOp()
	return p.Cmd, p.Op
}

func (p Pmdb) GetCmdStr() {
	p.Cmd = "GET /raft_root_entry/.*/.*"
}

func (p Pmdb) GetOp() {
	p.Op = RaftInfoOp
}

func (p Pmdb) UpdateCtlIfOut(c *CtlIfOut) CtlIfOut {
	p.EPInfo.RaftRootEntry = c.RaftRootEntry
	logrus.Debugf("update-raft %+v \n", c.RaftRootEntry)
	return p.EPInfo
}
