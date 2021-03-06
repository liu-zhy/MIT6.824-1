package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, Term, isleader)
//   start agreement on a new log entry
// rf.GetState() (Term, isLeader)
//   ask a Raft for its current Term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import "sync"
import (
	"labrpc"
	"time"
	"math/rand"
	"bytes"
	"encoding/gob"
)

// import "bytes"
// import "encoding/gob"

const(
	FollowerState  = 1
	CandidateState = 2
	LeaderState = 3
)

//
// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make().
//
type ApplyMsg struct {
	Index       int
	Command     interface{}
	UseSnapshot bool   // ignore for lab2; only used in lab3
	Snapshot    []byte // ignore for lab2; only used in lab3
}

type Log struct {
	Index int
	Term int
	L    interface{}
}

//
// A Go object implementing a single Raft peer.
//
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]

	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.

	//persistent state on all servers
	currentTerm int
	votedFor int
	log []Log

	//Volatile state on all servers
	commitIndex int
	lastApplied int

	//Volatile state on leaders
	nextIndex []int
	matchIndex []int

	//extend
	state int
	stateCh chan int
	heartBeatTimer *time.Timer
	commitTimer *time.Ticker
	applyCh chan ApplyMsg
	closed bool
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {
	rf.mu.Lock()
	var term int
	var isleader bool
	// Your code here (2A).
	term = rf.currentTerm
	if rf.state == LeaderState {
		isleader = true
	}else {
		isleader = false
	}
	rf.mu.Unlock()
	return term, isleader
}

//
// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
//
func (rf *Raft) persist() {
	// Your code here (2C).
	// Example:
	 w := new(bytes.Buffer)
	 e := gob.NewEncoder(w)
	 e.Encode(rf.currentTerm)
	 e.Encode(rf.votedFor)
	 e.Encode(rf.log)
	 e.Encode(rf.commitIndex)
	 e.Encode(rf.lastApplied)
	 data := w.Bytes()
	 rf.persister.SaveRaftState(data)
}

//
// restore previously persisted state.
//
func (rf *Raft) readPersist(data []byte) {
	// Your code here (2C).
	// Example:
	 r := bytes.NewBuffer(data)
	 d := gob.NewDecoder(r)
	 d.Decode(&rf.currentTerm)
	 d.Decode(&rf.votedFor)
	 d.Decode(&rf.log)
	 d.Decode(&rf.commitIndex)
	 d.Decode(&rf.lastApplied)
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
}

func (rf *Raft) readSnapshot(data []byte) {
	msg := ApplyMsg{UseSnapshot:true, Snapshot:data}
	rf.applyCh <- msg
}

type AppendEntriesArgs struct {
	Term int
	LeaderId int
	PrevLogIndex int
	PrevLogTerm int
	Entries []Log
	LeaderCommit int
}

type AppendEntriesReply struct{
	Term int
	Success bool
	NeedCheckIndex int
}

func (rf *Raft) getCurrentState() int{
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.state
}

func (rf *Raft) setCurrentState(s int) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	rf.state = s
}

func (rf *Raft) getLastIndex() int {
	return rf.log[len(rf.log) - 1].Index
}

func (rf *Raft) getLastTerm() int {
	return rf.log[len(rf.log) - 1].Term
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}



func min(a, b int) int{
	if a < b{
		return a
	}else {
		return b
	}
}

func (rf *Raft) checkCanCommited(){
	rf.mu.Lock()

	n := rf.commitIndex
	serverNum := len(rf.peers)
	okNum := serverNum
	for okNum > serverNum / 2{
		okNum = 0
		n++
		for i := 0; i < serverNum; i++ {
			if n <= rf.matchIndex[i] {
				okNum++
			}
		}
	}
	baseIndex := rf.log[0].Index

	canCommitIndex := n - 1
	beginCommitIndex := rf.commitIndex + 1
	for i := canCommitIndex; i >= beginCommitIndex; i-- {
		if rf.currentTerm == rf.log[i - baseIndex].Term {
			rf.commitIndex = i
			//DPrintf("PeerId %d leader commit to %d", rf.me, rf.commitIndex)
			rf.persist()
			break
		}else {
			//DPrintf("i:%d, beginCommitIndex%d", i, beginCommitIndex)
			//DPrintf("PeerId %d leader do not commit to %d, currentTerm is %d, log term is %d\n", rf.me, i, rf.currentTerm, rf.log[i - baseIndex].Term)
		}
	}
	rf.mu.Unlock()
}

func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) {
	//DPrintf("sendAppendEntries args add %p\n", args)

	//if we don't check this, may follower state send appenentries RPC, cause error
	if rf.state != LeaderState {
		//rf.mu.Unlock()
		return
	}
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)

	rf.mu.Lock()
	if ok {
		if reply.Success {
			rf.nextIndex[server] = args.PrevLogIndex + len(args.Entries) + 1
			rf.matchIndex[server] = args.PrevLogIndex + len(args.Entries)
			if len(args.Entries) != 0 {
				go rf.checkCanCommited()
			}
		} else {
			if reply.Term > rf.currentTerm {
				//DPrintf("sendAppendEntries peerId %d become Follower\n", rf.me)
				rf.updateNewTerm(reply.Term)
				rf.stateCh <- FollowerState
				rf.persist()
			}else {
				if reply.NeedCheckIndex != -1{
					rf.nextIndex[server] = min(reply.NeedCheckIndex, rf.nextIndex[server])
				} else if rf.nextIndex[server] > 1{
					rf.nextIndex[server]--
				}
			}
		}
	}

	rf.mu.Unlock()
}

//
// example RequestVote RPC arguments structure.
// field names must start with capital letters!
//
type RequestVoteArgs struct {
	// Your data here (2A, 2B).
	Term         int
	CandidateId  int
	LastLogIndex int
	LastLogTerm  int
}

//
// example RequestVote RPC reply structure.
// field names must start with capital letters!
//
type RequestVoteReply struct {
	// Your data here (2A).
	timeout     bool
	Term        int
	VoteGranted bool
}

//
// example RequestVote RPC handler.
//
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	rf.mu.Lock()
	if rf.state == FollowerState {
		rf.resetHeartBeatTimer()
	}

	// Your code here (2A, 2B).
	if rf.currentTerm > args.Term {
		reply.Term = rf.currentTerm
		reply.VoteGranted = false
		rf.mu.Unlock()
		return
	}

	if rf.currentTerm < args.Term {
		rf.updateNewTerm(args.Term)
		//DPrintf("RequestVote peerId %d become Follower\n", rf.me)
		rf.stateCh <- FollowerState
	}

	logIndexSelf := rf.getLastIndex()
	baseIndex := rf.log[0].Index

	//DPrintf("PeerId is %d, termA:%d, indexA:%d, termB:%d, indexB:%d", rf.me, args.LastLogTerm, args.LastLogIndex, rf.log[logIndexSelf - baseIndex].Term, logIndexSelf)
	if (rf.votedFor == -1 || rf.me == args.CandidateId) &&
		isNewest(args.LastLogTerm, args.LastLogIndex, rf.log[logIndexSelf - baseIndex].Term, logIndexSelf){
		reply.Term = rf.currentTerm
		reply.VoteGranted = true

		rf.votedFor = args.CandidateId
		rf.persist()
	}else{
		reply.Term = rf.currentTerm
		reply.VoteGranted = false
	}
	//DPrintf("PeerId %d vote for %d\n", rf.me, rf.votedFor)
	//DPrintf("PeerId %d reply PeerId %d: Term is %d, vote is %v\n",
	//			rf.me, args.CandidateId, rf.currentTerm, reply.VoteGranted)
	rf.mu.Unlock()
}

func isNewest(termA, indexA, termB, indexB int) bool {
	if termA == termB {
		return indexA >= indexB
	}
	return termA > termB
}

//
// example code to send a RequestVote RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// look at the comments in ../labrpc/labrpc.go for more details.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
//
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	if ok {
		reply.timeout = false
	}else {
		reply.timeout = true
	}
	return ok
}

//
// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// Term. the third return value is true if this server believes it is
// the leader.
//
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	index := -1
	term := -1
	isLeader := true

	// Your code here (2B).
	//DPrintf("PeerId %d, Start !!!!!!!!!", rf.me)

	rf.mu.Lock()
	if rf.state == LeaderState {
		isLeader = true
		index = rf.getLastIndex() + 1
		term = rf.currentTerm

		rf.log = append(rf.log, Log{index,term, command})
		rf.persist()
		go rf.sendAppendEntriesToAllServer()
		DPrintf("Start leader is %d, index is %d, term is %d, command is %v, log is %v\n",rf.me, index, term, command, rf.log)
	}else {
		isLeader = false
	}
	//DPrintf("PeerId %d start", rf.me)
	rf.mu.Unlock()

	return index, term, isLeader
}

func (rf *Raft) StartSnapshot(data []byte, index int){
	rf.mu.Lock()

	if index < rf.log[0].Index || index > rf.getLastIndex() {
		rf.mu.Unlock()
		return
	}
	baseIndex := rf.log[0].Index

	newLog := make([]Log, 0)
	newLog = append(newLog, Log{index, rf.log[index - baseIndex].Term, nil})

	for i := index + 1; i <= rf.getLastIndex(); i++ {
		newLog = append(newLog, rf.log[i - baseIndex])
	}
	rf.log = newLog
	rf.persist()
	rf.persister.SaveSnapshot(data)
	rf.mu.Unlock()
}

type InstallSnapshotArgs struct{
	Term int
	LeaderId int
	LastIncludeIndex int
	LastIncludeTerm int
	Data []byte
}

type InstallSnapshotReply struct {
	Term int
}

func (rf *Raft) InstallSnapshot(args *InstallSnapshotArgs, reply *InstallSnapshotReply) {
	rf.mu.Lock()
	if rf.state == FollowerState {
		rf.resetHeartBeatTimer()
	}

	if args.Term < rf.currentTerm {
		reply.Term = rf.currentTerm
		rf.mu.Unlock()
		return
	}

	rf.currentTerm = args.Term
	rf.votedFor = args.LeaderId

	oldBaseIndex := rf.log[0].Index
	newLog := make([]Log, 1)
	newLog[0].Index = args.LastIncludeIndex
	newLog[0].Term = args.LastIncludeTerm

	for i := args.LastIncludeIndex + 1; i < rf.getLastIndex(); i++ {
		newLog = append(newLog, rf.log[i - oldBaseIndex])
	}

	msg := ApplyMsg{UseSnapshot:true, Snapshot:args.Data}
	rf.lastApplied = args.LastIncludeIndex
	rf.commitIndex = args.LastIncludeIndex

	rf.persist()
	rf.persister.SaveSnapshot(args.Data)
	//DPrintf("InstallSnapshot")
	rf.mu.Unlock()
	rf.applyCh <- msg
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	//DPrintf("PeerId %d receive append entries\n", rf.me)
	rf.mu.Lock()
	if rf.state == FollowerState {
		rf.resetHeartBeatTimer()
	}

	lastLogIndex := rf.getLastIndex()
	baseIndex := rf.log[0].Index
	if args.Term > rf.currentTerm{
		rf.currentTerm = args.Term
		rf.votedFor = args.LeaderId
		//DPrintf("AppendEnties peerID %d become Follower\n",rf.me)
		rf.stateCh <- FollowerState
	}

	if args.Term < rf.currentTerm {
		//DPrintf("111111111")
		reply.Term = rf.currentTerm
		reply.Success = false
		reply.NeedCheckIndex = -1
		rf.mu.Unlock()
		return
	}
	if lastLogIndex < args.PrevLogIndex {
		//DPrintf("peerId is %d, 222222222, %d, %d", rf.me, lastLogIndex, args.PrevLogIndex)
		reply.Term = rf.currentTerm
		reply.Success = false
		reply.NeedCheckIndex = lastLogIndex + 1
		rf.mu.Unlock()
		return
	}
	if args.PrevLogIndex < baseIndex {
		reply.Term = rf.currentTerm
		reply.Success = false
		reply.NeedCheckIndex = -1
		rf.mu.Unlock()
		return
	}
	if rf.log[args.PrevLogIndex - baseIndex].Term != args.PrevLogTerm{
		//DPrintf("PeerId is %d, 33333333333，%d, %d, %d", rf.me,args.PrevLogIndex ,rf.log[args.PrevLogIndex - baseIndex].Term, args.PrevLogTerm)
		reply.Term = rf.currentTerm
		reply.Success = false
		reply.NeedCheckIndex = max(args.PrevLogIndex - 50, 1)
		rf.mu.Unlock()
		return
	}

	if args.LeaderId != rf.votedFor {
		//DPrintf("PeerId is %d, 44444444, %d, %d", rf.me, args.LeaderId, rf.votedFor)
		reply.Term = args.Term
		reply.Success = false
		reply.NeedCheckIndex = -1
		if rf.votedFor == -1 {
			rf.votedFor = args.LeaderId
		}
		rf.mu.Unlock()
		return
	}

	beginIndex := args.PrevLogIndex + 1
	DPrintf("PeerId %d: term is %d,  entries log is %v\n", rf.me,args.Term, args.Entries)
	for i := 0; i < len(args.Entries); i++ {
		if i + beginIndex <= rf.getLastIndex() {
			if rf.log[i + beginIndex - baseIndex].Index != args.Entries[i].Index ||
				rf.log[i + beginIndex - baseIndex].Term != args.Entries[i].Term{
				rf.log[i + beginIndex - baseIndex] = args.Entries[i]
				rf.log = rf.log[ : i + beginIndex + 1 - baseIndex: i + beginIndex + 1 - baseIndex]
			}

		}else {
			rf.log = append(rf.log, args.Entries[i])
		}
		//DPrintf("PeerId %d: index log is %v", rf.me, rf.log)
	}

	if args.LeaderCommit > rf.commitIndex {
		rf.commitIndex = min(args.LeaderCommit, rf.getLastIndex())
		DPrintf("PeerId %d commit to %d, log is %v", rf.me, rf.commitIndex, rf.log)
	}
	reply.Term = rf.currentTerm
	reply.Success = true
	if len(args.Entries) != 0 {
		rf.persist()
	}

	rf.mu.Unlock()
}

func (rf *Raft) sendInstallSnapshot(server int, args *InstallSnapshotArgs, reply *InstallSnapshotReply) {
	//if we don't check this, may follower state send appenentries RPC, cause error
	if rf.state != LeaderState {
		//rf.mu.Unlock()
		return
	}
	ok := rf.peers[server].Call("Raft.InstallSnapshot", args, reply)

	rf.mu.Lock()
	if ok {
		if reply.Term > rf.currentTerm {
			rf.updateNewTerm(reply.Term)
			rf.stateCh <- FollowerState
			rf.persist()
		}else {
			rf.nextIndex[server] = args.LastIncludeIndex + 1
			rf.matchIndex[server] = args.LastIncludeIndex
		}
	}
	rf.mu.Unlock()
}


func (rf *Raft) sendAppendEntriesToAllServer () {
	rf.mu.Lock()
	l := len(rf.peers)
	baseIndex := rf.log[0].Index
	for i := 0; i < l; i++ {
		if i == rf.me {
			rf.nextIndex[i] = rf.getLastIndex() + 1
			rf.matchIndex[i] = rf.getLastIndex()
			continue
		}
		if baseIndex >= rf.nextIndex[i] {
			installSnapshotArgs := new(InstallSnapshotArgs)

			//init
			installSnapshotArgs.Term = rf.currentTerm
			installSnapshotArgs.LeaderId = rf.me
			installSnapshotArgs.LastIncludeIndex = rf.log[0].Index
			installSnapshotArgs.LastIncludeTerm = rf.log[0].Term
			installSnapshotArgs.Data = rf.persister.ReadSnapshot()

			installSnapshotReply := new(InstallSnapshotReply)
			go rf.sendInstallSnapshot(i, installSnapshotArgs, installSnapshotReply)

		}else {
			appendEntriesArgs := new(AppendEntriesArgs)
			rf.initIPeerEntries(appendEntriesArgs, i)

			appendEntriesReply := new(AppendEntriesReply)
			appendEntriesReply.Success = false
			appendEntriesReply.Term = -1
			appendEntriesReply.NeedCheckIndex = -1

			go rf.sendAppendEntries(i, appendEntriesArgs, appendEntriesReply)
		}
	}

	rf.mu.Unlock()
}

//
// the tester calls Kill() when a Raft instance won't
// be needed again. you are not required to do anything
// in Kill(), but it might be convenient to (for example)
// turn off debug output from this instance.
//
func (rf *Raft) Kill() {
	// Your code here, if desired.
	rf.mu.Lock()
	//DPrintf("PeerId %d crash\n", rf.me)

	//close(rf.applyCh)
	//rf.applyCh = nil
	close(rf.stateCh)
	rf.stateCh = nil
	rf.closed = true
	rf.mu.Unlock()
}

//
// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
//
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	// Your initialization code here (2A, 2B, 2C).
	rf.state = FollowerState
	rf.stateCh = make(chan int)

	rf.currentTerm = 0
	rf.votedFor = -1
	rf.log = make([]Log, 1)
	rf.log[0].Index = 0
	rf.log[0].Term = 0
	rf.applyCh = applyCh

	rf.commitIndex = 0
	rf.lastApplied = 0

	rf.nextIndex = make([]int, len(peers))
	rf.matchIndex = make([]int, len(peers))

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())
	rf.readSnapshot(persister.ReadSnapshot())
	rf.displayLog()

	go rf.createCommitTimer()

	go rf.stateMachine()

	return rf
}

func (rf *Raft) displayLog() {
	//DPrintf("PeerId %d display begin", rf.me)
	rf.mu.Lock()
	baseIndex := rf.log[0].Index
	for i := baseIndex + 1; i <= rf.commitIndex; i++ {
		if i - baseIndex < len(rf.log){
			//DPrintf("i:%d, baseIndex:%d", i, baseIndex)
			log := rf.log[i - baseIndex]
			msg := ApplyMsg{log.Index, log.L, false, nil}
			rf.applyCh <- msg
		}
	}
	rf.mu.Unlock()
	//DPrintf("PeerId %d display end", rf.me)
}

func (rf *Raft) stopCommittimer() {
	rf.commitTimer.Stop()
}

func (rf *Raft) createCommitTimer() {
	rf.commitTimer = time.NewTicker(200 * time.Millisecond)
	for _ = range rf.commitTimer.C {
		rf.mu.Lock()
		if rf.closed {
			break
		}
		cIndex := rf.commitIndex
		for cIndex > rf.lastApplied {
			baseIndex := rf.log[0].Index
			rf.lastApplied++
			//DPrintf("PeerId %d log is %v", rf.me, rf.log)
			//apply
			rf.persist()

			index := rf.lastApplied
			if index > baseIndex && rf.closed != true{
				DPrintf("index :%d, baseIndex :%d, log :%v", index, baseIndex, rf.log)
				command := rf.log[index - baseIndex].L
				rf.mu.Unlock()
				rf.applyCh <- ApplyMsg{index, command, false, nil}
				rf.mu.Lock()
			}

		}
		rf.mu.Unlock()
	}
}


func (rf *Raft) getCurrentTerm() int{
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.currentTerm
}

func (rf *Raft) updateNewTerm(term int){
	rf.currentTerm = term
	rf.votedFor = -1
}

func (rf *Raft) createHeartBeatTimer() {
	//DPrintf("peerId %d Create Timer\n", rf.me)
	rf.heartBeatTimer = time.NewTimer(gettimeout() * time.Millisecond)

	go func(){
		for {
			<-rf.heartBeatTimer.C
			//DPrintf("peerId %d Follwer state send CandidateState\n", rf.me)
			if rf.stateCh == nil {
				 break
			}
			rf.stateCh <- CandidateState
		}
	}()
}

func gettimeout() time.Duration {
	n := rand.Intn(150) + 600
	return time.Duration(n)
}

func (rf *Raft) resetHeartBeatTimer() {
	//DPrintf("peerId %d Reset Timer\n", rf.me)
	rf.heartBeatTimer.Reset(gettimeout() * time.Millisecond)
}

func (rf *Raft) stopHeartBeatTimer() {
	//DPrintf("peerId %d Stop Timer\n", rf.me)
	rf.heartBeatTimer.Stop()
}

func (rf *Raft) stateMachine(){
	for {
		//DPrintf("peerId %d stateMachine state is %d, Term is %d\n", rf.me, rf.state, rf.currentTerm)

		switch rf.state {
		case FollowerState:
			if rf.heartBeatTimer == nil {
				rf.createHeartBeatTimer()
			}else {
				rf.resetHeartBeatTimer()
			}

			rf.state = <-rf.stateCh
		case CandidateState:
			rf.mu.Lock()
			rf.updateNewTerm(rf.currentTerm + 1)
			rf.mu.Unlock()
			rf.stopHeartBeatTimer()
			go time.AfterFunc(gettimeout() * time.Millisecond, func() {
				if rf.state == CandidateState {
					rf.stateCh <- CandidateState
				}
			})

			go rf.atCandidate()

			rf.state = <-rf.stateCh

		case LeaderState:
			rf.stopHeartBeatTimer()
			go rf.atLeader()
			rf.state = <- rf.stateCh
		default:
			//DPrintf("PeerId %d stateMachine default error\n", rf.me)
			return
		}
	}
}

func (rf *Raft) initIPeerEntries(appendEntriesArgs *AppendEntriesArgs, i int) {
	baseIndex := rf.log[0].Index
	appendEntriesArgs.Term = rf.currentTerm
	appendEntriesArgs.LeaderId = rf.me
	appendEntriesArgs.PrevLogIndex = rf.nextIndex[i] - 1
	appendEntriesArgs.PrevLogTerm = rf.log[appendEntriesArgs.PrevLogIndex - baseIndex].Term
	appendEntriesArgs.LeaderCommit = rf.commitIndex
	appendEntriesArgs.Entries = make([]Log, 0)

	logTag := rf.getLastIndex() + 1
	for j := rf.nextIndex[i]; j < logTag; j++ {
		appendEntriesArgs.Entries = append(appendEntriesArgs.Entries, rf.log[j - baseIndex])
	}
	DPrintf("PeerId %d, to %d, init entries %v", rf.me, i, appendEntriesArgs.Entries)
}

func (rf *Raft) atLeader() {
	for rf.getCurrentState() == LeaderState {
		rf.sendAppendEntriesToAllServer()

		time.Sleep((gettimeout() * time.Millisecond) / 2)
	}
}

func (rf *Raft) atCandidate() {
	rf.mu.Lock()
	var requestVoteArgs RequestVoteArgs
	requestVoteReplys := make([]RequestVoteReply, len(rf.peers))
	requestVoteArgs.Term = rf.currentTerm
	requestVoteArgs.CandidateId = rf.me
	requestVoteArgs.LastLogTerm = rf.getLastTerm()
	requestVoteArgs.LastLogIndex = rf.getLastIndex()
	rf.mu.Unlock()

	replyCh := make(chan bool)
	for i := 0; i < len(rf.peers); i++ {
		requestVoteReplys[i].timeout = true
		go func(i int) {
			ok := rf.sendRequestVote(i, &requestVoteArgs, &requestVoteReplys[i])
			if replyCh != nil{
				replyCh <- ok
			}
		}(i)
	}

	for i := 0; i < len(rf.peers); i++{
		ok := <-replyCh
		if !ok {
			continue
		}

		var voteSuccess int
		for i := 0; i < len(requestVoteReplys); i++ {
			if !requestVoteReplys[i].timeout && requestVoteReplys[i].VoteGranted && requestVoteReplys[i].Term == rf.currentTerm{
				voteSuccess++
				//DPrintf("peerId %d, peer %d vote success\n",rf.me, i)
			}

			if requestVoteReplys[i].Term > rf.getCurrentTerm() {
				rf.mu.Lock()
				rf.updateNewTerm(requestVoteReplys[i].Term)
				rf.persist()
				rf.mu.Unlock()
				rf.stateCh <- FollowerState
			}
		}
		//DPrintf("peerId %d Term %d, vote success number is %d!!!!!!!!!!!!!!!!\n", rf.me, rf.currentTerm, voteSuccess)

		if voteSuccess == len(requestVoteReplys) / 2 + 1 {
			//initial leader
			for i := 0; i < len(rf.peers); i++ {
				if i == rf.me {
					rf.nextIndex[i] = rf.getLastIndex() + 1
					rf.matchIndex[i] = rf.getLastIndex()
				}else {
					rf.nextIndex[i] = rf.getLastIndex() + 1
					rf.matchIndex[i] = 0
				}
			}
			rf.stateCh <- LeaderState
		}
	}
	close(replyCh)
}
