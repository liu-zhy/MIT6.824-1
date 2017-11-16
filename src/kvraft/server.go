package raftkv

import (
	"encoding/gob"
	"labrpc"
	"log"
	"raft"
	"sync"
	"time"
	//"bytes"
	"bytes"
)

const Debug = 1

const (
	PutOp = iota
	AppendOp
	GetOp
)

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug > 0 {
		log.Printf(format, a...)
	}
	return
}


func (op *Op) DoTask(m map[string]string) (Err, string){
	var ret string
	switch op.OpType {
	case PutOp:
		key := op.Args[0]
		value := op.Args[1]
		m[key] = value
	case AppendOp:
		key := op.Args[0]
		v, ok := m[key]
		if !ok {
			return ErrNoKey, ""
		}
		value := v + op.Args[1]
		m[key] = value
	case GetOp:
		key := op.Args[0]
		v, ok := m[key]
		if !ok {
			return ErrNoKey, ""
		}
		ret = v
	default:
	}

	return OK, ret
}


func (kv *RaftKV) Get(args *GetArgs, reply *GetReply) {
	// Your code here.
	op := Op{}

	op.OpType = GetOp
	op.Args[0] =args.Key
	op.ClientId = args.ClientId
	op.OpNum= args.OpNum

	kv.mu.Lock()
	index, _, isLeaader := kv.rf.Start(op)

	if isLeaader {
		kv.terms[index] = pack{&op, false, Err(""), ""}
	}
	kv.mu.Unlock()

	if isLeaader {
		t := time.NewTicker(timeout * time.Millisecond)
		i := 0
		for {
			<- t.C
			i++
			//DPrintf("tiemrtiemrtiemr")
			kv.mu.Lock()
			p := kv.terms[index]

			//timeout
			if i == 10 {
				reply.Err = OK
				reply.WrongLeader = true
				delete(kv.terms, index)

				kv.mu.Unlock()
				break
			}
			if p.runed {
				reply.Err= p.err
				reply.WrongLeader = false
				reply.Value = p.value
				if p.err == Err("err leader") {
					reply.WrongLeader = true
					reply.Err = OK
					//DPrintf("peerId is %d, reply addr is %x", kv.me, unsafe.Pointer(reply))
				}
				delete(kv.terms, index)
				//DPrintf("peerId is %d, putappend reply is %v, kv is %v", kv.me,reply, kv.kv)

				kv.mu.Unlock()
				break
			}

			kv.mu.Unlock()
		}
	}else{
		reply.WrongLeader = true
		reply.Err = OK
	}

	return
}

const timeout = 500

func (kv *RaftKV) PutAppend(args *PutAppendArgs, reply *PutAppendReply) {
	//Your code here.
	op := Op{}
	if args.Op == "Put"{
		op.OpType = PutOp
	}else {
		op.OpType = AppendOp
	}

	op.Args[0] = args.Key
	op.Args[1] = args.Value
	op.ClientId = args.ClientId
	op.OpNum= args.OpNum

	kv.mu.Lock()
	index, _, isLeaader := kv.rf.Start(op)
	if isLeaader {
		kv.terms[index] = pack{&op, false, Err("Default err"), ""}
	}
	kv.mu.Unlock()

	if isLeaader {
		t := time.NewTicker(timeout * time.Millisecond)
		i := 0
		for {
			<- t.C
			i++
			//DPrintf("tiemrtiemrtiemr")
			kv.mu.Lock()
			p := kv.terms[index]

			//timeout
			if i == 10 {
				reply.Err = OK
				reply.WrongLeader = true
				delete(kv.terms, index)

				kv.mu.Unlock()
				break
			}
			if p.runed {
				reply.Err= p.err
				reply.WrongLeader = false
				if p.err == Err("err leader") {
					reply.WrongLeader = true
					reply.Err = OK
					//DPrintf("peerId is %d, reply addr is %x", kv.me, unsafe.Pointer(reply))
				}
				delete(kv.terms, index)
				//DPrintf("peerId is %d, putappend reply is %v, kv is %v", kv.me,reply, kv.kv)

				kv.mu.Unlock()
				break
			}

			kv.mu.Unlock()
		}
	}else{
		reply.WrongLeader = true
		reply.Err = OK
		//DPrintf("peerId is %d, reply addr is %x", kv.me, unsafe.Pointer(reply))
	}
	kv.mu.Lock()
	DPrintf("peerId is %d, putappend args is %v, reply is %v, terms is %v, kv is %v", kv.me,args,reply, kv.terms, kv.kv)
	kv.mu.Unlock()
}

//
// the tester calls Kill() when a RaftKV instance won't
// be needed again. you are not required to do anything
// in Kill(), but it might be convenient to (for example)
// turn off debug output from this instance.
//
func (kv *RaftKV) Kill() {
	kv.rf.Kill()
	// Your code here, if desired.
}

//
// servers[] contains the ports of the set of
// servers that will cooperate via Raft to
// form the fault-tolerant key/value service.
// me is the index of the current server in servers[].
// the k/v server should store snapshots with persister.SaveSnapshot(),
// and Raft should save its state (including log) with persister.SaveRaftState().
// the k/v server should snapshot when Raft's saved state exceeds maxraftstate bytes,
// in order to allow Raft to garbage-collect its log. if maxraftstate is -1,
// you don't need to snapshot.
// StartKVServer() must return quickly, so it should start goroutines
// for any long-running work.
//
func StartKVServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister, maxraftstate int) *RaftKV {
	// call gob.Register on structures you want
	// Go's RPC library to marshall/unmarshall.
	gob.Register(Op{})

	kv := new(RaftKV)
	kv.me = me
	kv.maxraftstate = maxraftstate

	// You may need initialization code here.

	kv.applyCh = make(chan raft.ApplyMsg)
	kv.rf = raft.Make(servers, me, persister, kv.applyCh)
	kv.kv = make(map[string]string)
	kv.terms = make(map[int]pack)
	kv.opCount = make(map[int64]int64)
	kv.persister = persister
	kv.readPersist(persister.ReadSnapshot())
	go kv.applyChannel()

	// You may need initialization code here.

	return kv
}


type RaftKV struct {
	mu      sync.Mutex
	me      int
	rf      *raft.Raft
	applyCh chan raft.ApplyMsg

	maxraftstate int // snapshot if log grows this big

	// Your definitions here.
	kv map[string]string
	terms map[int]pack
	opCount map[int64]int64
	persister  *raft.Persister
}

type pack struct {
	op *Op
	runed bool
	err Err
	value string
}


type Op struct {
	// Your definitions here.
	// Field names must start with capital letters,
	// otherwise RPC will break.
	OpType int
	Args [2]string
	ClientId int64
	OpNum int64
}

func (kv *RaftKV) persist() {
	// Your code here (2C).
	// Example:
	w := new(bytes.Buffer)
	e := gob.NewEncoder(w)
	e.Encode(kv.kv)
	e.Encode(kv.opCount)
	data := w.Bytes()
	kv.persister.SaveSnapshot(data)
}

//
// restore previously persisted state.
//
func (kv *RaftKV) readPersist(data []byte) {
	// Your code here (2C).
	// Example:
	r := bytes.NewBuffer(data)
	d := gob.NewDecoder(r)
	d.Decode(&kv.kv)
	d.Decode(&kv.opCount)
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
}


func (kv *RaftKV) applyChannel (){
	for {
		apply, ok := <-kv.applyCh
		//DPrintf("applyChannel apply is %v, ok is %v", apply, ok)
		if ok {

			kv.mu.Lock()
			p, b := kv.terms[apply.Index]
			op := p.op

			command := apply.Command.(Op)
			if b {
				//run kv machine
				if kv.opCount[command.ClientId] >= command.OpNum && op.OpType != GetOp{
					kv.terms[apply.Index] = pack{op, true, OK, ""}
					//DPrintf("xxxxxx, %d", kv.opCount[command.ClientId])
				}else{
					//DPrintf("yyyyyy")
					error, value := command.DoTask(kv.kv)
					if *op == command{
						kv.terms[apply.Index] = pack{op, true,error, value}
					}else {
						kv.terms[apply.Index] = pack{op, true,Err("err leader"), ""}
					}
					kv.opCount[command.ClientId] = command.OpNum
				}
			}else{
				if kv.opCount[command.ClientId] < command.OpNum {
					command.DoTask(kv.kv)
					kv.opCount[command.ClientId] = command.OpNum
				}

			}
			kv.persist()
			kv.mu.Unlock()
		}else {
			break
		}
	}
}

