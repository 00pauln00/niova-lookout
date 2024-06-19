package lookout // niova control interface

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

type Time struct {
	WrappedTime time.Time `json:"time"`
}

type SystemInfo struct {
	CurrentTime             Time      `json:"current_time"`
	StartTime               Time      `json:"start_time"`
	Pid                     int       `json:"pid"`
	UUID                    uuid.UUID `json:"uuid"`
	CtlInterfacePath        string    `json:"ctl_interface_path"`
	CommandLine             string    `json:"command_line"`
	UtsNodename             string    `json:"uts.nodename"`
	UtsSysname              string    `json:"uts.sysname"`
	UtsRelease              string    `json:"uts.release"`
	UtsVersion              string    `json:"uts.version"`
	UtsMachine              string    `json:"uts.machine"`
	RusageUserCPUTimeUsed   float64   `json:"rusage.user_cpu_time_used" type:"gauge" metric:"SYSINFO_user_cpu_time"`
	RusageSystemCPUTimeUsed float64   `json:"rusage.system_cpu_time_used" type:"gauge" metric:"SYSINFO_sys_cpu_time"`
	RusageMaxRss            int       `json:"rusage.max_rss" type:"counter" metric:"SYSINFO_max_rss"`
	RusageMinFault          int       `json:"rusage.min_fault" type:"counter" metric:"SYSINFO_min_fault"`
	RusageMajFault          int       `json:"rusage.maj_fault" type:"counter" metric:"SYSINFO_maj_fault"`
	RusageInBlock           int       `json:"rusage.in_block" type:"counter" metric:"SYSINFO_in_block_usage"`
	RusageOutBlock          int       `json:"rusage.out_block" type:"counter" metric:"SYSINFO_out_block_usage"`
	RusageVolCtsw           int       `json:"rusage.vol_ctsw" type:"gauge" metric:"SYSINFO_vol_ctsw"`
	RusageInvolCtsw         int       `json:"rusage.invol_ctsw" type:"gauge" metric:"SYSINFO_in_vol_ctsw"`
}

type NISDInfo struct {
	UUID                  string    `json:"uuid"`
	DevPath               string    `json:"dev-path"`
	ServerMode            bool      `json:"server-mode"`
	Status                string    `json:"status"`
	ReadBytes             int       `json:"dev-bytes-read" type:"counter" metric:"nisd_dev_read_bytes"`
	WriteBytes            int       `json:"dev-bytes-write" type:"counter" metric:"nisd_dev_write_bytes"`
	NetRecvBytes          int       `json:"net-bytes-recv" type:"counter" metric:"nisd_net_bytes_recv"`
	NetSendBytes          int       `json:"net-bytes-send" type:"counter" metric:"nisd_net_bytes_send"`
	DevRdSize             Histogram `json:"dev-rd-size" type:"histogram" metric:"nisd_dev_rd_size"`
	DevWrSize             Histogram `json:"dev-wr-size" type:"histogram" metric:"nisd_dev_wr_size"`
	NetRecvSize           Histogram `json:"net-recv-size" type:"histogram" metric:"nisd_net_recv_size"`
	NetSendSize           Histogram `json:"net-send-size" type:"histogram" metric:"nisd_net_send_size"`
	DevRdLatencyUsec      Histogram `json:"dev-rd-latency-usec" type:"histogram" metric:"nisd_dev_rd_latency_usec"`
	DevWrLatencyUsec      Histogram `json:"dev-wr-latency-usec" type:"histogram" metric:"nisd_dev_wr_latency_usec"`
	NetRecvLatencyUsec    Histogram `json:"net-recv-latency-usec" type:"histogram" metric:"nisd_net_recv_latency_usec"`
	NetSendLatencyUsec    Histogram `json:"net-send-latency-usec" type:"histogram" metric:"nisd_net_send_latency_usec"`
	IOReqLatencyUsec      Histogram `json:"io-req-latency-usec" type:"histogram" metric:"nisd_io_req_latency_usec"`
	NIOPWaitLatencyUsec   Histogram `json:"niop-wait-latency-usec" type:"histogram" metric:"nisd_niop_wait_latency_usec"`
	NIOPUringLatencyUsec  Histogram `json:"niop-uring-latency-usec" type:"histogram" metric:"nisd_niop_uring_latency_usec"`
	NumSubSQEs            Histogram `json:"num-sub-sqes" type:"histogram" metric:"nisd_num_sub_sqes"`
	NumWaitCQEs           Histogram `json:"num-wait-cqes" type:"histogram" metric:"nisd_num_wait_cqes"`
	UringNetResubmissions Histogram `json:"uring-net-resubmissions" type:"histogram" metric:"nisd_uring_net_resubmissions"`
	UringDevResubmissions Histogram `json:"uring-dev-resubmissions" type:"histogram" metric:"nisd_uring_dev_resubmissions"`
	UringFixedBuf         Histogram `json:"uring-fixed-buf" type:"histogram" metric:"nisd_uring_fixed_buf"`
	UringNumIOVs          Histogram `json:"uring-num-iovs" type:"histogram" metric:"nisd_uring_num_iovs"`
}

type NISDRoot struct {
	UUID                    string    `json:"uuid"`
	InstanceUUID            string    `json:"instance-uuid"`
	Status                  string    `json:"status"`
	VBlockRead              int       `json:"vblks-read" type:"counter" metric:"nisd_vblk_read"`
	VBlockHoleRead          int       `json:"vblks-hole-read" type:"gauge" metric:"nisd_vblk_hole_read"`
	VBlockWritten           int       `json:"vblks-written" type:"counter" metric:"nisd_vblk_write"`
	S3SyncSendBytes         int       `json:"s3-sync-send-bytes" type:"gauge" metric:"nisd_s3_sync_send_bytes"`
	S3SyncVBlksRead         int       `json:"s3-sync-vblks-read" type:"gauge" metric:"nisd_s3_sync_vblks_read"`
	MetablockSectorsRead    int       `json:"metablock-sectors-read" type:"counter" metric:"nisd_metablock_sectors_read"`
	MetablockSectorsWritten int       `json:"metablock-sectors-written" type:"counter" metric:"nisd_metablock_sectors_written"`
	MetablockCacheHits      int       `json:"metablock-cache-hits" type:"counter" metric:"nisd_metablock_cache_hits"`
	MetablockCacheMisses    int       `json:"metablock-cache-misses" type:"counter" metric:"nisd_metablock_cache_misses"`
	ChunkMergeQLen          int       `json:"chunk-mergeq-len" type:"gauge" metric:"nisd_chunk_mergeq_len"`
	NumReservedPblks        int       `json:"num-reserved-pblks" type:"counter" metric:"nisd_num_reserved_pblks"`
	NumReservedPblksUsed    int       `json:"num-reserved-pblks-used" type:"counter" metric:"nisd_num_reserved_pblks_used"`
	NumPblks                int       `json:"num-pblks" type:"counter" metric:"nisd_num_pblks"`
	NumPblksUsed            int       `json:"num-pblks-used" type:"counter" metric:"nisd_num_pblks_used"`
	ReleaseObjBusy          int       `json:"release-obj-busy" type:"gauge" metric:"nisd_release_obj_busy"`
	ReleaseXtraObjBusy      int       `json:"release-xtra-obj-busy" type:"gauge" metric:"nisd_release_xtra_obj_busy"`
	ReleaseObjTotal         int       `json:"release-obj-total" type:"counter" metric:"nisd_release_obj_total"`
	ReleaseXtraObjTotal     int       `json:"release-xtra-obj-total" type:"counter" metric:"nisd_release_xtra_obj_total"`
	ShallowMergeInProgress  int       `json:"shallow-merge-in-progress" type:"gauge" metric:"nisd_shallow_merge_in_progress"`
	AltName                 string    `json:"alt-name"`
	VBlkMetaReadSectors     Histogram `json:"vblk-meta-read-sectors" type:"histogram" metric:"nisd_vblk_meta_read_sectors"`
	MCIBMetaReadSectors     Histogram `json:"mcib-meta-read-sectors" type:"histogram" metric:"nisd_mcib_meta_read_sectors"`
	FullCompactionMsec      Histogram `json:"full-compaction-msec" type:"histogram" metric:"nisd_full_compaction_msec"`
	ShallowCompactionMsec   Histogram `json:"shallow-compaction-msec" type:"histogram" metric:"nisd_shallow_compaction_msec"`
}

type NISDChunkInfo struct {
	VdevUUID                   string `json:"vdev-uuid"`
	Number                     int    `json:"number" type:"gauge" metric:"nisd_chunk_number"`
	Tier                       int    `json:"tier" type:"gauge" metric:"nisd_chunk_tier"`
	Type                       int    `json:"type" type:"gauge" metric:"nisd_chunk_type"`
	NumDataPblks               int    `json:"num-data-pblks" type:"counter" metric:"nisd_chunk_num_data_pblks"`
	NumMetaPblks               int    `json:"num-meta-pblks" type:"counter" metric:"nisd_chunk_num_meta_pblks"`
	NumMcibPblks               int    `json:"num-mcib-pblks" type:"counter" metric:"nisd_chunk_num_mcib_pblks"`
	NumReservedMetaPblks       int    `json:"num-reserved-meta-pblks" type:"counter" metric:"nisd_chunk_num_reserved_meta_pblks"`
	NumTypical2ReservedMbLinks int    `json:"num-typical-2-reserved-mb-links" type:"counter" metric:"nisd_chunk_num_typical_2_reserved_mb_links"`
	VblksRead                  int    `json:"vblks-read" type:"counter" metric:"nisd_chunk_vblks_read"`
	VblksWritten               int    `json:"vblks-written" type:"counter" metric:"nisd_chunk_vblks_written"`
	VblksPeerSent              int    `json:"vblks-peer-sent" type:"counter" metric:"nisd_chunk_vblks_peer_sent"`
	VblksPeerRecvd             int    `json:"vblks-peer-recvd" type:"counter" metric:"nisd_chunk_vblks_peer_recvd"`
	MergeShallowCnt            int    `json:"merge-shallow-cnt" type:"counter" metric:"nisd_chunk_merge_shallow_cnt"`
	MergeShallowStatus         string `json:"merge-shallow-status"`
	MergeFullCnt               int    `json:"merge-full-cnt" type:"counter" metric:"nisd_chunk_merge_full_cnt"`
	MergeFullCompletedCnt      int    `json:"merge-full-completed-cnt" type:"counter" metric:"nisd_chunk_merge_full_completed_cnt"`
	MergeFullStatus            string `json:"merge-full-status"`
	MergeFence                 int    `json:"merge-fence" type:"counter" metric:"nisd_chunk_merge_fence"`
	ClientRecoverySeqno        int    `json:"client-recovery-seqno" type:"counter" metric:"nisd_chunk_client_recovery_seqno"`
	MetablockSeqn              int    `json:"metablock-seqno" type:"counter" metric:"nisd_chunk_metablock_seqno"`
	S3SyncSeqno                int    `json:"s3-sync-seqno" type:"counter" metric:"nisd_chunk_s3_sync_seqno"`
	S3SyncState                string `json:"s3-sync-state"`
	NumCme                     int    `json:"num-cme" type:"counter" metric:"nisd_chunk_num_cme"`
	RefCnt                     int    `json:"ref-cnt" type:"counter" metric:"nisd_chunk_ref_cnt"`
	OooMbSyncCnt               int    `json:"ooo-mb-sync-cnt" type:"counter" metric:"nisd_chunk_ooo_mb_sync_cnt"`
	OooMbSyncNewMpblkCnt       int    `json:"ooo-mb-sync-new-mpblk-cnt" type:"counter" metric:"nisd_chunk_ooo_mb_sync_new_mpblk_cnt"`
	McibHits                   int    `json:"mcib-hits" type:"counter" metric:"nisd_chunk_mcib_hits"`
	McibMisses                 int    `json:"mcib-misses" type:"counter" metric:"nisd_chunk_mcib_misses"`
	McibSectorReads            int    `json:"mcib-sector-reads" type:"counter" metric:"nisd_chunk_mcib_sector_reads"`
	McibSectorWrites           int    `json:"mcib-sector-writes" type:"counter" metric:"nisd_chunk_mcib_sector_writes"`
	StashPblk                  int    `json:"stash-pblk" type:"counter" metric:"nisd_chunk_stash_pblk"`
	StashPblkStates            string `json:"stash-pblk-states"`
}

type Histogram struct {
	Num1       int `json:"1,omitempty"`
	Num2       int `json:"2,omitempty"`
	Num4       int `json:"4,omitempty"`
	Num8       int `json:"8,omitempty"`
	Num16      int `json:"16,omitempty"`
	Num32      int `json:"32,omitempty"`
	Num64      int `json:"64,omitempty"`
	Num128     int `json:"128,omitempty"`
	Num256     int `json:"256,omitempty"`
	Num512     int `json:"512,omitempty"`
	Num1024    int `json:"1024,omitempty"`
	Num2048    int `json:"2048,omitempty"`
	Num4096    int `json:"4096,omitempty"`
	Num8192    int `json:"8192,omitempty"`
	Num16384   int `json:"16384,omitempty"`
	Num32768   int `json:"32768,omitempty"`
	Num65536   int `json:"65536,omitempty"`
	Num131072  int `json:"131072,omitempty"`
	Num262144  int `json:"262144,omitempty"`
	Num524288  int `json:"524288,omitempty"`
	Num1048576 int `json:"1048576,omitempty"`
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

type CtlIfOut struct {
	SysInfo         SystemInfo      `json:"system_info,omitempty"`
	RaftRootEntry   []RaftInfo      `json:"raft_root_entry,omitempty"`
	NISDInformation []NISDInfo      `json:"niorq_mgr_root_entry,omitempty"`
	NISDRootEntry   []NISDRoot      `json:"nisd_root_entry,omitempty"`
	NISDChunk       []NISDChunkInfo `json:"nisd_chunks,omitempty"`
}

type NcsiEP struct {
	Uuid         uuid.UUID             `json:"-"`
	Path         string                `json:"-"`
	Name         string                `json:"name"`
	NiovaSvcType string                `json:"type"`
	Port         int                   `json:"port"`
	LastReport   time.Time             `json:"-"`
	LastClear    time.Time             `json:"-"`
	Alive        bool                  `json:"responsive"`
	EPInfo       CtlIfOut              `json:"ep_info"`
	pendingCmds  map[string]*epCommand `json:"-"`
	Mutex        sync.Mutex            `json:"-"`
}

type EPcmdType uint32

const (
	RaftInfoOp   EPcmdType = 1
	SystemInfoOp EPcmdType = 2
	NISDInfoOp   EPcmdType = 3
	Custom       EPcmdType = 4
)

type epCommand struct {
	ep      *NcsiEP
	cmd     string
	fn      string
	outJSON []byte
	err     error
	op      EPcmdType
}

// XXX this can be replaced with: func Trim(s string, cutset string) string
func chompQuotes(data []byte) []byte {
	s := string(data)

	// Check for quotes
	if len(s) > 0 {
		if s[0] == '"' {
			s = s[1:]
		}
		if s[len(s)-1] == '"' {
			s = s[:len(s)-1]
		}
	}

	return []byte(s)
}

// custom UnmarshalJSON method used for handling various timestamp formats.
func (t *Time) UnmarshalJSON(data []byte) error {
	var err error

	data = chompQuotes(data)

	if err = json.Unmarshal(data, t.WrappedTime); err == nil {
		return nil
	}
	const layout = "Mon Jan 02 15:04:05 MST 2006"

	t.WrappedTime, err = time.Parse(layout, string(data))

	return err
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
		cmd.fn = "lookout_ncsiep_" + strconv.FormatInt(int(os.Getpid()), 10) +
			"_" + strconv.FormatInt(int(time.Now().Nanosecond()), 10)
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

func (ep *NcsiEP) getRaftinfo() error {
	cmd := epCommand{ep: ep, cmd: "GET /raft_root_entry/.*/.*",
		op: RaftInfoOp}
	cmd.submit()

	return cmd.err
}

func (ep *NcsiEP) getSysinfo() error {
	cmd := epCommand{ep: ep, cmd: "GET /system_info/.*", op: SystemInfoOp}
	cmd.submit()
	return cmd.err
}

func (ep *NcsiEP) getNISDinfo() error {
	cmd := epCommand{ep: ep, cmd: "GET /.*/.*/.*", op: NISDInfoOp}
	cmd.submit()
	return cmd.err
}

func (ep *NcsiEP) CtlCustomQuery(customCMD string, ID string) error {
	cmd := epCommand{ep: ep, cmd: customCMD, op: Custom, fn: ID}
	cmd.submit()
	return cmd.err
}

func (ep *NcsiEP) update(ctlData *CtlIfOut, op EPcmdType) {
	switch op {
	case RaftInfoOp:
		ep.EPInfo.RaftRootEntry = ctlData.RaftRootEntry
		logrus.Debugf("update-raft %+v \n", ctlData.RaftRootEntry)
	case SystemInfoOp:
		ep.EPInfo.SysInfo = ctlData.SysInfo
		//ep.LastReport = ep.EPInfo.SysInfo.CurrentTime.WrappedTime
		logrus.Debugf("update-sys %+v \n", ctlData.SysInfo)
	case NISDInfoOp:
		//update
		ep.EPInfo.NISDInformation = ctlData.NISDInformation
		ep.EPInfo.NISDRootEntry = ctlData.NISDRootEntry
		ep.EPInfo.SysInfo = ctlData.SysInfo
		ep.EPInfo.NISDChunk = ctlData.NISDChunk
	default:
		logrus.Debugf("invalid op=%d \n", op)
	}
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
	if cmd.op == Custom {
		*output = cmd.getOutJSON()
		return nil
	}

	var err error
	var ctlifout CtlIfOut
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
	ep.update(&ctlifout, cmd.op)

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
		switch appType {
		case "NISD":
			ep.getNISDinfo()
		case "PMDB":
			err = ep.getSysinfo()
			if err == nil {
				err = ep.getRaftinfo()
			}

		}

		if time.Since(ep.LastReport) > time.Second*EPtimeoutSec {
			ep.Alive = false
		}
		return err
	}
	return nil
}

func (ep *NcsiEP) Check() error {
	return nil
}
