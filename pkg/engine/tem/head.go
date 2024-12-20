package tem

import (
	"log"
	"math"
	"sync/atomic"

	"github.com/szuwgh/hawkobserve/pkg/temql"

	"github.com/szuwgh/hawkobserve/pkg/analysis"
	"github.com/szuwgh/hawkobserve/pkg/engine/tem/mem"
	"github.com/szuwgh/hawkobserve/pkg/engine/tem/util/byteutil"
	"github.com/szuwgh/hawkobserve/pkg/lib/logproto"
	"github.com/szuwgh/hawkobserve/pkg/tokenizer"
)

type Head struct {
	rwControl
	mint       int64
	MaxT       int64
	indexMem   *mem.MemTable
	logsMem    *mem.LogsTable
	chunkRange int64
	lastSegNum uint64
	a          *analysis.Analyzer
	logSize    uint64
	nextID     uint64
	meta       *blockMeta
}

func NewHead(alloc byteutil.Allocator, chunkRange int64, a *analysis.Analyzer, skiplistLevel, skipListInterval int, msgTagName string) *Head {
	h := &Head{
		mint: math.MinInt64,
		MaxT: math.MinInt64,
	}
	h.indexMem = mem.NewMemTable(byteutil.NewInvertedBytePool(alloc), skiplistLevel, skipListInterval, msgTagName)
	h.logsMem = mem.NewLogsTable(byteutil.NewForwardBytePool(alloc))
	h.chunkRange = chunkRange
	h.a = a
	return h
}

//add some logs
func (h *Head) addLogs(r logproto.Stream) error {
	log.Println("add logs", r.Labels, r.Entries[0].Timestamp.Unix(), r.Entries[len(r.Entries)-1].Timestamp.Unix())
	context := mem.Context{}
	series, err := h.serieser(r.Labels)
	if err != nil {
		return err
	}
	h.setMinTime(r.Entries[0].Timestamp.UnixNano() / 1e6)
	for _, e := range r.Entries {
		tokens := h.tokener(&e)
		h.indexMem.Index(&context, h.getNextID(), e.Timestamp.UnixNano()/1e6, series, tokens)
		h.indexMem.Flush()
	}
	h.setMaxTime(r.Entries[len(r.Entries)-1].Timestamp.UnixNano() / 1e6)
	return nil
}

func (h *Head) serieser(labels string) (*mem.MemSeries, error) {
	lset, err := temql.ParseLabels(labels)
	if err != nil {
		return nil, err
	}
	s, _ := h.indexMem.GetOrCreate(lset.Hash(), lset)
	return s, nil
}

func (h *Head) tokener(entry *logproto.Entry) tokenizer.Tokens {
	msg := byteutil.Str2bytes(entry.Line)
	tokens := h.a.Analyze(msg)
	return tokens
}

func (h *Head) setMinTime(t int64) {
	if h.mint == math.MinInt64 {
		atomic.StoreInt64(&h.mint, t)
		h.indexMem.SetBaseTimeStamp(t)
	}
}

func (h *Head) setMaxTime(t int64) {
	ht := h.MaxT
	atomic.CompareAndSwapInt64(&h.MaxT, ht, t)
}

func (h *Head) reset() {
	h.mint = math.MinInt64
	h.lastSegNum = 0
	h.logSize = 0
	h.nextID = 0
}

func (h *Head) ReadDone() {
	h.pendingReaders.Done()
}

func (h *Head) Index() IndexReader {
	h.startRead()
	return &blockIndexReader{h.indexMem, h}
}

func (h *Head) Logs() LogReader {
	h.startRead()
	return &blockLogReader{h.logsMem, h}
}

func (h *Head) MinTime() int64 {
	return atomic.LoadInt64(&h.mint)
}

func (h *Head) MaxTime() int64 {
	return atomic.LoadInt64(&h.MaxT)
}

func (h *Head) LogNum() uint64 {
	return h.indexMem.LogNum()
}

func (h *Head) LastSegNum() uint64 {
	return h.lastSegNum
}

func (h *Head) size() uint64 {
	return atomic.LoadUint64(&h.logSize)
}

func (h *Head) Close() {
	h.waitRead()

}

func (h *Head) open() {
	h.closing = false
	h.indexMem.Init()
}

func (h *Head) getNextID() uint64 {
	h.nextID++
	return h.nextID
}

func (h *Head) release(recycle, alloced *int) error {
	h.waitRead()
	h.reset()
	h.indexMem.ReleaseBuff(recycle, alloced)
	h.logsMem.ReleaseBuff(recycle, alloced)
	return nil
}
