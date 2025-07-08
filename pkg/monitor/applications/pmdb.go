package applications

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"net/http"

	serviceDiscovery "github.com/00pauln00/niova-pumicedb/go/pkg/utils/servicediscovery"

	"github.com/00pauln00/niova-lookout/pkg/prometheusHandler"
	"github.com/00pauln00/niova-lookout/pkg/requestResponseLib"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

type Pmdb struct {
	uuid          uuid.UUID
	EPInfo        CtlIfOut
	RaftRootEntry []RaftInfo
	membership    map[string]bool
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
func (p *Pmdb) LoadPMDBLabelMap(labelMap map[string]string) map[string]string {
	labelMap["STATE"] = p.RaftRootEntry[0].State
	labelMap["RAFT_UUID"] = p.RaftRootEntry[0].RaftUUID
	labelMap["VOTED_FOR"] = p.RaftRootEntry[0].VotedForUUID
	labelMap["FOLLOWER_REASON"] = p.RaftRootEntry[0].FollowerReason
	labelMap["CLIENT_REQS"] = p.RaftRootEntry[0].ClientRequests

	return labelMap
}

func (p *Pmdb) SetMembership(membership map[string]bool) {
	p.membership = membership
}

func (p *Pmdb) GetMembership() map[string]bool {
	return p.membership
}

func (p *Pmdb) GetAppDetectInfo(b bool) (string, EPcmdType) {
	if b {
		return "GET /raft_root_entry/.*/.*", RaftInfoOp
	} else {
		syst := &Syst{}
		return syst.GetAppDetectInfo(b)
	}
}

func (p *Pmdb) GetAppName() string {
	return "PMDB"
}

func (p *Pmdb) SetCtlIfOut(c CtlIfOut) {
	p.EPInfo.RaftRootEntry = c.RaftRootEntry
	logrus.Debugf("update-raft %+v \n", c.RaftRootEntry)
}

func (p *Pmdb) GetCtlIfOut() CtlIfOut {
	return p.EPInfo
}

func (p *Pmdb) SetUUID(uuid uuid.UUID) {
	p.uuid = uuid
}

func (p *Pmdb) GetUUID() uuid.UUID {
	return p.uuid
}

func (p *Pmdb) Parse(labelMap map[string]string, w http.ResponseWriter, r *http.Request) {
	var output string
	labelMap["PMDB_UUID"] = p.GetUUID().String()
	labelMap["TYPE"] = p.GetAppName()
	// Loading labelMap with PMDB data
	labelMap = p.LoadPMDBLabelMap(labelMap)
	// Parsing exported data
	output += prometheusHandler.GenericPromDataParser(p.RaftRootEntry[0], labelMap)
	// Parsing membership data
	output += p.parseMembershipPrometheus()
	// Parsing follower data
	output += p.getFollowerStats()
	// Parsing system info
	output += prometheusHandler.GenericPromDataParser(p.EPInfo.SysInfo, labelMap)
	fmt.Fprintf(w, "%s", output)
}

func (p *Pmdb) getFollowerStats() string {
	var output string
	for indx := range p.RaftRootEntry[0].FollowerStats {
		UUID := p.RaftRootEntry[0].FollowerStats[indx].PeerUUID
		NextIdx := p.RaftRootEntry[0].FollowerStats[indx].NextIdx
		PrevIdxTerm := p.RaftRootEntry[0].FollowerStats[indx].PrevIdxTerm
		LastAckMs := p.RaftRootEntry[0].FollowerStats[indx].LastAckMs
		output += "\n" + fmt.Sprintf(`follower_stats{uuid="%s"next_idx="%d"prev_idx_term="%d"}%d`, UUID, NextIdx, PrevIdxTerm, LastAckMs)
	}
	return output
}

func (p *Pmdb) parseMembershipPrometheus() string {
	var output string
	for name, isAlive := range p.membership {
		var adder, status string
		if isAlive {
			adder = "1"
			status = "online"
		} else {
			adder = "0"
			status = "offline"
		}
		if p.GetUUID().String() == name {
			output += "\n" + fmt.Sprintf(`node_status{uuid="%s"state="%s"status="%s"raftUUID="%s"} %s`, name, p.RaftRootEntry[0].State, status, p.RaftRootEntry[0].RaftUUID, adder)
		} else {
			// since we do not know the state of other nodes
			output += "\n" + fmt.Sprintf(`node_status{uuid="%s"status="%s"raftUUID="%s"} %s`, name, status, p.RaftRootEntry[0].RaftUUID, adder)
		}

	}
	return output
}
